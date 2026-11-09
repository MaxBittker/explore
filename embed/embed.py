"""SigLIP 2 image/text embedder for the river, running on Modal.

    modal run embed/embed.py::test --sample sample.csv --out sample_siglip2.npz
    modal run embed/embed.py::bench
    modal run embed/embed.py::run --rows blocks.csv --tag siglip2-v1   # resumable bulk run

Bulk results land in the `river-embeddings` Volume as <tag>/shard_<n>.npz
(ids, emb f16, width, height, dhash, failed). width/height are the downloaded
thumbnail's; dhash is a 64-bit difference hash for exact-duplicate detection.
Re-running with the same CSV and tag skips finished shards; use a new tag for
a new export, since shard n is just rows[n*SHARD:(n+1)*SHARD].

Embeddings are L2-normalized float16: 1152-d google/siglip2-so400m-patch16-384
by default, or 768-d google/siglip2-base-patch16-256 with RIVER_MODEL=base256
(what the river2m_base search DB uses):

    RIVER_MODEL=base256 modal run embed/embed.py::run --rows river2m.csv --tag siglip2-base256-river2m
"""

import io
import os
import time
from concurrent.futures import ThreadPoolExecutor

import modal

# RIVER_MODEL picks the encoder; it is baked into the Modal image env so the
# containers load the same model as the local entrypoint.
MODELS = {
    "so400m": ("google/siglip2-so400m-patch16-384", 1152),
    "base256": ("google/siglip2-base-patch16-256", 768),
}
MODEL = os.environ.get("RIVER_MODEL", "so400m")
# RIVER_MODEL=both embeds every download with so400m AND base256 (downloads are
# the bottleneck, so the second encoder is nearly free); bulk runs then write
# two shard dirs, --tag (so400m) and --tag-b (base256). Text/bench use so400m.
ENCODERS = ["so400m", "base256"] if MODEL == "both" else [MODEL]
MODEL_ID, DIM = MODELS[ENCODERS[0]]
GPU = os.environ.get("RIVER_GPU", "A10G")  # L4 had no capacity on 2026-11-04
BATCH = 64
DOWNLOAD_CONCURRENCY = 16  # per container; keep this polite
USER_AGENT = "river-embedder/1.0 (+https://river.maxbittker.com)"

app = modal.App("river-embed")
hf_cache = modal.Volume.from_name("river-hf-cache", create_if_missing=True)
results = modal.Volume.from_name("river-embeddings", create_if_missing=True)
SHARD = 1000

image = (
    modal.Image.debian_slim(python_version="3.12")
    .pip_install(
        "torch==2.8.0",
        "torchvision==0.23.0",
        "transformers>=4.56,<5",
        "sentencepiece",
        "protobuf",
        "pillow",
        "requests",
        "numpy",
    )
    .env({"HF_HOME": "/cache/hf", "HF_HUB_ENABLE_HF_TRANSFER": "0", "RIVER_MODEL": MODEL})
)


def _features(out):
    import torch

    return out if torch.is_tensor(out) else out.pooler_output


def _load_model(model_id=MODEL_ID):
    import torch
    from transformers import AutoModel, AutoProcessor

    model = AutoModel.from_pretrained(model_id, dtype=torch.float16).cuda().eval()
    processor = AutoProcessor.from_pretrained(model_id, use_fast=True)
    hf_cache.commit()
    return model, processor


def _embed_images(model, processor, imgs):
    """PIL images -> list of (n x DIM) L2-normalized float16 arrays, BATCH at a time."""
    import torch

    out = []
    for i in range(0, len(imgs), BATCH):
        inputs = processor(images=imgs[i : i + BATCH], return_tensors="pt").to("cuda")
        with torch.inference_mode():
            f = _features(model.get_image_features(pixel_values=inputs["pixel_values"].half()))
        f = torch.nn.functional.normalize(f.float(), dim=-1)
        out.append(f.half().cpu().numpy())
    return out


def _embed_texts(model, processor, texts):
    """texts -> (n x DIM) L2-normalized float16 array."""
    import torch

    inputs = processor(
        text=[t.lower() for t in texts], padding="max_length", max_length=64, truncation=True, return_tensors="pt"
    ).to("cuda")
    with torch.inference_mode():
        f = _features(model.get_text_features(**inputs))
    return torch.nn.functional.normalize(f.float(), dim=-1).half().cpu().numpy()


def _dhash(img) -> int:
    """64-bit difference hash of the border-trimmed image, as a signed int64.

    Solid borders (letterboxing, padding onto a square) are cropped first, so a
    poster and the same poster padded onto a black square hash alike. Compare
    hashes by Hamming distance. Keep in sync with thristy/hash_pass.py.
    """
    from PIL import Image, ImageChops

    g = img.convert("L")
    bg = Image.new("L", g.size, g.getpixel((0, 0)))
    box = ImageChops.difference(g, bg).point(lambda v: 255 if v > 16 else 0).getbbox()
    if box and (box[2] - box[0]) * (box[3] - box[1]) >= 0.25 * g.size[0] * g.size[1]:
        g = g.crop(box)
    px = list(g.resize((9, 8), Image.LANCZOS).getdata())
    bits = 0
    for r in range(8):
        for c in range(8):
            bits = (bits << 1) | (1 if px[r * 9 + c] > px[r * 9 + c + 1] else 0)
    return bits - (1 << 64) if bits >= (1 << 63) else bits


# Each container downloads DOWNLOAD_CONCURRENCY thumbs at a time; bulk runs are
# download-bound, so this is the knob for throughput (and CDN load).
CONTAINERS = int(os.environ.get("RIVER_CONTAINERS", "4"))


@app.cls(gpu=GPU, image=image, volumes={"/cache": hf_cache, "/out": results}, timeout=3600, max_containers=CONTAINERS)
class Embedder:
    @modal.enter()
    def load(self):
        import requests

        self.encoders = {name: _load_model(MODELS[name][0]) for name in ENCODERS}
        self.model, self.processor = self.encoders[ENCODERS[0]]
        self.session = requests.Session()
        self.session.headers["User-Agent"] = USER_AGENT
        adapter = requests.adapters.HTTPAdapter(pool_maxsize=DOWNLOAD_CONCURRENCY)
        self.session.mount("https://", adapter)

    def _download(self, url):
        from PIL import Image

        for attempt in range(3):
            try:
                r = self.session.get(url, timeout=20)
                if r.status_code == 404:
                    return None
                r.raise_for_status()
                img = Image.open(io.BytesIO(r.content))
                img.seek(0)  # first frame of gifs
                return img.convert("RGB")
            except Exception:
                time.sleep(2**attempt)
        return None

    def _embed_images(self, imgs):
        return _embed_images(self.model, self.processor, imgs)

    @modal.method()
    def embed_rows(self, rows: list[tuple[int, str]]) -> dict:
        return self._embed_rows(rows)

    @modal.method()
    def embed_shard(self, tag: str, n: int, rows: list[tuple[int, str]], tag_b: str = "") -> tuple[int, int]:
        import numpy as np

        r = self._embed_rows(rows)
        # in both-mode the primary (so400m) shard is written last, so its
        # presence means the base256 one exists too
        tags = dict(zip(ENCODERS, [tag, tag_b]))
        for name in reversed(ENCODERS):
            d = f"/out/{tags[name]}"
            os.makedirs(d, exist_ok=True)
            tmp = f"{d}/.shard_{n:06d}.npz"
            np.savez(
                tmp, ids=r["ids"], emb=r["embs"][name], width=r["width"], height=r["height"],
                dhash=r["dhash"], failed=np.array(r["failed"], np.int64),
            )
            os.replace(tmp, f"{d}/shard_{n:06d}.npz")
        results.commit()
        return len(r["ids"]), len(r["failed"])

    def _embed_rows(self, rows):
        """rows: [(block_id, thumb_url)] -> ids, emb (n x DIM f16), width, height, failed ids."""
        import numpy as np

        t0 = time.time()
        with ThreadPoolExecutor(DOWNLOAD_CONCURRENCY) as pool:
            imgs = list(pool.map(self._download, [u for _, u in rows]))
        t1 = time.time()
        ok = [(bid, im) for (bid, _), im in zip(rows, imgs) if im is not None]
        failed = [bid for (bid, _), im in zip(rows, imgs) if im is None]
        embs = {
            name: (
                np.concatenate(_embed_images(m, p, [im for _, im in ok]))
                if ok
                else np.zeros((0, MODELS[name][1]), np.float16)
            )
            for name, (m, p) in self.encoders.items()
        }
        t2 = time.time()
        return {
            "ids": np.array([bid for bid, _ in ok], np.int64),
            "emb": embs[ENCODERS[0]],
            "embs": embs,
            "width": np.array([im.width for _, im in ok], np.int32),
            "height": np.array([im.height for _, im in ok], np.int32),
            "dhash": np.array([_dhash(im) for _, im in ok], np.int64),
            "failed": failed,
            "download_s": t1 - t0,
            "embed_s": t2 - t1,
        }

    @modal.method()
    def embed_texts(self, texts: list[str]):
        return _embed_texts(self.model, self.processor, texts)

    @modal.method()
    def bench(self, n: int = 2048) -> float:
        """Pure GPU+preprocess throughput (images/sec) on synthetic 400px images."""
        import numpy as np
        from PIL import Image

        rng = np.random.default_rng(0)
        imgs = [Image.fromarray(rng.integers(0, 255, (400, 320, 3), dtype=np.uint8)) for _ in range(BATCH)]
        self._embed_images(imgs)  # warmup
        t = time.time()
        for _ in range(n // BATCH):
            self._embed_images(imgs)
        return n / (time.time() - t)


@app.local_entrypoint()
def test(sample: str, out: str, chunk: int = 250):
    import csv

    import numpy as np

    with open(sample) as f:
        rows = [(int(r["id"]), r["thumb"]) for r in csv.DictReader(f)]
    chunks = [rows[i : i + chunk] for i in range(0, len(rows), chunk)]
    t = time.time()
    results = list(Embedder().embed_rows.map(chunks))
    wall = time.time() - t
    ids = np.concatenate([r["ids"] for r in results])
    emb = np.concatenate([r["emb"] for r in results])
    failed = sum((r["failed"] for r in results), [])
    queries = [
        "brutalist concrete architecture",
        "a hand-drawn map",
        "japanese woodblock print of waves",
        "risograph poster with bold typography",
        "a cozy messy studio with plants",
        "fog over a mountain landscape",
    ]
    qemb = Embedder().embed_texts.remote(queries)
    np.savez(out, ids=ids, emb=emb, failed=np.array(failed), queries=np.array(queries), qemb=qemb)
    print(f"embedded {len(ids)} / {len(rows)} ({len(failed)} failed) in {wall:.1f}s wall")
    for r in results:
        print(f"  chunk: download {r['download_s']:.1f}s, embed {r['embed_s']:.1f}s for {len(r['ids'])}")


@app.local_entrypoint()
def run(rows: str, tag: str = "siglip2-v1", limit: int = 0, keep: str = "", tag_b: str = ""):
    """rows: CSV with id,thumb columns, in a stable order (shard n = rows[n*SHARD:(n+1)*SHARD]).

    keep: optional file of block ids (one per line). Rows not listed are skipped
    inside their shard, so shard numbering, and resuming, are unaffected.
    tag_b: base256 shard dir when RIVER_MODEL=both (default: tag with
    "siglip2-" -> "siglip2-base256-").
    """
    import csv

    csv.field_size_limit(10**7)
    with open(rows) as f:
        all_rows = [(int(r["id"]), r["thumb"]) for r in csv.DictReader(f) if r["thumb"]]
    if limit:
        all_rows = all_rows[:limit]
    shards = [all_rows[i : i + SHARD] for i in range(0, len(all_rows), SHARD)]
    if keep:
        with open(keep) as f:
            ids = {int(x) for x in f if x.strip()}
        shards = [[r for r in s if r[0] in ids] for s in shards]
        print(f"keeping {sum(map(len, shards))} of {len(all_rows)} rows listed in {keep}")
    if MODEL == "both":
        tag_b = tag_b or tag.replace("siglip2-", "siglip2-base256-", 1)
        assert tag_b != tag, "pass --tag-b"
        print(f"both encoders: so400m -> {tag}, base256 -> {tag_b}")

    def listed(t):
        try:
            return {e.path.rsplit("/", 1)[-1] for e in results.listdir(t)}
        except Exception:
            return set()

    done = listed(tag) & listed(tag_b) if MODEL == "both" else listed(tag)
    todo = [(tag, n, s, tag_b) for n, s in enumerate(shards) if f"shard_{n:06d}.npz" not in done]
    print(f"{len(all_rows)} rows, {len(shards)} shards, {len(shards) - len(todo)} already done, {len(todo)} to go")
    t, ok, bad = time.time(), 0, 0
    for i, (k, f) in enumerate(Embedder().embed_shard.starmap(todo, order_outputs=False)):
        ok, bad = ok + k, bad + f
        if i % 10 == 0 or i == len(todo) - 1:
            print(f"  {i + 1}/{len(todo)} shards, {ok} embedded, {bad} failed, {ok / (time.time() - t):.0f} img/s")


@app.local_entrypoint()
def bench(n: int = 2048):
    print(f"{GPU}: {Embedder().bench.remote(n):.0f} images/sec (preprocess + fp16 forward)")

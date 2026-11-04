"""SigLIP 2 image/text embedder for the river, running on Modal.

    modal run embed/embed.py::test --sample sample.csv --out sample_siglip2.npz
    modal run embed/embed.py::bench

Embeddings are L2-normalized float16, 1152-d (google/siglip2-so400m-patch16-384).
"""

import io
import time
from concurrent.futures import ThreadPoolExecutor

import modal

MODEL_ID = "google/siglip2-so400m-patch16-384"
DIM = 1152
GPU = "L4"
BATCH = 64
DOWNLOAD_CONCURRENCY = 16  # per container; keep this polite
USER_AGENT = "river-embedder/1.0 (+https://river.maxbittker.com)"

app = modal.App("river-embed")
hf_cache = modal.Volume.from_name("river-hf-cache", create_if_missing=True)

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
    .env({"HF_HOME": "/cache/hf", "HF_HUB_ENABLE_HF_TRANSFER": "0"})
)


def _features(out):
    import torch

    return out if torch.is_tensor(out) else out.pooler_output


@app.cls(gpu=GPU, image=image, volumes={"/cache": hf_cache}, timeout=3600, max_containers=4)
class Embedder:
    @modal.enter()
    def load(self):
        import requests
        import torch
        from transformers import AutoModel, AutoProcessor

        self.torch = torch
        self.model = AutoModel.from_pretrained(MODEL_ID, torch_dtype=torch.float16).cuda().eval()
        self.processor = AutoProcessor.from_pretrained(MODEL_ID, use_fast=True)
        hf_cache.commit()
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
        torch = self.torch
        out = []
        for i in range(0, len(imgs), BATCH):
            inputs = self.processor(images=imgs[i : i + BATCH], return_tensors="pt").to("cuda")
            with torch.inference_mode():
                f = _features(self.model.get_image_features(pixel_values=inputs["pixel_values"].half()))
            f = torch.nn.functional.normalize(f.float(), dim=-1)
            out.append(f.half().cpu().numpy())
        return out

    @modal.method()
    def embed_rows(self, rows: list[tuple[int, str]]) -> dict:
        """rows: [(block_id, thumb_url)] -> ids, emb (n x DIM f16), width, height, failed ids."""
        import numpy as np

        t0 = time.time()
        with ThreadPoolExecutor(DOWNLOAD_CONCURRENCY) as pool:
            imgs = list(pool.map(self._download, [u for _, u in rows]))
        t1 = time.time()
        ok = [(bid, im) for (bid, _), im in zip(rows, imgs) if im is not None]
        failed = [bid for (bid, _), im in zip(rows, imgs) if im is None]
        emb = (
            np.concatenate(self._embed_images([im for _, im in ok]))
            if ok
            else np.zeros((0, DIM), np.float16)
        )
        t2 = time.time()
        return {
            "ids": np.array([bid for bid, _ in ok], np.int64),
            "emb": emb,
            "width": np.array([im.width for _, im in ok], np.int32),
            "height": np.array([im.height for _, im in ok], np.int32),
            "failed": failed,
            "download_s": t1 - t0,
            "embed_s": t2 - t1,
        }

    @modal.method()
    def embed_texts(self, texts: list[str]):
        torch = self.torch
        inputs = self.processor(
            text=[t.lower() for t in texts], padding="max_length", max_length=64, truncation=True, return_tensors="pt"
        ).to("cuda")
        with torch.inference_mode():
            f = _features(self.model.get_text_features(**inputs))
        return torch.nn.functional.normalize(f.float(), dim=-1).half().cpu().numpy()

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
def bench(n: int = 2048):
    print(f"{GPU}: {Embedder().bench.remote(n):.0f} images/sec (preprocess + fp16 forward)")

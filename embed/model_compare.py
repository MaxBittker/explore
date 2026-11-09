"""Compare candidate search encoders on a sample of river images (Modal GPU).

    modal run embed/model_compare.py --sample bench_sample.npz   # ids, thumbs (from the DB)

1. fetch: downloads each thumb once (polite: 4 containers x 16), keeps a
   512px JPEG for embedding and a 128px JPEG for contact sheets.
2. embed: one GPU container per model embeds every image plus QUERIES with that
   model's own preprocessing and writes modelcmp/<name>.npz to the Volume.
Fetch the results with: modal volume get river-embeddings modelcmp <dir>
"""

import io
import pickle
from concurrent.futures import ThreadPoolExecutor

import modal

USER_AGENT = "river-embedder/1.0 (+https://river.maxbittker.com)"
CHUNK = 5000

MODELS = {
    # name: (kind, hf id)
    "siglip2-so400m-384": ("siglip", "google/siglip2-so400m-patch16-384"),
    "siglip2-large-256": ("siglip", "google/siglip2-large-patch16-256"),
    "siglip2-base-256": ("siglip", "google/siglip2-base-patch16-256"),
    "siglip2-base-224": ("siglip", "google/siglip2-base-patch16-224"),
    "mobileclip2-b": ("openclip", "hf-hub:timm/MobileCLIP2-B-OpenCLIP"),
    "mobileclip2-s2": ("openclip", "hf-hub:timm/MobileCLIP2-S2-OpenCLIP"),
}

QUERIES = [
    "stained glass window",
    "brutalist concrete architecture",
    "a hand-drawn map",
    "japanese woodblock print of waves",
    "fog over a mountain landscape",
    "cool sword",
    "risograph poster",
    "swiss style typography poster",
    "a cat sleeping",
    "interior with lots of plants",
    "90s web design",
    "diagram of a machine",
    "red",
    "melancholy",
    "people dancing",
    "a chair",
    "textile pattern",
    "screenshot of a tweet",
    "city at night",
    "pixel art",
    "old family photograph",
    "sculpture made of found objects",
    "handwritten notes",
    "a bowl of noodles",
]

app = modal.App("river-model-compare")
hf_cache = modal.Volume.from_name("river-hf-cache", create_if_missing=True)
results = modal.Volume.from_name("river-embeddings", create_if_missing=True)

image = (
    modal.Image.debian_slim(python_version="3.12")
    .pip_install(
        "torch==2.8.0",
        "torchvision==0.23.0",
        "transformers>=4.56,<5",
        "open_clip_torch>=3.2",
        "timm>=1.0.20",
        "sentencepiece",
        "protobuf",
        "pillow",
        "requests",
        "numpy",
    )
    .env({"HF_HOME": "/cache/hf"})
)


@app.function(image=image, cpu=4, volumes={"/out": results}, timeout=3600, max_containers=4)
def fetch(k: int, thumbs: list[str]):
    import requests
    from PIL import Image

    sess = requests.Session()
    sess.headers["User-Agent"] = USER_AGENT

    def one(url):
        try:
            im = Image.open(io.BytesIO(sess.get(url, timeout=30).content))
            im.seek(0)
            im = im.convert("RGB")
            big = im.copy()
            big.thumbnail((512, 512))
            small = im.copy()
            small.thumbnail((128, 128))
            out = []
            for x, q in ((big, 92), (small, 75)):
                b = io.BytesIO()
                x.save(b, "JPEG", quality=q)
                out.append(b.getvalue())
            return tuple(out)
        except Exception:
            return None

    import os

    with ThreadPoolExecutor(16) as pool:
        jpegs = list(pool.map(one, thumbs))
    os.makedirs("/out/modelcmp", exist_ok=True)
    with open(f"/out/modelcmp/images_{k:03d}.pkl", "wb") as f:
        pickle.dump(jpegs, f)
    results.commit()
    return sum(j is not None for j in jpegs)


@app.function(image=image, gpu="A10G", volumes={"/cache": hf_cache, "/out": results}, timeout=3600)
def embed(name: str):
    import numpy as np
    import torch
    from PIL import Image

    kind, hf_id = MODELS[name]
    import glob

    jpegs = []
    for p in sorted(glob.glob("/out/modelcmp/images_*.pkl")):
        with open(p, "rb") as f:
            jpegs += pickle.load(f)
    ok = [i for i, j in enumerate(jpegs) if j is not None]

    if kind == "siglip":
        from transformers import AutoModel, AutoProcessor

        model = AutoModel.from_pretrained(hf_id, dtype=torch.float16).cuda().eval()
        proc = AutoProcessor.from_pretrained(hf_id, use_fast=True)

        def feats(out):
            return out if torch.is_tensor(out) else out.pooler_output

        def enc_images(imgs):
            px = proc(images=imgs, return_tensors="pt")["pixel_values"].cuda().half()
            return feats(model.get_image_features(pixel_values=px))

        def enc_texts(texts):
            ids = proc(text=[t.lower() for t in texts], padding="max_length", max_length=64,
                       truncation=True, return_tensors="pt")["input_ids"].cuda()
            return feats(model.get_text_features(input_ids=ids))
    else:
        import open_clip

        from timm.utils import reparameterize_model as reparameterize  # merge MobileOne branches

        model, _, preprocess = open_clip.create_model_and_transforms(hf_id)
        model = reparameterize(model.cuda().eval())
        tok = open_clip.get_tokenizer(hf_id)

        def enc_images(imgs):
            # fp32: under fp16 autocast MobileCLIP2-S2's text tower collapsed
            # every query to nearly the same vector.
            return model.encode_image(torch.stack([preprocess(im) for im in imgs]).cuda())

        def enc_texts(texts):
            return model.encode_text(tok(texts).cuda())

    hf_cache.commit()

    def norm(f):
        return torch.nn.functional.normalize(f.float(), dim=-1).cpu().numpy().astype(np.float16)

    out = []
    with torch.inference_mode():
        for s in range(0, len(ok), 128):
            imgs = [Image.open(io.BytesIO(jpegs[i][0])).convert("RGB") for i in ok[s : s + 128]]
            out.append(norm(enc_images(imgs)))
        q = norm(enc_texts(QUERIES))
    emb = np.zeros((len(jpegs), out[0].shape[1]), np.float16)
    emb[ok] = np.concatenate(out)
    np.savez(f"/out/modelcmp/{name}.npz", emb=emb, ok=np.array(ok), queries=np.array(QUERIES), qemb=q)
    results.commit()
    return name, emb.shape


@app.local_entrypoint()
def main(sample: str, models: str = ",".join(MODELS), skip_fetch: bool = False):
    import numpy as np

    if not skip_fetch:
        thumbs = [str(t) for t in np.load(sample)["thumbs"]]
        chunks = [thumbs[i : i + CHUNK] for i in range(0, len(thumbs), CHUNK)]
        n = sum(fetch.starmap(enumerate(chunks)))
        print(f"fetched {n}/{len(thumbs)}")
    for name, shape in embed.map(models.split(",")):
        print(name, shape)

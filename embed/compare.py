"""Side-by-side neighbors: old CLIP B/32 (from the DB) vs SigLIP 2, within a sample.

    uv run --with numpy --with pillow --with requests python embed/compare.py sample.csv sample_siglip2.npz out.png
"""

import csv
import io
import json
import sys
from concurrent.futures import ThreadPoolExecutor

import numpy as np
import requests
from PIL import Image, ImageDraw

K = 7
TILE = 120
N_IMAGE_QUERIES = 5


def main(sample_csv, npz_path, out_png, seed=3):
    csv.field_size_limit(10**7)
    with open(sample_csv) as f:
        rows = {int(r["id"]): r for r in csv.DictReader(f)}
    d = np.load(npz_path)
    ids = d["ids"]
    new = d["emb"].astype(np.float32)
    old = np.array([json.loads(rows[i]["embedding"]) for i in ids], np.float32)
    old /= np.linalg.norm(old, axis=1, keepdims=True)
    thumbs = [rows[i]["thumb"] for i in ids]

    rng = np.random.default_rng(seed)
    qidx = rng.choice(len(ids), N_IMAGE_QUERIES, replace=False)

    lines = []  # (label, [indices]) ; first index highlighted as query
    for q in qidx:
        for name, E in (("old CLIP B/32", old), ("SigLIP 2", new)):
            sims = E @ E[q]
            nn = [i for i in np.argsort(-sims) if i != q][:K]
            lines.append((name, [q] + nn))
    for text, qe in zip(d["queries"], d["qemb"].astype(np.float32)):
        nn = list(np.argsort(-(new @ qe))[: K + 1])
        lines.append((f'SigLIP 2 text: "{text}"', nn))

    needed = sorted({i for _, l in lines for i in l})
    sess = requests.Session()
    sess.headers["User-Agent"] = "river-embedder/1.0 (+https://river.maxbittker.com)"

    def fetch(i):
        try:
            im = Image.open(io.BytesIO(sess.get(thumbs[i], timeout=20).content)).convert("RGB")
            im.thumbnail((TILE, TILE))
            return i, im
        except Exception:
            return i, Image.new("RGB", (TILE, TILE), "gray")

    with ThreadPoolExecutor(8) as pool:
        tiles = dict(pool.map(fetch, needed))

    label_h = 18
    W = (K + 1) * (TILE + 4) + 4
    H = len(lines) * (TILE + label_h + 6) + 4
    sheet = Image.new("RGB", (W, H), "white")
    draw = ImageDraw.Draw(sheet)
    y = 4
    for li, (label, idxs) in enumerate(lines):
        is_text = label.startswith("SigLIP 2 text")
        draw.text((6, y + 2), label, fill="black")
        y += label_h
        for j, i in enumerate(idxs):
            x = 4 + j * (TILE + 4)
            t = tiles[i]
            sheet.paste(t, (x + (TILE - t.width) // 2, y + (TILE - t.height) // 2))
            if j == 0 and not is_text:
                draw.rectangle([x, y, x + TILE - 1, y + TILE - 1], outline="red", width=3)
        y += TILE + 6
        if not is_text and li % 2 == 1:
            draw.line([(0, y - 3), (W, y - 3)], fill="#999", width=2)
    sheet.save(out_png)
    print(f"wrote {out_png} ({W}x{H}); overlap of top-{K} sets old vs new:")
    for q in qidx:
        a = set(np.argsort(-(old @ old[q]))[1 : K + 1])
        b = set(np.argsort(-(new @ new[q]))[1 : K + 1])
        print(f"  block {ids[q]}: {len(a & b)}/{K}")


if __name__ == "__main__":
    main(*sys.argv[1:4])

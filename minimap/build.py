"""Build the minimap data from the image atlas (UMAP layout + k-means cells).

Reads the atlas working directory (thristy/work/map: U.npy, clu_fine.npy,
cent_fine.npy) and writes:

  minimap/cells.bin         for the server: each cell's centroid (int8) and
                            map position, used to find a block or a search
                            vector on the map and the cells nearest it.
  site/public/minimap.json  for the page: each cell's position and image
                            count.

    python minimap/build.py ~/workplace/thristy/work/map
"""
import json, os, struct, sys
import numpy as np

src = sys.argv[1]
repo = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

U = np.load(f"{src}/U.npy").astype(np.float64)
fine = np.load(f"{src}/clu_fine.npy")
cent = np.load(f"{src}/cent_fine.npy").astype(np.float32)
K, dim = cent.shape

# Frame the landmass: square box around the bulk of the points, small margin.
lo, hi = np.percentile(U, 0.2, axis=0), np.percentile(U, 99.8, axis=0)
span = (hi - lo).max() * 1.08
P = np.clip((U - (lo + hi) / 2) / span + 0.5, 0, 1 - 1e-9)

# Cell anchor: mean of the half of its points nearest their median, so a cell
# split across islands lands on its main one rather than in the sea between.
rng = np.random.default_rng(0)
order = np.argsort(fine, kind="stable")
bounds = np.searchsorted(fine[order], np.arange(K + 1))
anchors = np.zeros((K, 2), np.float32)
for c in range(K):
    mem = order[bounds[c]:bounds[c + 1]]
    p = P[mem] if len(mem) <= 4000 else P[rng.choice(mem, 4000, replace=False)]
    m = np.median(p, axis=0)
    d = ((p - m) ** 2).sum(1)
    anchors[c] = p[d <= np.median(d)].mean(0)

counts = np.diff(bounds)
cells = []
for c in range(K):
    cells += [int(round(anchors[c, 0] * 1000)), int(round(anchors[c, 1] * 1000)), int(counts[c])]
with open(f"{repo}/site/public/minimap.json", "w") as f:
    json.dump({"cells": cells}, f, separators=(",", ":"))

# Centroids as int8: on 20k blocks the nearest centroid came out the same as
# with float32.
C = cent / np.linalg.norm(cent, axis=1, keepdims=True)
q = np.round(C * (127 / np.abs(C).max())).astype(np.int8)
with open(f"{repo}/minimap/cells.bin", "wb") as f:
    f.write(b"RMAP1\0\0\0" + struct.pack("<II", K, dim))
    f.write(anchors.astype("<f4").tobytes())
    f.write(q.tobytes())
print("cells", K, "dim", dim)

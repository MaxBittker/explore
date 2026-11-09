"""Export the SigLIP 2 search encoder to weight-quantized ONNX for CPU serving (serve.py).

    modal run embed/export_onnx.py --rows val_rows.csv   # rows: id,thumb (no header)
    modal run embed/export_onnx.py --rows val_rows.csv --model base256 --bits 0

Writes text.onnx, vision.onnx, tokenizer.json, meta.json to the
`river-embeddings` Volume under onnx/<variant>/, then checks the quantized ONNX
pipeline (siglip_onnx.py) against the fp32 transformers model.
Fetch the result with: modal volume get river-embeddings onnx/<variant> <dir>
"""

import json
import os
from pathlib import Path

import modal

MODELS = {  # same keys as RIVER_MODEL in embed.py
    "so400m": "google/siglip2-so400m-patch16-384",
    "base256": "google/siglip2-base-patch16-256",
}


def variant_name(model: str, bits: int, acc: int) -> str:
    """bits=0: fp32 weights (token table still split out)."""
    if bits == 0:
        return f"siglip2-{model}-fp32split"
    return f"siglip2-{model}-w{bits}" + (f"-acc{acc}" if acc else "")


HERE = Path(__file__).parent

app = modal.App("river-onnx-export")
hf_cache = modal.Volume.from_name("river-hf-cache", create_if_missing=True)
results = modal.Volume.from_name("river-embeddings", create_if_missing=True)

image = (
    modal.Image.debian_slim(python_version="3.12")
    .pip_install(
        "torch==2.8.0",
        "torchvision==0.23.0",
        "transformers>=4.56,<5",
        "sentencepiece",
        "protobuf",
        "pillow",
        "numpy",
        "requests",
        "onnx==1.19.0",
        "onnxruntime==1.23.0",
        "tokenizers",
    )
    .env({"HF_HOME": "/cache/hf"})
    .add_local_file(HERE / "siglip_onnx.py", "/root/siglip_onnx.py")
)

QUERIES = [
    "stained glass window",
    "cool sword",
    "brutalist concrete architecture",
    "a hand-drawn map",
    "japanese woodblock print of waves",
    "Obama eating ice cream",
    "fog over a mountain landscape",
    "a very long query " + "with many many words " * 20,
]


def _features(out):
    import torch

    return out if torch.is_tensor(out) else out.pooler_output


def split_token_embedding(m, npy_path):
    """Move the 256k x 1152 token-embedding table out of the text graph.

    The Gather that reads it is replaced by a graph input, token_embeds, and the
    table is saved as fp16 .npy. serve.py memory-maps it and looks up the ~64
    rows a query needs, instead of holding 1.2 GB of fp32 weights in RAM.
    """
    import numpy as np
    from onnx import TensorProto, helper, numpy_helper

    g = m.graph
    inits = {i.name: i for i in g.initializer}
    gather = max(
        (n for n in g.node if n.op_type == "Gather" and n.input[0] in inits),
        key=lambda n: np.prod(inits[n.input[0]].dims),
    )
    table = inits[gather.input[0]]
    vocab, dim = table.dims
    print(f"  token embedding {gather.input[0]}: {vocab} x {dim}")
    np.save(npy_path, numpy_helper.to_array(table).astype(np.float16))
    g.node.remove(gather)
    g.initializer.remove(table)
    g.input.append(helper.make_tensor_value_info(gather.output[0], TensorProto.FLOAT, ["batch", "tokens", dim]))
    # Rename the new input to something stable.
    old = gather.output[0]
    for n in g.node:
        for i, x in enumerate(n.input):
            if x == old:
                n.input[i] = "token_embeds"
    g.input[-1].name = "token_embeds"
    used = {x for n in g.node for x in n.input}
    for inp in [i for i in g.input if i.name not in used]:
        g.input.remove(inp)


@app.function(image=image, volumes={"/cache": hf_cache, "/out": results}, cpu=8, memory=49152, timeout=3600)
def export(rows: list[tuple[int, str]], bits: int = 8, block: int = 32, acc: int = 0, model_key: str = "so400m"):
    import io

    import numpy as np
    import requests
    import torch
    from PIL import Image
    from transformers import AutoModel, AutoProcessor

    from siglip_onnx import SiglipOnnx

    model_id = MODELS[model_key]
    model = AutoModel.from_pretrained(model_id, dtype=torch.float32).eval()
    proc = AutoProcessor.from_pretrained(model_id, use_fast=True)
    size = int(model.config.vision_config.image_size)

    class Text(torch.nn.Module):
        def __init__(self, m):
            super().__init__()
            self.m = m

        def forward(self, input_ids):
            return _features(self.m.get_text_features(input_ids=input_ids))

    class Vision(torch.nn.Module):
        def __init__(self, m):
            super().__init__()
            self.m = m

        def forward(self, pixel_values):
            return _features(self.m.get_image_features(pixel_values=pixel_values))

    import onnx
    from onnxruntime.quantization.matmul_nbits_quantizer import DefaultWeightOnlyQuantConfig, MatMulNBitsQuantizer

    fp32dir = Path(f"/out/onnx/siglip2-{model_key}-fp32")
    dst = Path(f"/out/onnx/{variant_name(model_key, bits, acc)}")
    fp32dir.mkdir(parents=True, exist_ok=True)
    dst.mkdir(parents=True, exist_ok=True)

    tok = proc.tokenizer
    for d in (fp32dir, dst):
        tok.backend_tokenizer.save(str(d / "tokenizer.json"))
        (d / "meta.json").write_text(json.dumps({
            "model_id": model_id, "dim": int(getattr(model.config.text_config, "projection_size", None) or model.config.text_config.hidden_size),
            "image_size": size,
            "pad_id": tok.pad_token_id, "pad_token": tok.pad_token,
        }))

    ids = proc(text=["x"], padding="max_length", max_length=64, truncation=True, return_tensors="pt")["input_ids"]
    pix = torch.zeros(1, 3, size, size)
    for name, mod, arg, inp in [("text", Text(model), ids, "input_ids"), ("vision", Vision(model), pix, "pixel_values")]:
        fp32 = fp32dir / f"{name}.onnx"
        if not fp32.exists():
            print(f"exporting {name}")
            raw = Path("/tmp/raw") / name / f"{name}.onnx"
            raw.parent.mkdir(parents=True, exist_ok=True)
            with torch.inference_mode():
                torch.onnx.export(
                    mod, (arg,), str(raw), input_names=[inp], output_names=["features"],
                    dynamic_axes={inp: {0: "batch"}, "features": {0: "batch"}}, opset_version=17, dynamo=False,
                )
            onnx.save(onnx.load(str(raw)), str(fp32), save_as_external_data=True,
                      all_tensors_to_one_file=True, location=f"{name}.onnx.data")
            results.commit()
        m = onnx.load(str(fp32))
        if name == "text":
            split_token_embedding(m, dst / "token_embedding.f16.npy")
        if bits == 0:
            onnx.save(m, str(dst / f"{name}.onnx"))
        else:
            # Weight-only: weights are block-quantized; activations stay fp32
            # unless acc=4 (MatMulNBits then quantizes activations per block,
            # which tolerates outliers far better than quantize_dynamic).
            # SigLIP's activation outliers make full int8 (quantize_dynamic) unusable.
            print(f"quantizing {name} (weight-only {bits}-bit, block {block}, accuracy_level {acc})")
            ops = ("MatMul",)
            cfg = DefaultWeightOnlyQuantConfig(
                block_size=block, is_symmetric=True, op_types_to_quantize=ops, bits=bits,
                accuracy_level=acc or None,
            )
            q = MatMulNBitsQuantizer(m, algo_config=cfg, op_types_to_quantize=ops)
            q.process()
            q.model.save_model_to_file(str(dst / f"{name}.onnx"), use_external_data_format=False)
        print(f"  {name}.onnx: {os.path.getsize(dst / f'{name}.onnx') / 1e6:.0f} MB")
    results.commit()

    # --- validate the quantized pipeline against fp32 transformers ---
    enc = SiglipOnnx(str(dst), threads=8)
    enc32 = SiglipOnnx(str(fp32dir), threads=8)

    ref_ids = proc(text=[q.lower() for q in QUERIES], padding="max_length", max_length=64, truncation=True,
                   return_tensors="pt")["input_ids"].numpy()
    print("token ids match transformers:", bool((enc.token_ids(QUERIES) == ref_ids).all()))

    def cos(a, b):
        a = a / np.linalg.norm(a, axis=-1, keepdims=True)
        b = b / np.linalg.norm(b, axis=-1, keepdims=True)
        return (a * b).sum(-1)

    with torch.inference_mode():
        ref_t = _features(model.get_text_features(input_ids=torch.from_numpy(ref_ids))).numpy()
    for label, e in (("onnx fp32", enc32), (variant_name(model_key, bits, acc), enc)):
        ct = cos(e.embed_texts(QUERIES), ref_t)
        print(f"text   {label} vs torch fp32 cosine: min {ct.min():.4f} mean {ct.mean():.4f}")

    imgs, kept = [], []
    for bid, url in rows:
        try:
            r = requests.get(url, timeout=20, headers={"User-Agent": "river-embedder/1.0"})
            im = Image.open(io.BytesIO(r.content))
            im.seek(0)
            imgs.append(im.convert("RGB"))
            kept.append(bid)
        except Exception as e:
            print("skip", bid, e)
    with torch.inference_mode():
        px = proc(images=imgs, return_tensors="pt")["pixel_values"]
        ref_i = _features(model.get_image_features(pixel_values=px)).numpy()
    ours_px = np.stack([__import__("siglip_onnx").preprocess_image(im, size) for im in imgs])
    print(f"pixel max abs diff vs processor: {np.abs(ours_px - px.numpy()).max():.4f} "
          f"(mean {np.abs(ours_px - px.numpy()).mean():.5f})")
    for label, e in (("onnx fp32", enc32), (variant_name(model_key, bits, acc), enc)):
        ci = cos(e.embed_images(imgs), ref_i)
        print(f"image  {label} vs torch fp32 cosine: min {ci.min():.4f} mean {ci.mean():.4f} over {len(imgs)} images")

    # Does the image ranking survive? For each text query, top-5 overlap.
    oi = enc.embed_images(imgs)
    ot = enc.embed_texts(QUERIES[:-1])
    rt = ref_t[:-1] / np.linalg.norm(ref_t[:-1], axis=-1, keepdims=True)
    ri = ref_i / np.linalg.norm(ref_i, axis=-1, keepdims=True)
    overlap = [len(set(np.argsort(-(ot @ oi.T)[k])[:5]) & set(np.argsort(-(rt @ ri.T)[k])[:5])) / 5 for k in range(len(ot))]
    print(f"text->image top-5 overlap {variant_name(model_key, bits, acc)} vs torch fp32: {np.mean(overlap):.2f}")
    return kept, enc.embed_images(imgs).tolist()


@app.local_entrypoint()
def main(rows: str, bits: int = 8, block: int = 32, acc: int = 0, model: str = "so400m"):
    import csv

    with open(rows) as f:
        rs = [(int(r[0]), r[1]) for r in csv.reader(f) if len(r) == 2]
    kept, vecs = export.remote(rs, bits, block, acc, model)
    out = Path(rows).with_suffix(f".{variant_name(model, bits, acc)}.json")
    out.write_text(json.dumps({"ids": kept, "vecs": vecs}))
    print("wrote", out)

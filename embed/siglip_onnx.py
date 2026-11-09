"""CPU inference for the SigLIP 2 search encoder, from the ONNX export.

Used by serve.py on the VPS and by export_onnx.py to validate the export, so
preprocessing lives in exactly one place. Must stay equivalent to the
transformers processor used for the stored embeddings (embed.py):
  text:  lowercase, Gemma tokenizer with EOS, pad/truncate to 64, no mask
  image: RGB, resize to image_size (meta.json) square, bilinear, scale to [-1, 1]
Dependencies: numpy, pillow, onnxruntime, tokenizers.
"""

import json
import os

import numpy as np
from PIL import Image

MAX_TEXT_TOKENS = 64


def preprocess_image(img: Image.Image, size: int) -> np.ndarray:
    """PIL image -> (3, size, size) float32 in [-1, 1]."""
    img = img.convert("RGB").resize((size, size), Image.BILINEAR)
    x = np.asarray(img, dtype=np.float32) / 255.0
    x = (x - 0.5) / 0.5
    return x.transpose(2, 0, 1)


class SiglipOnnx:
    def __init__(self, model_dir: str, threads: int = 2):
        import onnxruntime as ort
        from tokenizers import Tokenizer

        with open(os.path.join(model_dir, "meta.json")) as f:
            meta = json.load(f)
        self.dim = meta["dim"]
        self.image_size = meta.get("image_size", 384)  # exports before image_size was recorded were so400m-384

        opts = ort.SessionOptions()
        opts.intra_op_num_threads = threads
        opts.inter_op_num_threads = 1
        opts.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
        providers = ["CPUExecutionProvider"]
        self.text = ort.InferenceSession(os.path.join(model_dir, "text.onnx"), opts, providers=providers)
        self.vision = ort.InferenceSession(os.path.join(model_dir, "vision.onnx"), opts, providers=providers)
        self.text_inputs = {i.name for i in self.text.get_inputs()}

        # Exports from export_onnx.py keep the token-embedding table outside the
        # graph (token_embeds input); mmap it so only looked-up rows are paged in.
        table = os.path.join(model_dir, "token_embedding.f16.npy")
        self.token_embedding = np.load(table, mmap_mode="r") if os.path.exists(table) else None

        self.tokenizer = Tokenizer.from_file(os.path.join(model_dir, "tokenizer.json"))
        self.tokenizer.enable_truncation(MAX_TEXT_TOKENS)
        self.tokenizer.enable_padding(length=MAX_TEXT_TOKENS, pad_id=meta["pad_id"], pad_token=meta["pad_token"])

    def token_ids(self, texts: list[str]) -> np.ndarray:
        encs = self.tokenizer.encode_batch([t.lower() for t in texts])
        return np.array([e.ids for e in encs], dtype=np.int64)

    def embed_texts(self, texts: list[str]) -> np.ndarray:
        ids = self.token_ids(texts)
        feed = {}
        if "input_ids" in self.text_inputs:
            feed["input_ids"] = ids
        if "token_embeds" in self.text_inputs:
            feed["token_embeds"] = self.token_embedding[ids].astype(np.float32)
        (f,) = self.text.run(None, feed)
        return _normalize(f)

    def embed_images(self, imgs: list[Image.Image]) -> np.ndarray:
        x = np.stack([preprocess_image(im, self.image_size) for im in imgs])
        (f,) = self.vision.run(None, {"pixel_values": x})
        return _normalize(f)


def _normalize(f: np.ndarray) -> np.ndarray:
    f = f.astype(np.float32)
    return f / np.linalg.norm(f, axis=-1, keepdims=True)

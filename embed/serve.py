"""Search-time SigLIP 2 encoder for the VPS (CPU, onnxruntime).

    python serve.py --model /root/river-embedder/model --listen 127.0.0.1:6100

POST /embed {"text": "..."} or {"image": "<base64>"} -> {"vector": [dim floats]}
GET  /health

The Go server calls this (EMBED_URL=http://127.0.0.1:6100). It has no auth,
so only bind it to loopback. Requests run one at a time: the box has 4 cores
shared with Postgres, so --threads caps how many each inference may use.
Model files come from export_onnx.py; deps: onnxruntime tokenizers numpy pillow.
"""

import argparse
import base64
import io
import json
import logging
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from PIL import Image

from siglip_onnx import SiglipOnnx

MAX_BODY = 12 * 1024 * 1024  # base64 of an 8 MB image, plus slack
MAX_TEXT = 500
Image.MAX_IMAGE_PIXELS = 50_000_000  # refuse decompression bombs

log = logging.getLogger("embed")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", required=True)
    ap.add_argument("--listen", default="127.0.0.1:6100")
    ap.add_argument("--threads", type=int, default=2)
    args = ap.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(message)s")

    t = time.time()
    enc = SiglipOnnx(args.model, threads=args.threads)
    enc.embed_texts(["warmup"])
    log.info("model loaded in %.1fs", time.time() - t)
    lock = threading.Lock()

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path == "/health":
                return self._json(200, {"ok": True})
            self._json(404, {"detail": "not found"})

        def do_POST(self):
            if self.path != "/embed":
                return self._json(404, {"detail": "not found"})
            n = int(self.headers.get("Content-Length") or 0)
            if n > MAX_BODY:
                return self._json(413, {"detail": "too large"})
            try:
                body = json.loads(self.rfile.read(n))
            except Exception:
                return self._json(400, {"detail": "bad json"})
            text, img_b64 = body.get("text"), body.get("image")
            t0 = time.time()
            if isinstance(text, str) and text.strip():
                with lock:
                    v = enc.embed_texts([text.strip()[:MAX_TEXT]])[0]
                kind = "text"
            elif isinstance(img_b64, str) and img_b64:
                try:
                    img = Image.open(io.BytesIO(base64.b64decode(img_b64, validate=True)))
                    img.seek(0)  # first frame of gifs
                    img = img.convert("RGB")
                except Exception:
                    return self._json(400, {"detail": "unreadable image"})
                with lock:
                    v = enc.embed_images([img])[0]
                kind = "image"
            else:
                return self._json(400, {"detail": "send text or image"})
            log.info("%s embedded in %.0fms", kind, (time.time() - t0) * 1000)
            self._json(200, {"vector": v.tolist()})

        def _json(self, code, obj):
            buf = json.dumps(obj).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(buf)))
            self.end_headers()
            self.wfile.write(buf)

        def log_message(self, *a):
            pass

    host, port = args.listen.rsplit(":", 1)
    srv = ThreadingHTTPServer((host, int(port)), Handler)
    log.info("listening on %s", args.listen)
    srv.serve_forever()


if __name__ == "__main__":
    main()

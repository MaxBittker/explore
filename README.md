# river

A visual connection engine: browse Are.na images by embedding similarity.
Live at <https://river.maxbittker.com>.

- `main.go`, `queries.go`: Go/Echo server. Serves `site/dist` and the JSON API
  (`GET /neighbors`, `POST /vectorNeighbors`, `POST /embed`, `POST /api/flag`)
  on `:6000` by default.
- `embed.go`: `POST /embed` turns a search (form field `text`, or an `image`
  file) into a vector by calling the local encoder, `embed/serve.py`.
- `schema.sql`: Postgres + pgvector (0.8 or newer) schema. Embeddings are
  `halfvec(N)` with an HNSW inner-product index; the server reads N from the
  database at startup (1152 for SigLIP 2 so400m, 768 for SigLIP 2 base).
- `site/`: React/Vite frontend.

## Running locally

```sh
psql "$DATABASE_URL" -f schema.sql      # Postgres with the pgvector extension
(cd site && npm ci && npm run build)    # the server renders site/dist/index.html
DATABASE_URL=postgres://... go run . run
```

Environment:

- `DATABASE_URL`: Postgres connection string.
- `FLAG_PASSWORD`: admin password; flags sent with it apply immediately.
  Visitor flags apply once enough distinct visitors agree (see
  `crowdThresholds` in `flags.go`).
- `LISTEN_ADDR` (or `--listen`): listen address, default `:6000`.
- `EMBED_URL` (optional `EMBED_TOKEN`): the search encoder, e.g.
  `http://127.0.0.1:6100`. Without it, search returns 503.

The frontend calls the production API by default. To point `npm run dev` at a
local server, run the server with `LISTEN_ADDR=:8787` and put
`VITE_API_BASE=http://localhost:8787` in `site/.env.local`. (Browsers block
port 6000, so the default port can't be used from a browser.)

## Search encoder

`embed/serve.py` runs SigLIP 2 on CPU with onnxruntime, from the export made
by `embed/export_onnx.py` (Modal is only used for the export and for bulk
embedding, `embed/embed.py`). On the VPS it lives in `/root/river-embedder`
(`run.sh`, screen `river-embedder`, 127.0.0.1:6100). The encoder must be the
model that produced the stored embeddings.

The test app uses SigLIP 2 base at 256px (`RIVER_MODEL=base256`, 768-d,
fp32 export: `--model base256 --bits 0`): about 0.1s per text query and 0.5s
per image on the VPS, against 1.4s and 9-15s for so400m. On a 20k-image
sample its neighbors share an Are.na channel with the query 10.1% of the time
versus so400m's 12.0% (`embed/model_compare.py`).

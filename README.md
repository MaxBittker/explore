# river

A visual connection engine: browse Are.na images by embedding similarity.
Live at <https://river.maxbittker.com>.

- `main.go`, `queries.go`: Go/Echo server. Serves `site/dist` and the JSON API
  (`GET /neighbors`, `POST /vectorNeighbors`, `POST /api/flag`) on `:6000` by
  default.
- `schema.sql`: Postgres + pgvector schema.
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

The frontend calls the production API by default. To point `npm run dev` at a
local server, run the server with `LISTEN_ADDR=:8787` and put
`VITE_API_BASE=http://localhost:8787` in `site/.env.local`. (Browsers block
port 6000, so the default port can't be used from a browser.)

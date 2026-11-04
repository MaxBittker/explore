# river

A visual connection engine: browse Are.na images by embedding similarity.
Live at <https://river.maxbittker.com>.

- `main.go`, `queries.go`: Go/Echo server. Serves `site/dist` and the JSON API
  (`GET /neighbors`, `POST /vectorNeighbors`, `POST /api/flag`) on `:6000`.
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
- `FLAG_PASSWORD`: enables moderation flags (nsfw / boring / cool). Moderation
  is disabled when unset.

The frontend calls the production API by default. To point `npm run dev` at a
local server, put `VITE_API_BASE=http://localhost:6000` in `site/.env.local`.

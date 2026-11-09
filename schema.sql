-- pgvector >= 0.8 (halfvec, parallel HNSW builds, iterative index scans).
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS blocks (
    id INTEGER PRIMARY KEY,
    title TEXT,
    thumb TEXT,
    original TEXT,
    display TEXT,
    square TEXT,
    width INTEGER,
    height INTEGER,
    votes INTEGER DEFAULT 0,
    phash BIGINT DEFAULT null,
    nsfw BOOLEAN DEFAULT null,
    gif BOOLEAN DEFAULT null,
    -- L2-normalized (search orders by inner product, <#>). Half precision
    -- halves the storage and HNSW index size with no visible loss in search
    -- quality. The dimension is the encoder's (SigLIP 2 so400m: 1152; base:
    -- 768, as in river2m_base); the server reads it at startup.
    embedding halfvec(1152)
);

-- For bulk loads, drop this first and rebuild it afterwards in parallel, with
-- maintenance_work_mem large enough to hold the graph (see make_river1m.sh).
CREATE INDEX IF NOT EXISTS blocks_embedding_hnsw
    ON blocks USING hnsw (embedding halfvec_ip_ops) WITH (m = 16, ef_construction = 64);

CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY,
    slug TEXT,
    title TEXT,
    votes INTEGER DEFAULT 0,
    nsfw BOOLEAN DEFAULT null,
    scraped BOOLEAN DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS connections (
    id SERIAL PRIMARY KEY,
    block_id INTEGER REFERENCES blocks(id),
    channel_id INTEGER REFERENCES channels(id),
    UNIQUE (block_id, channel_id)
);

CREATE TABLE IF NOT EXISTS flags (
    id SERIAL PRIMARY KEY,
    block_id INTEGER REFERENCES blocks(id),
    flag text,
    -- pseudonymous visitor id (hashed IP); NULL for flags recorded before
    -- crowd thresholds existed
    voter text
);

-- migration for databases created before the voter column
ALTER TABLE flags ADD COLUMN IF NOT EXISTS voter text;
CREATE INDEX IF NOT EXISTS flags_block_flag ON flags (block_id, flag);

CREATE TABLE IF NOT EXISTS clusters (
    id SERIAL PRIMARY KEY,
    cluster_id INTEGER,
    block_id INTEGER REFERENCES blocks(id)
);
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
    -- L2-normalized (search orders by inner product, <#>). The dimension must
    -- match embeddingDim in queries.go.
    embedding vector(512)
);

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
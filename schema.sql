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
    embedding vector(512)
);

CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY,
    slug TEXT,
    title TEXT,
    votes INTEGER DEFAULT 0,
    nsfw BOOLEAN DEFAULT null scraped BOOLEAN DEFAULT FALSE
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
    flag text
);
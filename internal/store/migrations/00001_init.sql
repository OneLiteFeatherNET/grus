-- +goose Up
CREATE TABLE pools (
    name        TEXT PRIMARY KEY,
    spec        TEXT    NOT NULL,
    generation  INTEGER NOT NULL,
    maintenance INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL
);

-- No foreign key to pools: the store never deletes instances, the controller decides what happens to them.
CREATE TABLE instances (
    id              TEXT PRIMARY KEY,
    pool            TEXT    NOT NULL,
    role            TEXT    NOT NULL,
    pool_generation INTEGER NOT NULL,
    host            TEXT    NOT NULL DEFAULT '',
    driver_ref      TEXT    NOT NULL DEFAULT '',
    address         TEXT    NOT NULL DEFAULT '',
    state           TEXT    NOT NULL,
    state_reason    TEXT    NOT NULL DEFAULT '',
    state_since     TEXT    NOT NULL,
    players         INTEGER NOT NULL DEFAULT 0,
    max_players     INTEGER NOT NULL DEFAULT 0,
    labels          TEXT    NOT NULL DEFAULT 'null',
    allocated_at    TEXT,
    last_heartbeat  TEXT,
    token_hash      TEXT    NOT NULL DEFAULT '',
    created_at      TEXT    NOT NULL
);

CREATE INDEX instances_pool ON instances (pool);

-- Instances without a token keep an empty hash, so only non-empty hashes must be unique.
CREATE UNIQUE INDEX instances_token_hash ON instances (token_hash) WHERE token_hash <> '';

CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- +goose Down
DROP TABLE meta;
DROP TABLE instances;
DROP TABLE pools;

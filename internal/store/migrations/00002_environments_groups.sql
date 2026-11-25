-- +goose Up
ALTER TABLE pools ADD COLUMN source TEXT NOT NULL DEFAULT 'api';

CREATE TABLE environments (
    name       TEXT PRIMARY KEY,
    spec       TEXT    NOT NULL,
    source     TEXT    NOT NULL DEFAULT 'api',
    generation INTEGER NOT NULL,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);

CREATE TABLE groups (
    name       TEXT PRIMARY KEY,
    spec       TEXT    NOT NULL,
    source     TEXT    NOT NULL DEFAULT 'api',
    generation INTEGER NOT NULL,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);

-- +goose Down
DROP TABLE groups;
DROP TABLE environments;
ALTER TABLE pools DROP COLUMN source;

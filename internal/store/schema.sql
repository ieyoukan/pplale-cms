CREATE TABLE IF NOT EXISTS users (
    discord_id   TEXT PRIMARY KEY,
    display_name TEXT        NOT NULL DEFAULT '',
    role         TEXT        NOT NULL,
    added_by     TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash   TEXT PRIMARY KEY,
    discord_id   TEXT        NOT NULL,
    display_name TEXT        NOT NULL DEFAULT '',
    csrf_token   TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_discord_id_idx ON sessions (discord_id);
CREATE INDEX IF NOT EXISTS sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE IF NOT EXISTS submissions (
    id           BIGSERIAL PRIMARY KEY,
    discord_id   TEXT        NOT NULL,
    display_name TEXT        NOT NULL DEFAULT '',
    kind         TEXT        NOT NULL,
    card_id      TEXT        NOT NULL,
    card_name    TEXT        NOT NULL DEFAULT '',
    branch       TEXT        NOT NULL DEFAULT '',
    pr_number    INTEGER     NOT NULL,
    pr_url       TEXT        NOT NULL DEFAULT '',
    status       TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS submissions_pr_number_idx ON submissions (pr_number);

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
    branch       TEXT        NOT NULL DEFAULT '',
    pr_number    INTEGER     NOT NULL,
    pr_url       TEXT        NOT NULL DEFAULT '',
    status       TEXT        NOT NULL,
    -- []SubmissionCard: one PR can bundle several cards, possibly across
    -- different dataset files.
    cards        JSONB       NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS submissions_pr_number_idx ON submissions (pr_number);

-- カードを提出待ちで貯めておく下書き。画像は作成時点で WebP/OGP-PNG に
-- 変換済みのバイト列として持つ（変換は決定的なので提出時まで遅らせる理由がない）。
CREATE TABLE IF NOT EXISTS drafts (
    id           BIGSERIAL PRIMARY KEY,
    discord_id   TEXT        NOT NULL,
    display_name TEXT        NOT NULL DEFAULT '',
    kind         TEXT        NOT NULL,
    card_id      TEXT        NOT NULL DEFAULT '',
    name         TEXT        NOT NULL,
    fruit        TEXT        NOT NULL,
    description  TEXT        NOT NULL DEFAULT '',
    cost         INTEGER     NOT NULL DEFAULT 0,
    hp           INTEGER     NOT NULL DEFAULT 0,
    attack       INTEGER     NOT NULL DEFAULT 0,
    effect       TEXT,
    role         TEXT,
    sweet_type   TEXT,
    version      TEXT,
    taxonomy     JSONB       NOT NULL DEFAULT '{}',
    image_slug   TEXT        NOT NULL DEFAULT '',
    webp         BYTEA,
    ogp_png      BYTEA,
    source_bytes INTEGER     NOT NULL DEFAULT 0,
    source_type  TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS drafts_discord_id_idx ON drafts (discord_id);

-- Existing installations predate classification changes on drafts.
ALTER TABLE drafts ADD COLUMN IF NOT EXISTS taxonomy JSONB NOT NULL DEFAULT '{}';

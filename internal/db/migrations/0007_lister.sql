-- The seller's marketplace keys (Slabs & Singles now, more later), sent up
-- by a linked PC or entered on the website. Encrypted like the AI key.
CREATE TABLE account_market_keys (
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    market       TEXT NOT NULL CHECK (market IN ('slabs')),
    key_enc      BYTEA NOT NULL,
    key_last4    TEXT NOT NULL,
    settings     JSONB NOT NULL DEFAULT '{}', -- e.g. Slabs & Singles package/weight defaults from the PC
    source       TEXT NOT NULL CHECK (source IN ('pc', 'web')),
    device_id    UUID REFERENCES devices(id) ON DELETE SET NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, market)
);

-- Bulk Lister runs: a list of cards to put on a marketplace, worked through
-- in the background (photos are uploaded and each card listed in turn).
CREATE TABLE list_jobs (
    id           UUID PRIMARY KEY,
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    edition      TEXT NOT NULL,
    market       TEXT NOT NULL,
    options      JSONB NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'done', 'stopped')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at  TIMESTAMPTZ
);
CREATE INDEX list_jobs_account ON list_jobs (account_id, created_at DESC);

CREATE TABLE list_job_items (
    job_id       UUID NOT NULL REFERENCES list_jobs(id) ON DELETE CASCADE,
    position     INT NOT NULL,
    card_id      UUID NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'listed', 'updated', 'skipped', 'failed')),
    message      TEXT,
    name         TEXT,
    done_at      TIMESTAMPTZ,
    PRIMARY KEY (job_id, position)
);
CREATE INDEX list_job_items_pending ON list_job_items (job_id) WHERE status = 'pending';

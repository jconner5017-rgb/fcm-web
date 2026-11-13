-- The seller's own AI key (Claude or Gemini) for identifying cards from a
-- phone. Sent up by a linked PC (the key already saved in FireCardManager)
-- or entered on the website by a web-only seller. Encrypted with the
-- server's MFA_ENCRYPTION_KEY; never sent back to the browser.
CREATE TABLE account_ai_keys (
    account_id   UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    provider     TEXT NOT NULL CHECK (provider IN ('claude', 'gemini')),
    key_enc      BYTEA NOT NULL,
    key_last4    TEXT NOT NULL,
    model        TEXT,
    source       TEXT NOT NULL CHECK (source IN ('pc', 'web')),
    device_id    UUID REFERENCES devices(id) ON DELETE SET NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Phone identifications per account, for a simple hourly limit.
CREATE TABLE identify_log (
    id          BIGSERIAL PRIMARY KEY,
    account_id  UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    ok          BOOLEAN NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX identify_log_account ON identify_log (account_id, created_at);

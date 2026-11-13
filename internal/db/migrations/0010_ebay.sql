-- A seller's eBay connection made on the website (OAuth). Tokens are
-- encrypted with the server's key, like the other saved keys.
CREATE TABLE account_ebay (
    account_id          UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    refresh_enc         BYTEA NOT NULL,
    refresh_expires_at  TIMESTAMPTZ NOT NULL,
    access_enc          BYTEA,
    access_expires_at   TIMESTAMPTZ,
    ebay_user           TEXT NOT NULL DEFAULT '',
    connected_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One-time "state" values for the eBay sign-in round trip (10 minutes).
CREATE TABLE ebay_oauth_states (
    state       TEXT PRIMARY KEY,
    account_id  UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

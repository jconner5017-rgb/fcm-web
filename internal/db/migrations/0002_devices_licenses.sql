-- Linked desktop apps (one row per app on a PC) and the license keys they
-- were linked with.

CREATE TABLE devices (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id    UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,                    -- the PC's name, as the desktop reports it
    edition       TEXT NOT NULL CHECK (edition IN ('sports', 'tcg')),
    hwid          TEXT NOT NULL,
    token_hash    BYTEA NOT NULL UNIQUE,
    app_version   TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ
);
CREATE INDEX devices_account ON devices (account_id) WHERE revoked_at IS NULL;

-- What a license key proved when a PC linked. Keys are verified by their
-- signature (the same public key the apps use), so this is a record, not
-- the source of truth; the desktop re-sends its key on every link.
CREATE TABLE licenses (
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    edition      TEXT NOT NULL CHECK (edition IN ('sports', 'tcg')),
    hwid         TEXT NOT NULL,
    key_hash     BYTEA NOT NULL,
    expires_at   TIMESTAMPTZ,                       -- NULL: a legacy key with no expiry
    verified_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, edition, hwid)
);

-- Linking a PC: the desktop starts a request and shows a short code; the
-- seller approves it on the website while signed in; the desktop, polling
-- with its private secret, then collects its device token once.
CREATE TABLE device_links (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_code           TEXT NOT NULL UNIQUE,      -- e.g. KQ7M-2XHD, shown on the desktop
    poll_secret_hash    BYTEA NOT NULL,
    device_name         TEXT NOT NULL,
    edition             TEXT NOT NULL CHECK (edition IN ('sports', 'tcg')),
    hwid                TEXT NOT NULL,
    license_key         TEXT,                       -- kept only until the link finishes
    license_expires_at  TIMESTAMPTZ,
    license_state       TEXT NOT NULL CHECK (license_state IN ('valid', 'no_expiry', 'grace', 'expired', 'trial')),
    app_version         TEXT,
    expires_at          TIMESTAMPTZ NOT NULL,
    approved_by         UUID REFERENCES accounts(id) ON DELETE CASCADE,
    approved_at         TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

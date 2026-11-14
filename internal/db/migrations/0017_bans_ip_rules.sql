-- Bans: hashed identifiers of banned accounts (email, networks they used,
-- their PCs) so a banned seller can't just make a new account. Kept after
-- the account is deleted.
CREATE TABLE banned_identifiers (
    kind          TEXT NOT NULL CHECK (kind IN ('email', 'ip', 'hwid')),
    value_hash    TEXT NOT NULL,
    account_ref   UUID NOT NULL,
    account_email TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, value_hash)
);
CREATE INDEX banned_identifiers_account ON banned_identifiers (account_ref);
ALTER TABLE accounts ADD COLUMN banned_at TIMESTAMPTZ;

-- Networks the back office may be used from (none: any, 2FA still required).
CREATE TABLE admin_ip_rules (
    id         BIGSERIAL PRIMARY KEY,
    cidr       TEXT NOT NULL UNIQUE,
    label      TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

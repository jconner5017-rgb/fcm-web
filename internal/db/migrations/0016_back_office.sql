-- Back office: staff the owner grants access to, suspensions, staff notes,
-- an audit log of staff actions, and where each account signed up from.
ALTER TABLE accounts ADD COLUMN staff_role TEXT CHECK (staff_role IN ('support', 'manager'));
ALTER TABLE accounts ADD COLUMN suspended_reason TEXT;
ALTER TABLE accounts ADD COLUMN signup_ip TEXT;

CREATE TABLE admin_notes (
    id           BIGSERIAL PRIMARY KEY,
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    author_email TEXT NOT NULL,
    body         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX admin_notes_account ON admin_notes (account_id, created_at DESC);

CREATE TABLE admin_audit (
    id            BIGSERIAL PRIMARY KEY,
    actor_id      UUID,
    actor_email   TEXT NOT NULL,
    action        TEXT NOT NULL,
    account_id    UUID,              -- kept after the account is deleted
    account_email TEXT,
    detail        TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX admin_audit_time ON admin_audit (created_at DESC);
CREATE INDEX admin_audit_account ON admin_audit (account_id, created_at DESC);
CREATE INDEX login_events_ip ON login_events (ip, created_at DESC);
CREATE INDEX login_events_time ON login_events (created_at DESC);

-- Alerts staff marked as looked at (the key includes the count, so more of
-- the same activity raises it again).
CREATE TABLE admin_dismissed (
    key        TEXT PRIMARY KEY,
    by_email   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

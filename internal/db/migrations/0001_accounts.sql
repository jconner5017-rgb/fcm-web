-- FCM Web accounts, sign-in sessions, authenticator (2FA) and account email.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE accounts (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email              TEXT NOT NULL,
    password_hash      TEXT NOT NULL,
    email_verified_at  TIMESTAMPTZ,
    is_admin           BOOLEAN NOT NULL DEFAULT false,
    status             TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    -- Sync: 30 free days from sign-up, then the one-time $20 web add-on.
    sync_trial_ends_at TIMESTAMPTZ NOT NULL DEFAULT now() + interval '30 days',
    web_addon_at       TIMESTAMPTZ,
    web_addon_note     TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX accounts_email_key ON accounts (lower(email));

CREATE TABLE sessions (
    token_hash      BYTEA PRIMARY KEY,
    account_id      UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    -- A session made with the password alone, for an account with 2FA on,
    -- can only finish the authenticator step until this is set.
    mfa_ok          BOOLEAN NOT NULL DEFAULT false,
    expires_at      TIMESTAMPTZ NOT NULL,
    ip              TEXT,
    user_agent      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_account ON sessions (account_id);

CREATE TABLE account_mfa (
    account_id     UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    secret_enc     BYTEA NOT NULL,          -- AES-GCM sealed with MFA_ENCRYPTION_KEY
    confirmed_at   TIMESTAMPTZ,
    backup_hashes  TEXT[] NOT NULL DEFAULT '{}',
    failed_count   INT NOT NULL DEFAULT 0,
    locked_until   TIMESTAMPTZ,
    last_step      BIGINT NOT NULL DEFAULT 0, -- a code can't be used twice
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE login_events (
    id          BIGSERIAL PRIMARY KEY,
    account_id  UUID REFERENCES accounts(id) ON DELETE SET NULL,
    email       TEXT,
    ip          TEXT,
    user_agent  TEXT,
    success     BOOLEAN NOT NULL,
    reason      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE email_tokens (
    token_hash  BYTEA PRIMARY KEY,
    account_id  UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    purpose     TEXT NOT NULL CHECK (purpose IN ('verify', 'reset')),
    email       TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE email_outbox (
    id          BIGSERIAL PRIMARY KEY,
    account_id  UUID REFERENCES accounts(id) ON DELETE SET NULL,
    to_email    TEXT NOT NULL,
    subject     TEXT NOT NULL,
    body        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sent', 'failed')),
    attempts    INT NOT NULL DEFAULT 0,
    last_error  TEXT,
    send_after  TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX email_outbox_queued ON email_outbox (send_after) WHERE status = 'queued';

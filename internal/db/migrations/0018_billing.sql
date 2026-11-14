-- Plans bought online through Stripe Checkout (the same Stripe account
-- FireCardManager licenses are sold through). One row per paid session, so
-- a webhook delivered twice adds the months once.
CREATE TABLE billing_payments (
    session_id   TEXT PRIMARY KEY,
    account_id   UUID REFERENCES accounts(id) ON DELETE SET NULL,
    plan         TEXT NOT NULL,
    months       INT NOT NULL,
    amount_cents INT NOT NULL,
    email        TEXT,
    hwid         TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX billing_payments_account ON billing_payments (account_id, created_at DESC);

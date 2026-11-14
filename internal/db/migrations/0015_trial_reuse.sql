-- Free-trial reuse: what each account's trial was tied to. Kept after an
-- account is closed (no foreign key), so closing an account and signing up
-- again with a new email doesn't start another free trial for the same
-- person, PC or inventory. Values are one-way hashes.
CREATE TABLE trial_claims (
    kind        TEXT NOT NULL CHECK (kind IN ('email', 'hwid')),
    value       BYTEA NOT NULL,
    account_ref UUID NOT NULL,               -- the account's id (may since be deleted)
    claimed_at  TIMESTAMPTZ NOT NULL,        -- the account's sign-up time
    PRIMARY KEY (kind, value, account_ref)
);

-- One row per distinct card (player, year, set, number, parallel) an
-- account has had, to spot the same inventory brought into a new account.
CREATE TABLE trial_card_marks (
    account_ref     UUID NOT NULL,
    fp              BYTEA NOT NULL,
    account_created TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (account_ref, fp)
);
CREATE INDEX trial_card_marks_fp ON trial_card_marks (fp);

ALTER TABLE accounts ADD COLUMN trial_ended_reason TEXT;
ALTER TABLE accounts ADD COLUMN trial_marked_cards INT NOT NULL DEFAULT 0;

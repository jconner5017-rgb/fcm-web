-- FireCardManager Web uses its own Slabs & Singles key (made "for
-- FireCardManager Web" on Slabs & Singles and entered on the Account page),
-- never the desktop app's: only the desktop key's check-ins keep listings up
-- there. A linked PC now sends just its package/weight defaults.
CREATE TABLE account_market_settings (
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    market       TEXT NOT NULL,
    settings     JSONB NOT NULL DEFAULT '{}',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, market)
);
INSERT INTO account_market_settings (account_id, market, settings)
    SELECT account_id, market, settings FROM account_market_keys WHERE settings <> '{}'
    ON CONFLICT DO NOTHING;
DELETE FROM account_market_keys WHERE source = 'pc';

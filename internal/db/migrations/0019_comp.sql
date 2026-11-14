-- Free (complimentary) FireCardManager Web the owner gives to staff and
-- selected sellers: full access with no end date until the owner removes it.
ALTER TABLE accounts ADD COLUMN comp_at TIMESTAMPTZ;
ALTER TABLE accounts ADD COLUMN comp_note TEXT;

-- The web add-on follows the FCM license term: $20 with a 6-month license,
-- $40 with a 12-month license. It now runs until a date instead of forever.
ALTER TABLE accounts ADD COLUMN web_addon_until TIMESTAMPTZ;
UPDATE accounts SET web_addon_until = web_addon_at + interval '12 months' WHERE web_addon_at IS NOT NULL;

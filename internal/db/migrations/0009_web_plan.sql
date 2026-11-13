-- FireCardManager Web is sold two ways: as an add-on to an FCM or FCM TCG
-- license ($19.95 for 6 months, $39.95 for 12) or on its own ($29.95 / $49.95).
-- web_plan records which one the account last paid for.
ALTER TABLE accounts ADD COLUMN web_plan TEXT;
UPDATE accounts SET web_plan = 'addon' WHERE web_addon_at IS NOT NULL;

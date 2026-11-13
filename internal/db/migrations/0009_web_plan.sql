-- web_plan records which FireCardManager Web plan the account last paid for:
-- "web" (alone, $39.99 / 6 months, $59.99 / 12) or "bundle" (with FCM desktop,
-- $59.99 / $89.99). "addon" marks accounts from before plans were recorded.
ALTER TABLE accounts ADD COLUMN web_plan TEXT;
UPDATE accounts SET web_plan = 'addon' WHERE web_addon_at IS NOT NULL;

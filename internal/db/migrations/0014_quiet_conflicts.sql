-- Timestamp-only conflicts (every save changes updated_at) aren't shown any more.
UPDATE sync_conflicts SET resolved_at = now()
WHERE resolved_at IS NULL AND field IN ('updated_at', 'created_at', 'last_priced_at', 'cardsight_checked_at');

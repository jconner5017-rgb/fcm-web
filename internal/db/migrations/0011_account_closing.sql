-- Closing an account (privacy policy): it's switched off at once, then its
-- photos are deleted from storage and the account with everything in it is
-- removed by the hourly cleanup.
ALTER TABLE accounts ADD COLUMN closing_at TIMESTAMPTZ;

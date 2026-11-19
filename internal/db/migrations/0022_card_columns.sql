-- Speed (2026-11-19): the website sorted, searched and counted cards by
-- reading fields out of each card's JSON, so every page unpacked every card
-- in the account. These columns are copies the database keeps up to date by
-- itself on every insert and update (nothing in the sync code changes), and
-- the pages read them instead. Built for 500 accounts x 5,000 cards.
-- (Only immutable expressions, so a card that syncs can never fail here.)

ALTER TABLE sync_cards
    ADD COLUMN c_player   TEXT    GENERATED ALWAYS AS (lower(coalesce(data->>'player_name', ''))) STORED,
    ADD COLUMN c_set      TEXT    GENERATED ALWAYS AS (lower(coalesce(data->>'set_name', ''))) STORED,
    ADD COLUMN c_year     JSONB   GENERATED ALWAYS AS (data->'year') STORED,
    ADD COLUMN c_sport    TEXT    GENERATED ALWAYS AS (coalesce(data->>'sport', '')) STORED,
    ADD COLUMN c_row      TEXT    GENERATED ALWAYS AS (coalesce(data->>'slot_row', '')) STORED,
    ADD COLUMN c_slot     NUMERIC GENERATED ALWAYS AS (CASE WHEN jsonb_typeof(data->'slot_number') = 'number' THEN (data->>'slot_number')::numeric END) STORED,
    ADD COLUMN c_value    NUMERIC GENERATED ALWAYS AS (CASE WHEN jsonb_typeof(data->'estimated_value') = 'number' THEN (data->>'estimated_value')::numeric END) STORED,
    ADD COLUMN c_sale     NUMERIC GENERATED ALWAYS AS (CASE WHEN jsonb_typeof(data->'sale_price') = 'number' THEN (data->>'sale_price')::numeric END) STORED,
    ADD COLUMN c_bid      NUMERIC GENERATED ALWAYS AS (CASE WHEN jsonb_typeof(data->'start_bid') = 'number' THEN (data->>'start_bid')::numeric END) STORED,
    ADD COLUMN c_added    TEXT    GENERATED ALWAYS AS (data->>'created_at') STORED,
    ADD COLUMN c_sku      TEXT    GENERATED ALWAYS AS (upper(coalesce(data->>'sku', ''))) STORED,
    ADD COLUMN c_slabs    TEXT    GENERATED ALWAYS AS (coalesce(data->>'marketplace_listing_id', '')) STORED,
    ADD COLUMN c_slabs_at TEXT    GENERATED ALWAYS AS (data->>'marketplace_listed_at') STORED,
    -- What a search word is matched against (lowercase); the box name is added at query time.
    ADD COLUMN c_search   TEXT    GENERATED ALWAYS AS (lower(
        coalesce(data->>'player_name', '') || ' ' || coalesce(data->>'team', '') || ' ' || coalesce(data->>'brand', '') || ' ' ||
        coalesce(data->>'set_name', '') || ' ' || coalesce(data->>'card_number', '') || ' ' || coalesce(data->>'variety', '') || ' ' ||
        coalesce(data->>'sku', '') || ' ' || coalesce(data->>'year', '') || ' ' || coalesce(data->>'serial_number', '') || ' ' ||
        coalesce(data->>'grading_company', '') || ' ' || coalesce(data->>'card_grade', '') || ' ' || coalesce(data->>'sport', '') || ' ' ||
        coalesce(data->>'condition', ''))) STORED;

-- "Which editions does this account have" scanned the whole table: the old
-- index only covers live (not deleted) cards.
CREATE INDEX IF NOT EXISTS sync_cards_account_all ON sync_cards (account_id, edition);
-- eBay refresh looks cards up by SKU, once per eBay order.
CREATE INDEX IF NOT EXISTS sync_cards_sku ON sync_cards (account_id, c_sku) WHERE deleted_at IS NULL;

-- Keep each card's full JSON (and its field times) out of the table row, so
-- the rows the website scans hold only the small columns above: 5x smaller
-- on a 5,000-card test account (892 MB -> 167 MB per 500,000 cards), which
-- keeps the part every page reads in memory. 512 bytes moves the two big
-- values out and leaves the columns in place. lz4 if this server has it.
ALTER TABLE sync_cards SET (toast_tuple_target = 512);
DO $$
BEGIN
    ALTER TABLE sync_cards ALTER COLUMN data SET COMPRESSION lz4, ALTER COLUMN field_meta SET COMPRESSION lz4;
EXCEPTION WHEN OTHERS THEN
    NULL; -- no lz4: the default compression works too
END $$;
-- Existing cards move out when rewritten (new and synced cards do it on their own).
UPDATE sync_cards SET data = data || '{}'::jsonb, field_meta = field_meta || '{}'::jsonb;

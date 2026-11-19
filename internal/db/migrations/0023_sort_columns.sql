-- Sortable editor columns (2026-11-19): Product (brand) and Card # join the
-- columns the database keeps from each card's data (see 0022).
ALTER TABLE sync_cards
    ADD COLUMN c_brand  TEXT GENERATED ALWAYS AS (lower(coalesce(data->>'brand', ''))) STORED,
    ADD COLUMN c_number TEXT GENERATED ALWAYS AS (lower(coalesce(data->>'card_number', ''))) STORED;

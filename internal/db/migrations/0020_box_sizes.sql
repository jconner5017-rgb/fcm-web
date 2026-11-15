-- Box sizes (2026-11-15): the size of the boxes a seller stores each type of
-- card in, for boxes made on the website (each box's own size and "closed"
-- travel with the box through sync). One row per account and edition.
CREATE TABLE account_box_sizes (
    account_id  UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    edition     TEXT NOT NULL,
    raw_size    INT,
    relic_size  INT,
    slab_size   INT,
    fill_sold   BOOLEAN NOT NULL DEFAULT false,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, edition)
);

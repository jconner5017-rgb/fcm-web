-- The last price "Refresh from eBay" saw for each SKU (desktop ebay-prices.json),
-- so a price changed on eBay updates the card but an unchanged one doesn't
-- overwrite a newer price set in the app.
CREATE TABLE ebay_seen_prices (
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    sku        TEXT NOT NULL,
    price      NUMERIC(12,2) NOT NULL,
    PRIMARY KEY (account_id, sku)
);

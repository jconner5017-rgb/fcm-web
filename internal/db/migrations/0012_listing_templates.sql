-- eBay listing templates: the ones a linked PC sends (FireCardManager's
-- Listing Templates, read-only here) and ones made on the website.
CREATE TABLE listing_templates (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id       UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    edition          TEXT NOT NULL,
    source           TEXT NOT NULL CHECK (source IN ('pc', 'web')),
    name             TEXT NOT NULL,
    title_template   TEXT NOT NULL,
    description_html TEXT NOT NULL,
    is_default       BOOLEAN NOT NULL DEFAULT false,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX listing_templates_account ON listing_templates (account_id, edition);

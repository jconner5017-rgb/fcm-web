-- Resource monitor (back office): one snapshot a day of how much the site
-- stores, so growth per day and "full in N days" can be shown.
CREATE TABLE resource_snapshots (
    taken_at     TIMESTAMPTZ PRIMARY KEY DEFAULT now(),
    db_bytes     BIGINT NOT NULL,
    photo_bytes  BIGINT NOT NULL,
    photos       BIGINT NOT NULL,
    cards        BIGINT NOT NULL,
    accounts     BIGINT NOT NULL
);

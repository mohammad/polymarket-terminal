CREATE TABLE IF NOT EXISTS subscribed_markets (
    asset_id   TEXT        PRIMARY KEY,
    label      TEXT        NOT NULL DEFAULT '',
    active     BOOLEAN     NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orderbook_snapshots (
    asset_id   TEXT        PRIMARY KEY,
    bids       JSONB       NOT NULL DEFAULT '[]',
    asks       JSONB       NOT NULL DEFAULT '[]',
    hash       TEXT        NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed an example market; replace with real Polymarket token IDs.
-- Token IDs are the ERC-1155 token ID for each outcome (YES/NO) on a market.
INSERT INTO subscribed_markets (asset_id, label) VALUES
    ('71321045679252212594626385532706912750332728571942532289631379312455583992563', 'Will Trump win 2024?')
ON CONFLICT DO NOTHING;

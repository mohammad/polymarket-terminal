package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"polymarket-terminal/internal/orderbook"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// Store wraps a pgxpool and provides typed queries.
type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Market is a row from subscribed_markets.
type Market struct {
	AssetID string
	Label   string
}

func (s *Store) ListMarkets(ctx context.Context) ([]Market, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT asset_id, label FROM subscribed_markets WHERE active = true ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Market
	for rows.Next() {
		var m Market
		if err := rows.Scan(&m.AssetID, &m.Label); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) AddMarket(ctx context.Context, assetID, label string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO subscribed_markets (asset_id, label)
         VALUES ($1, $2)
         ON CONFLICT (asset_id) DO UPDATE SET label = $2, active = true`,
		assetID, label)
	return err
}

func (s *Store) LoadLastViewedMarket(ctx context.Context) (string, error) {
	var assetID string
	err := s.pool.QueryRow(ctx,
		`SELECT value FROM app_state WHERE key = 'last_viewed_market'`).Scan(&assetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	return assetID, err
}

func (s *Store) SaveLastViewedMarket(ctx context.Context, assetID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO app_state (key, value, updated_at)
		 VALUES ('last_viewed_market', $1, $2)
		 ON CONFLICT (key) DO UPDATE
		   SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
		assetID, time.Now())
	return err
}

// SaveSnapshot upserts the current orderbook state for an asset.
func (s *Store) SaveSnapshot(ctx context.Context, assetID string, bids, asks []orderbook.Level, hash string) error {
	bj, err := marshalLevels(bids)
	if err != nil {
		return err
	}
	aj, err := marshalLevels(asks)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO orderbook_snapshots (asset_id, bids, asks, hash, updated_at)
         VALUES ($1, $2, $3, $4, $5)
         ON CONFLICT (asset_id) DO UPDATE
           SET bids = $2, asks = $3, hash = $4, updated_at = $5`,
		assetID, bj, aj, hash, time.Now())
	return err
}

// LoadSnapshot retrieves the last persisted snapshot for an asset.
// Returns pgx.ErrNoRows if no snapshot exists yet.
func (s *Store) LoadSnapshot(ctx context.Context, assetID string) (bids, asks []orderbook.Level, hash string, err error) {
	var bj, aj []byte
	err = s.pool.QueryRow(ctx,
		`SELECT bids, asks, hash FROM orderbook_snapshots WHERE asset_id = $1`, assetID).
		Scan(&bj, &aj, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, "", err
	}
	if err != nil {
		return nil, nil, "", err
	}

	bids, err = unmarshalLevels(bj)
	if err != nil {
		return nil, nil, "", err
	}
	asks, err = unmarshalLevels(aj)
	return bids, asks, hash, err
}

// ── helpers ──────────────────────────────────────────────────────────────────

type levelJSON struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

func marshalLevels(levels []orderbook.Level) ([]byte, error) {
	lj := make([]levelJSON, len(levels))
	for i, l := range levels {
		lj[i] = levelJSON{Price: l.Price.String(), Size: l.Size.String()}
	}
	return json.Marshal(lj)
}

func unmarshalLevels(data []byte) ([]orderbook.Level, error) {
	var lj []levelJSON
	if err := json.Unmarshal(data, &lj); err != nil {
		return nil, err
	}
	out := make([]orderbook.Level, len(lj))
	for i, l := range lj {
		p, _ := decimal.NewFromString(l.Price)
		s, _ := decimal.NewFromString(l.Size)
		out[i] = orderbook.Level{Price: p, Size: s}
	}
	return out, nil
}

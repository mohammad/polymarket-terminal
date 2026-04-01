package reconciler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"polymarket-terminal/internal/orderbook"
	"polymarket-terminal/internal/ws"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type fakeREST struct {
	mu        sync.Mutex
	snapshots map[string]snapshot
	errs      map[string]error
}

type snapshot struct {
	bids []orderbook.Level
	asks []orderbook.Level
	hash string
}

func (f *fakeREST) FetchBook(ctx context.Context, assetID string) ([]orderbook.Level, []orderbook.Level, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[assetID]; err != nil {
		return nil, nil, "", err
	}
	s := f.snapshots[assetID]
	return s.bids, s.asks, s.hash, nil
}

type fakeStore struct {
	mu        sync.Mutex
	snapshots map[string]snapshot
}

func (f *fakeStore) LoadSnapshot(ctx context.Context, assetID string) ([]orderbook.Level, []orderbook.Level, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.snapshots[assetID]
	if !ok {
		return nil, nil, "", pgx.ErrNoRows
	}
	return s.bids, s.asks, s.hash, nil
}

func (f *fakeStore) SaveSnapshot(ctx context.Context, assetID string, bids, asks []orderbook.Level, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots[assetID] = snapshot{bids: bids, asks: asks, hash: hash}
	return nil
}

func TestSwitchMarketUsesWarmBookBeforeRESTResync(t *testing.T) {
	events := make(chan ws.Event, 8)
	rest := &fakeREST{
		snapshots: map[string]snapshot{
			"a1": {bids: levels("0.4", "10"), asks: levels("0.6", "10"), hash: "rest-a1"},
			"a2": {bids: levels("0.2", "20"), asks: levels("0.8", "20"), hash: "rest-a2"},
		},
		errs: map[string]error{},
	}
	store := &fakeStore{snapshots: map[string]snapshot{}}

	updates := make(chan struct {
		assetID string
		hash    string
	}, 16)

	rec := New(events, rest, store, Config{
		ActiveAssetID: "a1",
		AssetIDs:      []string{"a1", "a2"},
		SyncInterval:  time.Hour,
		WriteInterval: time.Hour,
		OnUpdate: func(assetID string, bids, asks []orderbook.Level, hash string) {
			updates <- struct {
				assetID string
				hash    string
			}{assetID: assetID, hash: hash}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rec.Run(ctx)

	waitForUpdate(t, updates, "a1", "rest-a1")

	events <- ws.Event{
		EventType: "book",
		AssetID:   "a2",
		Hash:      "warm-a2",
		Bids:      []ws.PriceLevel{{Price: "0.3", Size: "12"}},
		Asks:      []ws.PriceLevel{{Price: "0.7", Size: "13"}},
	}
	time.Sleep(50 * time.Millisecond)

	rec.SwitchMarket("a2")
	waitForUpdate(t, updates, "a2", "warm-a2")
}

func TestForceResyncReportsErrors(t *testing.T) {
	events := make(chan ws.Event, 1)
	rest := &fakeREST{
		snapshots: map[string]snapshot{
			"a1": {bids: levels("0.4", "10"), asks: levels("0.6", "10"), hash: "rest-a1"},
		},
		errs: map[string]error{},
	}
	store := &fakeStore{snapshots: map[string]snapshot{}}
	statuses := make(chan Status, 16)

	rec := New(events, rest, store, Config{
		ActiveAssetID: "a1",
		AssetIDs:      []string{"a1"},
		SyncInterval:  time.Hour,
		WriteInterval: time.Hour,
		OnStatus: func(status Status) {
			statuses <- status
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rec.Run(ctx)

	waitForStatus(t, statuses, func(status Status) bool {
		return status.Source == "startup" && !status.Syncing
	})

	rest.mu.Lock()
	rest.errs["a1"] = errors.New("boom")
	rest.mu.Unlock()

	rec.ForceResync()
	waitForStatus(t, statuses, func(status Status) bool {
		return status.Source == "manual" && status.Error != ""
	})
}

func waitForUpdate(t *testing.T, updates <-chan struct {
	assetID string
	hash    string
}, assetID, hash string) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case update := <-updates:
			if update.assetID == assetID && update.hash == hash {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for update %s/%s", assetID, hash)
		}
	}
}

func waitForStatus(t *testing.T, statuses <-chan Status, match func(Status) bool) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case status := <-statuses:
			if match(status) {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for matching status")
		}
	}
}

func levels(price, size string) []orderbook.Level {
	p, _ := decimal.NewFromString(price)
	s, _ := decimal.NewFromString(size)
	return []orderbook.Level{{Price: p, Size: s}}
}

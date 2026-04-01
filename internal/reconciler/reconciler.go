package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"polymarket-terminal/internal/orderbook"
	"polymarket-terminal/internal/ws"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type restFetcher interface {
	FetchBook(ctx context.Context, assetID string) (bids, asks []orderbook.Level, hash string, err error)
}

type snapshotStore interface {
	LoadSnapshot(ctx context.Context, assetID string) (bids, asks []orderbook.Level, hash string, err error)
	SaveSnapshot(ctx context.Context, assetID string, bids, asks []orderbook.Level, hash string) error
}

// UpdateFn is called after the active book mutates with a consistent snapshot.
type UpdateFn func(assetID string, bids, asks []orderbook.Level, hash string)

// StatusFn reports the active market's sync lifecycle to the UI/logging layer.
type StatusFn func(Status)

type Status struct {
	AssetID   string
	Syncing   bool
	Error     string
	Message   string
	Source    string
	UpdatedAt time.Time
}

// Config holds tunable parameters for the Reconciler.
type Config struct {
	ActiveAssetID string
	AssetIDs      []string
	SyncInterval  time.Duration
	WriteInterval time.Duration
	OnUpdate      UpdateFn
	OnStatus      StatusFn
}

// Reconciler owns the write path for all subscribed books while exposing one
// active market to the UI. Hidden markets stay warm from the shared WS feed so
// switching is usually instant.
type Reconciler struct {
	books         map[string]*orderbook.Book
	activeAssetID string
	events        <-chan ws.Event
	restClient    restFetcher
	store         snapshotStore
	syncInterval  time.Duration
	writeInterval time.Duration
	onUpdate      UpdateFn
	onStatus      StatusFn
	switchCh      chan string
	resyncCh      chan struct{}
	dirty         map[string]struct{}
}

func New(
	events <-chan ws.Event,
	restClient restFetcher,
	store snapshotStore,
	cfg Config,
) *Reconciler {
	books := make(map[string]*orderbook.Book, len(cfg.AssetIDs))
	for _, assetID := range cfg.AssetIDs {
		books[assetID] = orderbook.New(assetID)
	}
	if cfg.ActiveAssetID != "" && books[cfg.ActiveAssetID] == nil {
		books[cfg.ActiveAssetID] = orderbook.New(cfg.ActiveAssetID)
	}

	return &Reconciler{
		books:         books,
		activeAssetID: cfg.ActiveAssetID,
		events:        events,
		restClient:    restClient,
		store:         store,
		syncInterval:  cfg.SyncInterval,
		writeInterval: cfg.WriteInterval,
		onUpdate:      cfg.OnUpdate,
		onStatus:      cfg.OnStatus,
		switchCh:      make(chan string, 1),
		resyncCh:      make(chan struct{}, 1),
		dirty:         make(map[string]struct{}),
	}
}

func (r *Reconciler) SwitchMarket(assetID string) {
	for {
		select {
		case r.switchCh <- assetID:
			return
		default:
			select {
			case <-r.switchCh:
			default:
			}
		}
	}
}

func (r *Reconciler) ForceResync() {
	select {
	case r.resyncCh <- struct{}{}:
	default:
	}
}

// Run is the main reconciliation loop. Blocks until ctx is done.
func (r *Reconciler) Run(ctx context.Context) {
	r.coldStart(ctx, r.activeAssetID, true)

	syncTicker := time.NewTicker(r.syncInterval)
	writeTicker := time.NewTicker(r.writeInterval)
	defer syncTicker.Stop()
	defer writeTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case assetID := <-r.switchCh:
			if assetID == "" {
				continue
			}
			r.activeAssetID = assetID
			slog.Info("market switched", "asset", assetID)
			r.notifyStatus(Status{
				AssetID:   assetID,
				Syncing:   true,
				Message:   "switching market",
				Source:    "switch",
				UpdatedAt: time.Now(),
			})
			r.notifyActive()
			r.coldStart(ctx, assetID, false)

		case e := <-r.events:
			r.handleEvent(e)

		case <-r.resyncCh:
			r.resync(ctx, r.activeAssetID, "manual")

		case <-syncTicker.C:
			r.periodicSync(ctx)

		case <-writeTicker.C:
			r.persist(ctx)
		}
	}
}

func (r *Reconciler) coldStart(ctx context.Context, assetID string, forceDB bool) {
	book := r.bookFor(assetID)
	if forceDB || book.Hash() == "" {
		if bids, asks, hash, err := r.store.LoadSnapshot(ctx, assetID); err == nil {
			book.ApplySnapshot(bids, asks, hash)
			r.markDirty(assetID)
			if assetID == r.activeAssetID {
				r.notifyActive()
			}
			slog.Info("seeded from db", "asset", assetID, "hash", shortHash(hash))
		} else if err != pgx.ErrNoRows {
			slog.Warn("db snapshot load failed", "asset", assetID, "err", err)
		}
	}

	r.resync(ctx, assetID, "startup")
}

func (r *Reconciler) handleEvent(e ws.Event) {
	switch e.EventType {
	case "book":
		book := r.lookupBook(e.AssetID)
		if book == nil {
			return
		}
		bids := parseLevels(e.Bids)
		asks := parseLevels(e.Asks)
		book.ApplySnapshot(bids, asks, e.Hash)
		r.markDirty(e.AssetID)
		if e.AssetID == r.activeAssetID {
			r.notifyActive()
		}

	case "price_change":
		updatedAssets := make(map[string]struct{})
		for _, ch := range e.Changes {
			book := r.lookupBook(ch.AssetID)
			if book == nil {
				continue
			}
			price, _ := decimal.NewFromString(ch.Price)
			size, _ := decimal.NewFromString(ch.Size)
			book.ApplyDelta(ch.Side, price, size, ch.Hash)
			r.markDirty(ch.AssetID)
			updatedAssets[ch.AssetID] = struct{}{}
		}
		if _, ok := updatedAssets[r.activeAssetID]; ok {
			r.notifyActive()
		}
	}
}

func (r *Reconciler) periodicSync(ctx context.Context) {
	r.resync(ctx, r.activeAssetID, "periodic")
}

func (r *Reconciler) resync(ctx context.Context, assetID, source string) {
	if assetID == "" {
		return
	}
	r.notifyStatus(Status{
		AssetID:   assetID,
		Syncing:   true,
		Message:   "refreshing orderbook",
		Source:    source,
		UpdatedAt: time.Now(),
	})

	book := r.bookFor(assetID)
	localHash := book.Hash()
	bids, asks, hash, err := r.restClient.FetchBook(ctx, assetID)
	if err != nil {
		msg := fmt.Sprintf("%s sync failed: %v", source, err)
		slog.Warn("rest sync failed", "asset", assetID, "source", source, "err", err)
		if assetID == r.activeAssetID {
			r.notifyStatus(Status{
				AssetID:   assetID,
				Syncing:   false,
				Error:     msg,
				Message:   msg,
				Source:    source,
				UpdatedAt: time.Now(),
			})
		}
		return
	}
	if hash == localHash && source == "periodic" {
		if assetID == r.activeAssetID {
			r.notifyStatus(Status{
				AssetID:   assetID,
				Syncing:   false,
				Message:   "book in sync",
				Source:    source,
				UpdatedAt: time.Now(),
			})
		}
		return
	}

	if source == "periodic" && localHash != "" && localHash != hash {
		slog.Info("hash drift detected",
			"asset", assetID,
			"local", shortHash(localHash),
			"remote", shortHash(hash))
	}

	book.ApplySnapshot(bids, asks, hash)
	r.markDirty(assetID)
	if assetID == r.activeAssetID {
		r.notifyActive()
		r.notifyStatus(Status{
			AssetID:   assetID,
			Syncing:   false,
			Message:   "orderbook synced",
			Source:    source,
			UpdatedAt: time.Now(),
		})
	}
	slog.Info("resynced from REST", "asset", assetID, "source", source, "hash", shortHash(hash))
}

func (r *Reconciler) persist(ctx context.Context) {
	for assetID := range r.dirty {
		book := r.bookFor(assetID)
		bids, asks, hash := book.Snapshot()
		if hash == "" {
			delete(r.dirty, assetID)
			continue
		}
		if err := r.store.SaveSnapshot(ctx, assetID, bids, asks, hash); err != nil {
			slog.Warn("db persist failed", "asset", assetID, "err", err)
			continue
		}
		delete(r.dirty, assetID)
	}
}

func (r *Reconciler) notifyActive() {
	if r.onUpdate == nil || r.activeAssetID == "" {
		return
	}
	bids, asks, hash := r.bookFor(r.activeAssetID).Snapshot()
	r.onUpdate(r.activeAssetID, bids, asks, hash)
}

func (r *Reconciler) notifyStatus(status Status) {
	if r.onStatus == nil {
		return
	}
	r.onStatus(status)
}

func (r *Reconciler) lookupBook(assetID string) *orderbook.Book {
	if assetID == "" {
		return nil
	}
	return r.books[assetID]
}

func (r *Reconciler) bookFor(assetID string) *orderbook.Book {
	if book := r.lookupBook(assetID); book != nil {
		return book
	}
	book := orderbook.New(assetID)
	r.books[assetID] = book
	return book
}

func (r *Reconciler) markDirty(assetID string) {
	if assetID != "" {
		r.dirty[assetID] = struct{}{}
	}
}

func parseLevels(levels []ws.PriceLevel) []orderbook.Level {
	out := make([]orderbook.Level, 0, len(levels))
	for _, l := range levels {
		p, _ := decimal.NewFromString(l.Price)
		s, _ := decimal.NewFromString(l.Size)
		if !s.IsZero() {
			out = append(out, orderbook.Level{Price: p, Size: s})
		}
	}
	return out
}

func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

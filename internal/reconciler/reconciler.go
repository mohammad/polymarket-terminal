package reconciler

import (
	"context"
	"log/slog"
	"time"

	"polymarket-terminal/internal/db"
	"polymarket-terminal/internal/orderbook"
	"polymarket-terminal/internal/rest"
	"polymarket-terminal/internal/ws"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// UpdateFn is called after every book mutation with a consistent snapshot.
// It runs in the reconciler goroutine, so implementations must be non-blocking
// (e.g. send to a buffered channel, call tea.Program.Send).
type UpdateFn func(bids, asks []orderbook.Level, hash string)

// Config holds tunable parameters for the Reconciler.
type Config struct {
	SyncInterval  time.Duration // how often to diff against the REST snapshot
	WriteInterval time.Duration // how often to persist the book to Postgres
	OnUpdate      UpdateFn      // called on every book change; may be nil
}

// Reconciler owns the write path for a single orderbook.
// It seeds from the DB on cold start, syncs from the REST API on connect and
// periodically, applies streaming WS deltas, and writes snapshots to the DB.
//
// Market switching is handled via SwitchMarket; the caller creates a new Book
// and hands it to the reconciler, which then re-seeds and re-syncs.
type Reconciler struct {
	book          *orderbook.Book
	wsClient      *ws.Client
	restClient    *rest.Client
	store         *db.Store
	syncInterval  time.Duration
	writeInterval time.Duration
	onUpdate      UpdateFn
	switchCh      chan *orderbook.Book
}

func New(
	book *orderbook.Book,
	wsClient *ws.Client,
	restClient *rest.Client,
	store *db.Store,
	cfg Config,
) *Reconciler {
	return &Reconciler{
		book:          book,
		wsClient:      wsClient,
		restClient:    restClient,
		store:         store,
		syncInterval:  cfg.SyncInterval,
		writeInterval: cfg.WriteInterval,
		onUpdate:      cfg.OnUpdate,
		switchCh:      make(chan *orderbook.Book, 1),
	}
}

// SwitchMarket replaces the active book and triggers an immediate resync.
// Safe to call from any goroutine.
func (r *Reconciler) SwitchMarket(book *orderbook.Book) {
	r.switchCh <- book
}

// Run is the main reconciliation loop. Blocks until ctx is done.
func (r *Reconciler) Run(ctx context.Context) {
	r.coldStart(ctx)

	syncTicker := time.NewTicker(r.syncInterval)
	writeTicker := time.NewTicker(r.writeInterval)
	defer syncTicker.Stop()
	defer writeTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case newBook := <-r.switchCh:
			r.book = newBook
			slog.Info("switched market", "asset", r.book.AssetID())
			r.coldStart(ctx)

		case e := <-r.wsClient.Events:
			r.handleEvent(e)

		case <-syncTicker.C:
			r.periodicSync(ctx)

		case <-writeTicker.C:
			r.persist(ctx)
		}
	}
}

// coldStart seeds from the DB snapshot (instant data on restart) then fetches
// an authoritative REST snapshot to overwrite any stale DB state.
func (r *Reconciler) coldStart(ctx context.Context) {
	if bids, asks, hash, err := r.store.LoadSnapshot(ctx, r.book.AssetID()); err == nil {
		r.book.ApplySnapshot(bids, asks, hash)
		r.notify()
		slog.Info("seeded from db", "asset", r.book.AssetID(), "hash", shortHash(hash))
	} else if err != pgx.ErrNoRows {
		slog.Warn("db snapshot load error", "asset", r.book.AssetID(), "err", err)
	}

	r.resync(ctx)
}

func (r *Reconciler) handleEvent(e ws.Event) {
	if e.AssetID != r.book.AssetID() {
		return
	}

	switch e.EventType {
	case "book":
		bids := parseLevels(e.Bids)
		asks := parseLevels(e.Asks)
		r.book.ApplySnapshot(bids, asks, e.Hash)
		r.notify()

	case "price_change":
		for _, ch := range e.Changes {
			price, _ := decimal.NewFromString(ch.Price)
			size, _ := decimal.NewFromString(ch.Size)
			r.book.ApplyDelta(ch.Side, price, size, e.Hash)
		}
		r.notify()
	}
}

// periodicSync fetches the REST snapshot and reconciles if the hash drifted.
func (r *Reconciler) periodicSync(ctx context.Context) {
	bids, asks, hash, err := r.restClient.FetchBook(ctx, r.book.AssetID())
	if err != nil {
		slog.Warn("periodic sync failed", "asset", r.book.AssetID(), "err", err)
		return
	}
	if hash == r.book.Hash() {
		return // in sync
	}
	slog.Info("hash drift detected — resyncing",
		"asset", r.book.AssetID(),
		"local", shortHash(r.book.Hash()),
		"remote", shortHash(hash))
	r.book.ApplySnapshot(bids, asks, hash)
	r.notify()
}

// resync always fetches and applies the REST snapshot regardless of hash.
func (r *Reconciler) resync(ctx context.Context) {
	bids, asks, hash, err := r.restClient.FetchBook(ctx, r.book.AssetID())
	if err != nil {
		slog.Warn("resync failed", "asset", r.book.AssetID(), "err", err)
		return
	}
	r.book.ApplySnapshot(bids, asks, hash)
	r.notify()
	slog.Info("resynced from REST", "asset", r.book.AssetID(), "hash", shortHash(hash))
}

func (r *Reconciler) persist(ctx context.Context) {
	bids, asks, hash := r.book.Snapshot()
	if hash == "" {
		return // nothing to persist yet
	}
	if err := r.store.SaveSnapshot(ctx, r.book.AssetID(), bids, asks, hash); err != nil {
		slog.Warn("db persist failed", "asset", r.book.AssetID(), "err", err)
	}
}

func (r *Reconciler) notify() {
	if r.onUpdate == nil {
		return
	}
	bids, asks, hash := r.book.Snapshot()
	r.onUpdate(bids, asks, hash)
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

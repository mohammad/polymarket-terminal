package orderbook

import (
	"sort"
	"sync"

	"github.com/shopspring/decimal"
)

// Level is a single price level on the book.
type Level struct {
	Price decimal.Decimal
	Size  decimal.Decimal
}

// Book is a thread-safe in-memory orderbook for a single asset.
// Only the reconciler writes to it; any number of readers may call Snapshot.
type Book struct {
	mu      sync.RWMutex
	assetID string
	bids    map[string]decimal.Decimal // price string → size
	asks    map[string]decimal.Decimal
	hash    string
}

func New(assetID string) *Book {
	return &Book{
		assetID: assetID,
		bids:    make(map[string]decimal.Decimal),
		asks:    make(map[string]decimal.Decimal),
	}
}

func (b *Book) AssetID() string { return b.assetID }

// ApplySnapshot replaces the entire book with the provided levels and hash.
func (b *Book) ApplySnapshot(bids, asks []Level, hash string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.bids = make(map[string]decimal.Decimal, len(bids))
	b.asks = make(map[string]decimal.Decimal, len(asks))

	for _, l := range bids {
		if !l.Size.IsZero() {
			b.bids[l.Price.String()] = l.Size
		}
	}
	for _, l := range asks {
		if !l.Size.IsZero() {
			b.asks[l.Price.String()] = l.Size
		}
	}
	b.hash = hash
}

// ApplyDelta applies a single incremental change.
// side must be "BUY" or "SELL". A zero size removes the price level.
func (b *Book) ApplyDelta(side string, price, size decimal.Decimal, hash string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	m := b.bids
	if side == "SELL" {
		m = b.asks
	}

	key := price.String()
	if size.IsZero() {
		delete(m, key)
	} else {
		m[key] = size
	}
	b.hash = hash
}

// Hash returns the current book hash without taking a full snapshot.
func (b *Book) Hash() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.hash
}

// Snapshot returns a consistent copy of the book:
// bids sorted descending, asks sorted ascending.
func (b *Book) Snapshot() (bids, asks []Level, hash string) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	bids = make([]Level, 0, len(b.bids))
	for p, s := range b.bids {
		price, _ := decimal.NewFromString(p)
		bids = append(bids, Level{Price: price, Size: s})
	}
	asks = make([]Level, 0, len(b.asks))
	for p, s := range b.asks {
		price, _ := decimal.NewFromString(p)
		asks = append(asks, Level{Price: price, Size: s})
	}

	sort.Slice(bids, func(i, j int) bool {
		return bids[i].Price.GreaterThan(bids[j].Price)
	})
	sort.Slice(asks, func(i, j int) bool {
		return asks[i].Price.LessThan(asks[j].Price)
	})

	return bids, asks, b.hash
}

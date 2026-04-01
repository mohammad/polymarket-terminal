package ws

// SubscribeMsg is sent immediately after connecting to subscribe to market data.
// AssetIDs are ERC-1155 token IDs (not condition IDs).
type SubscribeMsg struct {
	AssetIDs []string `json:"assets_ids"`
	Type     string   `json:"type"`
}

// Event is a message received from the Polymarket CLOB WebSocket.
//
// EventType "book"         — full orderbook snapshot (Bids + Asks populated).
// EventType "price_change" — incremental update     (Changes populated).
//
// Note: book events arrive as a JSON array; price_change events are single objects.
// The asset_id and hash for price_change are inside each PriceChange item, not
// at the top level of the event.
type Event struct {
	EventType string        `json:"event_type"`
	AssetID   string        `json:"asset_id"`
	Market    string        `json:"market"`
	Hash      string        `json:"hash"`
	Timestamp string        `json:"timestamp"`
	Bids      []PriceLevel  `json:"bids,omitempty"`
	Asks      []PriceLevel  `json:"asks,omitempty"`
	Changes   []PriceChange `json:"price_changes,omitempty"`
}

// PriceLevel is a bid or ask level in a full snapshot.
type PriceLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// PriceChange is a single level mutation in a price_change event.
// Side is "BUY" (bid) or "SELL" (ask). Size "0" means remove the level.
// AssetID and Hash are per-change fields (not on the parent Event).
type PriceChange struct {
	AssetID string `json:"asset_id"`
	Side    string `json:"side"`
	Price   string `json:"price"`
	Size    string `json:"size"`
	Hash    string `json:"hash"`
}

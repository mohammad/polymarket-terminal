package ws

// SubscribeMsg is sent immediately after connecting to subscribe to market data.
// Auth may be nil for public orderbook data.
type SubscribeMsg struct {
	Auth    *AuthPayload `json:"auth"`
	Markets []string     `json:"markets"`
	Type    string       `json:"type"`
}

// AuthPayload is only required for private channels.
type AuthPayload struct {
	APIKey     string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// Event is a message received from the Polymarket CLOB WebSocket.
//
// EventType "book"         — full orderbook snapshot (Bids + Asks populated).
// EventType "price_change" — incremental update     (Changes populated).
type Event struct {
	EventType string        `json:"event_type"`
	AssetID   string        `json:"asset_id"`
	Market    string        `json:"market"`
	Hash      string        `json:"hash"`
	Timestamp string        `json:"timestamp"`
	Bids      []PriceLevel  `json:"bids,omitempty"`
	Asks      []PriceLevel  `json:"asks,omitempty"`
	Changes   []PriceChange `json:"changes,omitempty"`
}

// PriceLevel is a bid or ask level in a full snapshot.
type PriceLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// PriceChange is a single level mutation in a price_change event.
// Side is "BUY" (bid) or "SELL" (ask). Size "0" means remove the level.
type PriceChange struct {
	Side  string `json:"side"`
	Price string `json:"price"`
	Size  string `json:"size"`
}

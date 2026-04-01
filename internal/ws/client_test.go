package ws

import (
	"encoding/json"
	"testing"
)

func TestNewClientStoresURLAndMarkets(t *testing.T) {
	markets := []string{"asset-1", "asset-2"}
	client := NewClient("wss://example.com/ws/market", markets)

	if client.url != "wss://example.com/ws/market" {
		t.Fatalf("url = %q, want websocket URL to be stored", client.url)
	}

	stored := client.markets.Load()
	if stored == nil || len(*stored) != len(markets) {
		t.Fatalf("stored markets = %#v, want %v", stored, markets)
	}
}

func TestDispatchAcceptsArrayAndSingleEvents(t *testing.T) {
	client := NewClient("wss://example.com/ws/market", nil)

	arrayPayload, err := json.Marshal([]Event{{
		EventType: "book",
		AssetID:   "asset-1",
		Hash:      "hash-1",
	}})
	if err != nil {
		t.Fatalf("marshal array payload: %v", err)
	}
	client.dispatch(arrayPayload)

	singlePayload, err := json.Marshal(Event{
		EventType: "price_change",
		Changes: []PriceChange{{
			AssetID: "asset-1",
			Side:    "BUY",
			Price:   "0.5",
			Size:    "10",
			Hash:    "hash-2",
		}},
	})
	if err != nil {
		t.Fatalf("marshal single payload: %v", err)
	}
	client.dispatch(singlePayload)

	var got []Event
	for i := 0; i < 2; i++ {
		select {
		case event := <-client.Events:
			got = append(got, event)
		default:
			t.Fatalf("expected 2 events, got %d", len(got))
		}
	}

	if got[0].EventType != "book" || got[1].EventType != "price_change" {
		t.Fatalf("unexpected event sequence: %#v", got)
	}
}

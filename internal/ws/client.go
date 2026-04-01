package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeTimeout  = 10 * time.Second
	pongWait      = 60 * time.Second
	pingInterval  = 10 * time.Second
	reconnectBase = 1 * time.Second
	reconnectMax  = 30 * time.Second
)

// Client maintains a persistent WebSocket connection to the Polymarket CLOB feed.
// It exposes a buffered Events channel; consumers should drain it continuously.
type Client struct {
	url       string
	markets   atomic.Pointer[[]string] // safe for concurrent UpdateMarkets calls
	writeMu   sync.Mutex
	Events    chan Event
	Connected chan bool // signals connection state changes (non-blocking send)
}

func NewClient(url string, markets []string) *Client {
	c := &Client{
		url:       url,
		Events:    make(chan Event, 512),
		Connected: make(chan bool, 8),
	}
	c.markets.Store(&markets)
	return c
}

// UpdateMarkets replaces the subscription list; takes effect on next reconnect.
func (c *Client) UpdateMarkets(markets []string) {
	cp := make([]string, len(markets))
	copy(cp, markets)
	c.markets.Store(&cp)
}

// Run connects and maintains the WebSocket until ctx is cancelled.
// Reconnects with exponential backoff on any error.
func (c *Client) Run(ctx context.Context) {
	backoff := reconnectBase
	for {
		if err := c.connect(ctx); ctx.Err() != nil {
			return
		} else if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				if backoff < reconnectMax {
					backoff *= 2
				}
			}
		} else {
			backoff = reconnectBase
		}
	}
}

func (c *Client) connect(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.url, http.Header{})
	if err != nil {
		c.signal(false)
		return fmt.Errorf("dial: %w", err)
	}
	defer func() {
		conn.Close()
		c.signal(false)
	}()

	markets := *c.markets.Load()
	sub := SubscribeMsg{
		AssetIDs: markets,
		Type:     "market",
	}
	if err := c.writeJSON(conn, sub); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	c.signal(true)

	// The server can ping us at any time; reply with a pong and extend the read
	// deadline. All writes share writeMu because the client also sends heartbeats.
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPingHandler(func(appData string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		return conn.WriteMessage(websocket.PongMessage, []byte(appData))
	})

	readErr := make(chan error, 1)
	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}
			c.dispatch(raw)
		}
	}()

	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pingTicker.C:
			if err := c.writeJSON(conn, map[string]any{}); err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
		case err := <-readErr:
			return err
		}
	}
}

// dispatch parses raw bytes as either a JSON array or a single Event.
func (c *Client) dispatch(raw []byte) {
	// Polymarket may send an array of events or a single event.
	var events []Event
	if err := json.Unmarshal(raw, &events); err != nil {
		var e Event
		if err2 := json.Unmarshal(raw, &e); err2 == nil && e.EventType != "" {
			events = []Event{e}
		}
	}
	for _, e := range events {
		select {
		case c.Events <- e:
		default:
			// drop rather than block; reconciler will catch up via periodic sync
		}
	}
}

func (c *Client) signal(connected bool) {
	select {
	case c.Connected <- connected:
	default:
	}
}

func (c *Client) writeJSON(conn *websocket.Conn, v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return conn.WriteJSON(v)
}

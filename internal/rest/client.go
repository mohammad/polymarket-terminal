package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"polymarket-terminal/internal/orderbook"

	"github.com/shopspring/decimal"
)

// Client fetches REST snapshots from the Polymarket CLOB API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type bookResponse struct {
	Bids []priceLevel `json:"bids"`
	Asks []priceLevel `json:"asks"`
	Hash string       `json:"hash"`
}

type priceLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// FetchBook retrieves the current orderbook snapshot for the given token ID.
func (c *Client) FetchBook(ctx context.Context, assetID string) (bids, asks []orderbook.Level, hash string, err error) {
	url := fmt.Sprintf("%s/book?token_id=%s", c.baseURL, assetID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, "", fmt.Errorf("unexpected HTTP status %d for asset %s", resp.StatusCode, assetID)
	}

	var br bookResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		return nil, nil, "", fmt.Errorf("decode: %w", err)
	}

	for _, l := range br.Bids {
		p, _ := decimal.NewFromString(l.Price)
		s, _ := decimal.NewFromString(l.Size)
		if !s.IsZero() {
			bids = append(bids, orderbook.Level{Price: p, Size: s})
		}
	}
	for _, l := range br.Asks {
		p, _ := decimal.NewFromString(l.Price)
		s, _ := decimal.NewFromString(l.Size)
		if !s.IsZero() {
			asks = append(asks, orderbook.Level{Price: p, Size: s})
		}
	}

	return bids, asks, br.Hash, nil
}

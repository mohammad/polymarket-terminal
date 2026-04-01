package ui

import (
	"testing"
	"time"

	"polymarket-terminal/internal/db"
	"polymarket-terminal/internal/orderbook"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSwitcherMovesCursorWithoutSwitchingUntilEnter(t *testing.T) {
	var switchedTo string
	model := New([]db.Market{
		{AssetID: "a1", Label: "Market 1"},
		{AssetID: "a2", Label: "Market 2"},
		{AssetID: "a3", Label: "Market 3"},
	}, "a1", func(assetID string) {
		switchedTo = assetID
	}, nil)

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m := next.(Model)
	if !m.switchMode {
		t.Fatalf("switcher should open")
	}
	if m.switchIdx != m.marketIdx {
		t.Fatalf("switch cursor = %d, want %d", m.switchIdx, m.marketIdx)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.switchIdx != 1 {
		t.Fatalf("switch cursor = %d, want 1", m.switchIdx)
	}
	if m.marketIdx != 0 {
		t.Fatalf("active market changed early: %d", m.marketIdx)
	}
	if switchedTo != "" {
		t.Fatalf("switch callback fired before enter: %q", switchedTo)
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.switchMode {
		t.Fatalf("switcher should close after enter")
	}
	if m.marketIdx != 1 {
		t.Fatalf("active market = %d, want 1", m.marketIdx)
	}
	if cmd == nil {
		t.Fatalf("expected switch command on enter")
	}
	_ = cmd()
	if switchedTo != "a2" {
		t.Fatalf("switch callback = %q, want a2", switchedTo)
	}
}

func TestEscapeCancelsPendingSelection(t *testing.T) {
	model := New([]db.Market{
		{AssetID: "a1", Label: "Market 1"},
		{AssetID: "a2", Label: "Market 2"},
	}, "a1", nil, nil)

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m := next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.switchMode {
		t.Fatalf("switcher should close on esc")
	}
	if m.marketIdx != 0 {
		t.Fatalf("active market = %d, want 0", m.marketIdx)
	}
	if m.switchIdx != m.marketIdx {
		t.Fatalf("switch cursor should reset to active market")
	}
}

func TestSwitchingMsgClearsStaleBookState(t *testing.T) {
	model := New([]db.Market{{AssetID: "a1", Label: "Market 1"}}, "a1", nil, nil)
	model.bids = []orderbook.Level{{}}
	model.asks = []orderbook.Level{{}}
	model.hash = "hash-1"
	model.lastUpdate = time.Now()

	next, _ := model.Update(SwitchingMsg{})
	m := next.(Model)

	if !m.switching {
		t.Fatalf("switching flag should be set")
	}
	if len(m.bids) != 0 || len(m.asks) != 0 {
		t.Fatalf("book should be cleared while switching")
	}
	if m.hash != "" {
		t.Fatalf("hash = %q, want cleared hash", m.hash)
	}
	if !m.lastUpdate.IsZero() {
		t.Fatalf("last update should be reset while switching")
	}
}

func TestSwitcherSearchFiltersMarkets(t *testing.T) {
	model := New([]db.Market{
		{AssetID: "a1", Label: "Alpha"},
		{AssetID: "a2", Label: "Beta"},
		{AssetID: "a3", Label: "Gamma"},
	}, "a1", nil, nil)

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m := next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = next.(Model)

	if len(m.filtered) != 1 {
		t.Fatalf("filtered markets = %d, want 1", len(m.filtered))
	}
	if m.markets[m.filtered[0]].Label != "Gamma" {
		t.Fatalf("filtered market = %q, want Gamma", m.markets[m.filtered[0]].Label)
	}
}

func TestRefreshKeyRunsRefreshCommand(t *testing.T) {
	called := false
	model := New([]db.Market{{AssetID: "a1", Label: "Alpha"}}, "a1", nil, func() {
		called = true
	})

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatalf("expected refresh command")
	}
	_ = cmd()
	if !called {
		t.Fatalf("refresh callback was not called")
	}
}

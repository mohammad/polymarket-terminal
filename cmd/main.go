package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"polymarket-terminal/internal/config"
	"polymarket-terminal/internal/db"
	"polymarket-terminal/internal/orderbook"
	"polymarket-terminal/internal/reconciler"
	"polymarket-terminal/internal/rest"
	"polymarket-terminal/internal/ui"
	"polymarket-terminal/internal/ws"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// ── DB ────────────────────────────────────────────────────────────────────
	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	markets, err := store.ListMarkets(ctx)
	if err != nil || len(markets) == 0 {
		slog.Error("no active markets found; add rows to subscribed_markets", "err", err)
		os.Exit(1)
	}

	// ── WS client — subscribe to all known markets upfront ────────────────────
	assetIDs := make([]string, len(markets))
	for i, m := range markets {
		assetIDs[i] = m.AssetID
	}
	wsClient := ws.NewClient(cfg.WSURL, assetIDs)

	// ── REST client ───────────────────────────────────────────────────────────
	restClient := rest.NewClient(cfg.RESTURL)

	// ── Initial book ──────────────────────────────────────────────────────────
	book := orderbook.New(markets[0].AssetID)

	// ── Declare variables up-front so closures can capture them ─────────────────
	var prog *tea.Program
	var rec *reconciler.Reconciler

	// switchFn is called by the TUI when the user picks a different market.
	// rec and prog are set before the TUI event loop starts, so this is safe.
	switchFn := func(assetID string) {
		newBook := orderbook.New(assetID)
		rec.SwitchMarket(newBook)    // reconciler picks it up immediately
		prog.Send(ui.SwitchingMsg{}) // clear stale display while resyncing
	}

	uiModel := ui.New(markets, switchFn)
	prog = tea.NewProgram(uiModel, tea.WithAltScreen())

	// ── Reconciler ────────────────────────────────────────────────────────────
	rec = reconciler.New(book, wsClient, restClient, store, reconciler.Config{
		SyncInterval:  cfg.SyncInterval,
		WriteInterval: cfg.DBWriteInterval,
		OnUpdate: func(bids, asks []orderbook.Level, hash string) {
			prog.Send(ui.BookUpdateMsg{Bids: bids, Asks: asks, Hash: hash})
		},
	})

	// Forward WS connection state to the TUI.
	go func() {
		for connected := range wsClient.Connected {
			prog.Send(ui.ConnectionMsg{Connected: connected})
		}
	}()

	// ── Start background goroutines ───────────────────────────────────────────
	go wsClient.Run(ctx)
	go rec.Run(ctx)

	// ── Run TUI (blocks until quit) ───────────────────────────────────────────
	if _, err := prog.Run(); err != nil {
		slog.Error("tui", "err", err)
		os.Exit(1)
	}
}

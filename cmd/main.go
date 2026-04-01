package main

import (
	"context"
	"errors"
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
	"github.com/jackc/pgx/v5"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

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

	activeAssetID := markets[0].AssetID
	if saved, err := store.LoadLastViewedMarket(ctx); err == nil && indexForAsset(markets, saved) >= 0 {
		activeAssetID = saved
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("load last viewed market", "err", err)
	}

	assetIDs := make([]string, len(markets))
	for i, market := range markets {
		assetIDs[i] = market.AssetID
	}
	wsClient := ws.NewClient(cfg.WSURL, assetIDs)
	restClient := rest.NewClient(cfg.RESTURL)

	var prog *tea.Program
	var rec *reconciler.Reconciler

	switchFn := func(assetID string) {
		if err := store.SaveLastViewedMarket(context.Background(), assetID); err != nil {
			slog.Warn("save last viewed market", "asset", assetID, "err", err)
		}
		rec.SwitchMarket(assetID)
		prog.Send(ui.SwitchingMsg{})
	}

	refreshFn := func() {
		rec.ForceResync()
		prog.Send(ui.SyncStatusMsg{
			Message: "manual resync requested",
			Source:  "manual",
			Syncing: true,
		})
	}

	uiModel := ui.New(markets, activeAssetID, switchFn, refreshFn)
	prog = tea.NewProgram(uiModel, tea.WithAltScreen())

	rec = reconciler.New(wsClient.Events, restClient, store, reconciler.Config{
		ActiveAssetID: activeAssetID,
		AssetIDs:      assetIDs,
		SyncInterval:  cfg.SyncInterval,
		WriteInterval: cfg.DBWriteInterval,
		OnUpdate: func(assetID string, bids, asks []orderbook.Level, hash string) {
			prog.Send(ui.BookUpdateMsg{Bids: bids, Asks: asks, Hash: hash})
		},
		OnStatus: func(status reconciler.Status) {
			prog.Send(ui.SyncStatusMsg{
				Message: status.Message,
				Error:   status.Error,
				Source:  status.Source,
				Syncing: status.Syncing,
			})
		},
	})

	go func() {
		for connected := range wsClient.Connected {
			prog.Send(ui.ConnectionMsg{Connected: connected})
		}
	}()

	go wsClient.Run(ctx)
	go rec.Run(ctx)

	if _, err := prog.Run(); err != nil {
		slog.Error("tui", "err", err)
		os.Exit(1)
	}
}

func indexForAsset(markets []db.Market, assetID string) int {
	for i, market := range markets {
		if market.AssetID == assetID {
			return i
		}
	}
	return -1
}

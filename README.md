# polymarket-terminal

A terminal UI for watching live Polymarket orderbooks, built with Go + Bubbletea.

```
 POLYMARKET  ● LIVE  Netanyahu out by Mar 31 (YES)
 ──────────────────────────────────────────────────────────────────────────

  ASKS
  PRICE         SIZE
  ─────────────────────────
  0.9200        3412.50
  0.9100        8201.00
  0.9000        15032.75
  ── SPREAD 0.0100 ──
  ─────────────────────────
  PRICE         SIZE
  BIDS
  0.8900        12100.00
  0.8800        5340.25

  hash: 3fa2b91c…  updated: 0.4s ago

 [s] switch  [↑/↓] navigate  [esc] cancel  [q] quit
```

## Architecture

```
cmd/main.go
│
├── ws.Client        — WebSocket connection to Polymarket CLOB feed
├── rest.Client      — REST snapshots from clob.polymarket.com
├── reconciler       — syncs WS deltas against periodic REST snapshots
├── db               — Postgres persistence (subscribed markets + snapshots)
└── ui               — Bubbletea TUI (orderbook display + market switcher)
```

- **WS feed** subscribes to all markets on startup and streams real-time price/size deltas.
- **Reconciler** applies deltas to an in-memory orderbook, re-syncs via REST every `SYNC_INTERVAL`, and writes snapshots to Postgres every `DB_WRITE_INTERVAL`.
- **TUI** renders asks (red), bids (green), spread, and connection status. Press `s` to open the market switcher.

## Requirements

- Go 1.22+
- Docker (for Postgres)

## Setup

```bash
cp .env.example .env
make run          # starts Postgres, runs migrations, builds + launches the TUI
```

`make run` calls `docker compose up`, applies `migrations/001_init.sql`, then starts the binary.

### Environment variables

| Variable           | Default                                          | Description                        |
|--------------------|--------------------------------------------------|------------------------------------|
| `DATABASE_URL`     | `postgres://poly:poly@localhost:5432/polymarket` | Postgres connection string         |
| `WS_URL`           | `wss://ws-subscriptions-clob.polymarket.com/...` | Polymarket CLOB WebSocket endpoint |
| `REST_URL`         | `https://clob.polymarket.com`                    | Polymarket CLOB REST base URL      |
| `SYNC_INTERVAL`    | `30s`                                            | How often to re-sync from REST     |
| `DB_WRITE_INTERVAL`| `5s`                                             | How often to persist snapshots     |

## Adding / changing markets

Markets are stored in the `subscribed_markets` Postgres table. Each row maps a Polymarket **ERC-1155 token ID** (the YES or NO outcome token) to a display label.

```sql
INSERT INTO subscribed_markets (asset_id, label)
VALUES ('42750054...', 'US forces Iran by Mar 31 (YES)');
```

To find token IDs for any event, query the Gamma API:

```bash
curl "https://gamma-api.polymarket.com/markets?active=true&closed=false&order=volume24hr&ascending=false&limit=20" \
  | python3 -c "import json,sys; [print(m['question'], json.loads(m['clobTokenIds'])) for m in json.load(sys.stdin)]"
```

The seed in `migrations/001_init.sql` contains the top 15 markets by 24h volume as of 2026-03-31.

## Key bindings

| Key         | Action                          |
|-------------|--------------------------------|
| `s`         | Toggle market switcher          |
| `↑` / `k`   | Previous market (switcher open) |
| `↓` / `j`   | Next market (switcher open)     |
| `enter`     | Confirm selection               |
| `esc`       | Close switcher                  |
| `q` / `ctrl+c` | Quit                         |

## Makefile targets

| Target   | Description                            |
|----------|----------------------------------------|
| `make up`    | Start Postgres container           |
| `make down`  | Stop Postgres container            |
| `make reset` | Wipe DB volume and restart fresh   |
| `make build` | Build binary to `bin/`             |
| `make run`   | `up` + `build` + launch            |

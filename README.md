# polymarket-terminal

A Go + Bubble Tea terminal client for watching live Polymarket CLOB orderbooks.

The app subscribes to a configured set of outcome token IDs, keeps warm in-memory books from the shared websocket feed, periodically reconciles the active market against REST snapshots, persists snapshots to Postgres, and renders a low-latency terminal UI with market switching, search, resync, and stale-feed indicators.

```text
 POLYMARKET  ● LIVE  US forces Iran by Mar 31 (YES)  bid 0.0120  ask 0.0150
 ──────────────────────────────────────────────────────────────────────────────

  ASKS
  PRICE       SIZE
  ──────────────────────────
  0.0200      12000.00
  0.0180      5300.25
  0.0150      9000.00
  ── SPREAD 0.0030 ──
  ──────────────────────────
  PRICE       SIZE
  BIDS
  0.0120      10450.00
  0.0100      8750.50

  hash: a2ea88b1...  updated: 0.4s ago

 [s] switch  [r] resync  [↑/↓] navigate  [esc] cancel  [q] quit
```

## Features

- Live websocket orderbook updates from Polymarket CLOB.
- Warm per-market books so switching is fast after the first feed update.
- REST reconciliation on startup, on manual refresh, and on a fixed interval.
- Snapshot persistence in Postgres for fast cold starts.
- Last viewed market persistence across restarts.
- Market switcher with type-to-filter search.
- Terminal status hints for syncing, stale data, and refresh failures.

## Requirements

- Go 1.22+
- Docker / Docker Compose

## Quick start

```bash
cp .env.example .env
make run
```

`make run` does three things:

1. Starts Postgres with Docker.
2. Applies every SQL file in [`migrations/`](/Users/mohammadsyed/Source/polymarket-terminal/migrations) in lexical order.
3. Builds and launches the TUI.

## Configuration

Environment variables are loaded from `.env` if present.

| Variable | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | `postgres://poly:poly@localhost:5432/polymarket?sslmode=disable` | Postgres connection string |
| `WS_URL` | `wss://ws-subscriptions-clob.polymarket.com/ws/market` | Polymarket CLOB websocket endpoint |
| `REST_URL` | `https://clob.polymarket.com` | Polymarket REST API base URL |
| `SYNC_INTERVAL` | `30s` | Interval for active-market REST reconciliation |
| `DB_WRITE_INTERVAL` | `5s` | Interval for flushing dirty books to Postgres |

## Usage

### Key bindings

| Key | Action |
|---|---|
| `s` | Open / close market switcher |
| `↑` / `k` | Move switcher cursor up |
| `↓` / `j` | Move switcher cursor down |
| `enter` | Confirm selected market |
| `esc` | Cancel market switch |
| `r` | Force REST resync of the active market |
| `q` / `ctrl+c` | Quit |

### Market switching

- Press `s` to open the market switcher.
- Type to filter by label or asset ID.
- Use arrows or `j` / `k` to move.
- Press `enter` to switch.
- Press `esc` to cancel.

### Feed status

The UI distinguishes a few important runtime states:

- `● LIVE`: websocket connection is currently up.
- `● DISC`: websocket connection is currently disconnected.
- `syncing book...`: active market is waiting on a fresh book snapshot.
- `feed stale`: the active book has not changed for more than 15 seconds.
- footer error text: the most recent REST sync failed.

## Architecture

### High-level flow

```text
                +----------------------+
                |   Polymarket WS      |
                | market subscriptions |
                +----------+-----------+
                           |
                           v
                   +---------------+
                   |   ws.Client   |
                   | reconnecting  |
                   | heartbeat     |
                   +-------+-------+
                           |
                           v
                +----------------------+
                | reconciler.Reconciler|
                | - warm books per ID  |
                | - active market      |
                | - REST reconciliation|
                | - snapshot persistence|
                +----+-------------+---+
                     |             |
          updates/status           | snapshots/state
                     |             v
                     |      +-------------+
                     |      |   db.Store  |
                     |      | Postgres    |
                     |      +-------------+
                     v
               +-------------+
               |   ui.Model  |
               | Bubble Tea  |
               +-------------+
```

### Runtime responsibilities

#### [`cmd/main.go`](/Users/mohammadsyed/Source/polymarket-terminal/cmd/main.go)

- Loads config.
- Connects to Postgres.
- Loads configured markets from `subscribed_markets`.
- Restores the last viewed market from `app_state` when available.
- Starts the websocket client, reconciler, and Bubble Tea program.
- Bridges reconciler status/update callbacks into Bubble Tea messages.

#### [`internal/ws/client.go`](/Users/mohammadsyed/Source/polymarket-terminal/internal/ws/client.go)

- Maintains a persistent websocket connection.
- Subscribes to all configured token IDs at startup.
- Handles reconnect backoff.
- Sends heartbeats and responds to server pings.
- Emits parsed websocket events and connection state changes.

#### [`internal/rest/client.go`](/Users/mohammadsyed/Source/polymarket-terminal/internal/rest/client.go)

- Fetches authoritative orderbook snapshots from `/book?token_id=...`.
- Converts REST price levels into internal orderbook levels.

#### [`internal/orderbook/orderbook.go`](/Users/mohammadsyed/Source/polymarket-terminal/internal/orderbook/orderbook.go)

- Thread-safe in-memory orderbook.
- Supports full snapshot replacement and incremental delta updates.
- Produces sorted bid and ask slices for rendering/persistence.

#### [`internal/reconciler/reconciler.go`](/Users/mohammadsyed/Source/polymarket-terminal/internal/reconciler/reconciler.go)

- Owns the write path for all in-memory books.
- Maintains one book per configured asset ID.
- Treats one market as “active” for UI and periodic sync purposes.
- Seeds books from Postgres snapshots on cold start.
- Applies websocket `book` and `price_change` updates.
- Reconciles the active market against REST on startup, on interval, and on manual `r`.
- Persists dirty books to Postgres on a fixed interval.
- Emits both book updates and sync status to the UI.

#### [`internal/ui/model.go`](/Users/mohammadsyed/Source/polymarket-terminal/internal/ui/model.go)

- Renders the terminal orderbook.
- Shows connection state, best bid/ask, spread, hash age, and stale status.
- Implements the searchable market switcher.
- Handles manual resync and switch/refresh commands through Bubble Tea commands.

## Reconciliation model

The app uses a hybrid consistency model:

1. Postgres snapshot is loaded first to give the UI immediate data on startup.
2. A REST snapshot is fetched and applied to establish an authoritative baseline.
3. Websocket deltas continuously mutate warm in-memory books.
4. The active market is periodically checked against REST using `SYNC_INTERVAL`.
5. Dirty books are written back to Postgres using `DB_WRITE_INTERVAL`.

This gives a good tradeoff between startup speed, low-latency updates, and eventual correction if websocket events are dropped or out of order.

## Database schema

SQL migrations live in [`migrations/`](/Users/mohammadsyed/Source/polymarket-terminal/migrations).

### `subscribed_markets`

Defined in [`migrations/001_init.sql`](/Users/mohammadsyed/Source/polymarket-terminal/migrations/001_init.sql).

```sql
CREATE TABLE IF NOT EXISTS subscribed_markets (
    asset_id   TEXT        PRIMARY KEY,
    label      TEXT        NOT NULL DEFAULT '',
    active     BOOLEAN     NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Purpose:

- Stores the set of Polymarket ERC-1155 outcome token IDs the app should subscribe to.
- `label` is the human-readable name shown in the UI.
- `active = true` rows are loaded at startup.

### `orderbook_snapshots`

Defined in [`migrations/001_init.sql`](/Users/mohammadsyed/Source/polymarket-terminal/migrations/001_init.sql).

```sql
CREATE TABLE IF NOT EXISTS orderbook_snapshots (
    asset_id   TEXT        PRIMARY KEY,
    bids       JSONB       NOT NULL DEFAULT '[]',
    asks       JSONB       NOT NULL DEFAULT '[]',
    hash       TEXT        NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Purpose:

- Stores the last persisted orderbook snapshot for each subscribed market.
- `bids` and `asks` are arrays of `{price, size}` JSON objects.
- Used for fast cold start before the first REST sync completes.

Example payload shape:

```json
[
  { "price": "0.012", "size": "10450.00" },
  { "price": "0.010", "size": "8750.50" }
]
```

### `app_state`

Defined in [`migrations/002_app_state.sql`](/Users/mohammadsyed/Source/polymarket-terminal/migrations/002_app_state.sql).

```sql
CREATE TABLE IF NOT EXISTS app_state (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Purpose:

- Stores lightweight application state that is not part of the book itself.
- Currently used for `last_viewed_market`.

Example row:

```text
key:   last_viewed_market
value: 42750054381142639205639663180818682570869285140532640407891991570656047928885
```

## Adding or changing markets

Markets are configured in `subscribed_markets`.

Example:

```sql
INSERT INTO subscribed_markets (asset_id, label)
VALUES ('42750054381142639205639663180818682570869285140532640407891991570656047928885',
        'US forces Iran by Mar 31 (YES)')
ON CONFLICT (asset_id) DO UPDATE
SET label = EXCLUDED.label, active = true;
```

To find token IDs from Polymarket’s Gamma API:

```bash
curl "https://gamma-api.polymarket.com/markets?active=true&closed=false&order=volume24hr&ascending=false&limit=20" \
  | python3 -c "import json,sys; [print(m['question'], json.loads(m['clobTokenIds'])) for m in json.load(sys.stdin)]"
```

The seed rows in [`migrations/001_init.sql`](/Users/mohammadsyed/Source/polymarket-terminal/migrations/001_init.sql) are the top 15 markets by 24h volume as of 2026-03-31.

## Make targets

| Target | Description |
|---|---|
| `make up` | Start Postgres container |
| `make down` | Stop Postgres container |
| `make reset` | Destroy Postgres volume and recreate from scratch |
| `make migrate` | Apply all SQL migrations to the running Postgres instance |
| `make clean` | Remove `bin/` |
| `make build` | Build the app into `bin/polymarket-terminal` |
| `make run` | Start DB, apply migrations, build, and launch the TUI |

## Testing

Run the full Go test suite:

```bash
go test ./...
```

Current automated coverage includes:

- websocket client message parsing and constructor behavior
- UI switcher, switching, search, and refresh command behavior
- reconciler warm-book switching and forced resync status behavior

## Operational notes

- Docker’s `docker-entrypoint-initdb.d` only runs on a fresh volume, so this project uses `make migrate` to apply migrations to existing local databases too.
- The websocket feed can drop events under sustained pressure; the periodic REST reconciliation is the safety net.
- Only the active market is periodically REST-reconciled, but all configured markets stay warm from websocket traffic.

## Known limitations

- The app tracks one active market in the UI at a time.
- Only the active market gets periodic REST drift checks.
- Search in the switcher is substring-based and intentionally simple.
- Logging is process-local; there is no metrics export or remote observability yet.

# pacpool

Minimal Pingancoin pool control plane plus a first Stratum work server.

## Current scope

This version does four useful things:

- polls `pacd` for mining, network, and block-template state
- polls `pacdata` for indexer sync state
- exposes a small HTTP status surface for pool operations
- accepts basic Stratum miner sessions, validates shares, and forwards solved block candidates to `pacd`

The Stratum side is intentionally minimal for this stage. It supports:

- `mining.subscribe`
- `mining.authorize`
- `mining.notify`
- `mining.set_difficulty`
- `mining.submit`

At the moment, the pool uses per-worker VarDiff, persists its share ledger on local disk, tracks payout-ready rounds plus found-block attribution, and calculates payout previews per solved round. Extranonce fanout, actual payout execution, and miner dashboards come next.

## Run

```bash
go run ./cmd/pacpool \
  --pacd http://127.0.0.1:9509 \
  --pacdata http://127.0.0.1:9609 \
  --miningaddr SYourPoolPayoutAddress \
  --sharedifficulty 1 \
  --vardiff=true \
  --vardifftarget 15s \
  --datadir ./data \
  --listen 127.0.0.1:9809 \
  --stratumlisten 127.0.0.1:3333
```

## Routes

- `/`
- `/healthz`
- `/status`

## Status fields

`/status` includes pool readiness and live Stratum counters:

- `pool.ready_for_stratum`
- `pool.share_difficulty`
- `pool.vardiff_enabled`
- `pool.vardiff_target_sec`
- `pool.connected_miners`
- `pool.active_jobs`
- `pool.shares`
- `pool.workers`
- `pool.current_round`
- `pool.recent_rounds`
- `pool.pending_payouts`
- `pool.ledger_path`
- `pool.template`

## Development

```bash
go test ./...
go build ./...
```

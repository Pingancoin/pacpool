# pacpool

Minimal Pingancoin pool control plane plus a first Stratum work server.

## Current scope

This version does four useful things:

- polls `pacd` for mining, network, and block-template state
- polls `pacdata` for indexer sync state
- exposes a small HTTP status surface for pool operations
- accepts basic Stratum miner sessions and forwards solved block candidates to `pacd`

The Stratum side is intentionally minimal for this stage. It supports:

- `mining.subscribe`
- `mining.authorize`
- `mining.notify`
- `mining.set_difficulty`
- `mining.submit`

At the moment, shares are treated as full solved block candidates against the current network target. VarDiff, extranonce fanout, share accounting, payouts, and miner dashboards come next.

## Run

```bash
go run ./cmd/pacpool \
  --pacd http://127.0.0.1:9509 \
  --pacdata http://127.0.0.1:9609 \
  --miningaddr SYourPoolPayoutAddress \
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
- `pool.connected_miners`
- `pool.active_jobs`
- `pool.template`

## Development

```bash
go test ./...
go build ./...
```

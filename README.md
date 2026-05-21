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

At the moment, the pool uses per-worker VarDiff, persists its share ledger on local disk, tracks payout-ready rounds plus found-block attribution, calculates payout previews per solved round, and can mark payout batches executed in its ledger. Extranonce fanout, wallet-linked payout automation, and miner dashboards come next.

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
  --admintoken "$PACPOOL_ADMIN_TOKEN" \
  --listen 127.0.0.1:9809 \
  --stratumlisten 127.0.0.1:3333
```

Set `PACPOOL_ADMIN_TOKEN` in production. `/payouts/execute` requires the token
when configured and refuses empty `txid` values, so payout batches cannot be
accidentally marked paid without an operator-supplied transaction id.

## Routes

- `/`
- `/healthz`
- `/status`
- `/payouts`
- `/payouts/execute`

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

## Deployment

Production deployment templates live under `deploy/`. The default shape keeps
the HTTP control API local-only, exposes Stratum on a dedicated TCP port, and
blocks public access to payout execution at the reverse proxy layer.

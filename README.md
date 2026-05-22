# pacpool

Minimal Pingancoin pool control plane plus a first Stratum work server.

## Current scope

This version does four useful things:

- polls `pacd` for mining, network, and block-template state
- polls `pacdata` for indexer sync state
- exposes a small HTTP status surface plus a public dashboard for pool operations
- accepts basic Stratum miner sessions, validates shares, and forwards solved block candidates to `pacd`

The Stratum side is intentionally minimal for this stage. It supports:

- `mining.subscribe`
- `mining.authorize`
- `mining.notify`
- `mining.set_difficulty`
- `mining.submit`

At the moment, the pool uses per-worker VarDiff, persists its share ledger on local disk, tracks payout-ready rounds plus found-block attribution, calculates payout previews per solved round, can mark payout batches executed in its ledger, serves a multilingual miner status dashboard, and can call a local wallet service for automatic batch payouts. Extranonce fanout and richer miner account pages come next.

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
  --autopayout=false \
  --payoutwallet http://127.0.0.1:9810 \
  --listen 127.0.0.1:9809 \
  --stratumlisten 127.0.0.1:3333
```

Set `PACPOOL_ADMIN_TOKEN` in production. `/payouts/execute` requires the token
when configured and refuses empty `txid` values, so payout batches cannot be
accidentally marked paid without an operator-supplied transaction id.

Automatic payouts are disabled by default. To enable them, run `pacwallet` as a
local-only service on the same host and set:

- `PACPOOL_AUTO_PAYOUT=true`
- `PACPOOL_WALLET_URL=http://127.0.0.1:9810`
- `PACPOOL_WALLET_TOKEN` if the wallet service is token-protected
- `PACPOOL_WALLET_PASSPHRASE` only if the hot wallet is encrypted and the host is trusted
- `PACPOOL_PAYOUT_MIN_ATOMS` to require a minimum total pending batch before sending
- `PACPOOL_PAYOUT_INTERVAL`, for example `1h`

Miner usernames are treated as payout addresses. A worker suffix is allowed:
`P...` and `P....rig01` both pay the `P...` address.

## Routes

- `/` dashboard; accepts `?lang=en`, `?lang=zh-CN`, `?lang=ja`, and `?lang=ko`
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
- `pool.auto_payout`
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

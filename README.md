# pacpool

Minimal Pingancoin pool control plane.

## Phase 0

This first phase does three useful things:

- polls `pacd` for mining and network state
- polls `pacdata` for indexer sync state
- exposes a small HTTP status surface for pool operations

It is intentionally the control-plane foundation, not the final miner protocol yet. The next pool step is adding mining work/template RPC in `pacd`, then miner sessions, job broadcast, and share validation in `pacpool`.

## Run

```bash
go run ./cmd/pacpool \
  --pacd http://127.0.0.1:9509 \
  --pacdata http://127.0.0.1:9609 \
  --miningaddr SYourPoolPayoutAddress \
  --listen 127.0.0.1:9809
```

## Routes

- `/`
- `/healthz`
- `/status`

## Development

```bash
go test ./...
go build ./...
```

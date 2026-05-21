# pacpool Deployment

Run `pacpool` on a host that has local `pacd` RPC and `pacdata` available.
Keep the HTTP control API bound to `127.0.0.1`; expose only selected read-only
routes through a reverse proxy.

## Install

```bash
sudo install -m 0755 pacpool /usr/local/bin/pacpool
sudo useradd --system --home /var/lib/pacpool --shell /usr/sbin/nologin pacpool
sudo mkdir -p /var/lib/pacpool /etc/pingancoin
sudo chown -R pacpool:pacpool /var/lib/pacpool
sudo cp deploy/pacpool-mainnet.env.example /etc/pingancoin/pacpool-mainnet.env
sudo install -m 0644 deploy/systemd/pacpool-mainnet.service /etc/systemd/system/pacpool-mainnet.service
sudo systemctl daemon-reload
sudo systemctl enable --now pacpool-mainnet
```

Before enabling Stratum, set:

- `PACPOOL_MINING_ADDRESS`
- `PACPOOL_ADMIN_TOKEN`
- `PACPOOL_STRATUM_LISTEN`

## Verify

```bash
curl -s http://127.0.0.1:9809/healthz
curl -s http://127.0.0.1:9809/status
systemctl status pacpool-mainnet
```

## Public proxy

Use `deploy/nginx/pacpool-mainnet.conf.example` for read-only status routes.
The template blocks `/payouts/execute`; payout execution should stay behind a
private admin path plus `PACPOOL_ADMIN_TOKEN`.

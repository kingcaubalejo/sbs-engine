# SBS Engine — Lightsail Deployment Guide

The budget-tier deployment: ~$3.50/month total, single Amazon Lightsail
instance, Caddy for TLS, MongoDB Atlas M0 (free) for storage. Suitable
for low-traffic production with the resilience level "auto-restart on
process crash." For higher-availability topology (CloudFront + ALB + WAF)
see [DEPLOYMENT.md](DEPLOYMENT.md).

## Architecture

```
External registrar DNS  ──A──►  Lightsail static IP  (54.179.154.11)
                                       │
                              ┌────────▼────────┐
                              │ Lightsail nano  │
                              │ Amazon Linux 2023│
                              │                 │
                              │ Caddy :80,:443  │  ◄── Let's Encrypt auto-TLS
                              │   │             │
                              │   ▼ reverse_proxy
                              │ sbs-engine      │
                              │ :8080 (loopback)│
                              │ systemd, Restart=always
                              └────────┬────────┘
                                       │ MongoDB SRV
                                       ▼
                              MongoDB Atlas M0 (free)
```

## Cost

| Item | Monthly |
|---|--:|
| Lightsail $3.50 nano (1 vCPU, 512 MB, 20 GB SSD, 1 TB egress, static IP) | $3.50 |
| MongoDB Atlas M0 | $0.00 |
| Let's Encrypt cert (auto-renewed by Caddy) | $0.00 |
| External DNS | unchanged |
| **Total** | **$3.50** |

## Pre-flight checklist

Before the install steps below, verify all five:

1. **Lightsail instance running** — Amazon Linux 2023 blueprint, $3.50 plan, in your nearest region (`ap-southeast-1` for SG/PH).
2. **Static IP attached** — must be attached or it costs $0.005/hr.
3. **Lightsail firewall** — open 22, 80, 443. Console → instance → Networking.
4. **DNS A record** — `api.yourdomain.com → <static IP>` at your registrar. Verify with `dig +short api.yourdomain.com` from your laptop.
5. **Atlas Network Access** — allowlist the Lightsail static IP, otherwise the binary starts but every DB call fails.

## SSH access

```bash
chmod 400 ~/Desktop/SBSEngine.pem
ssh -i ~/Desktop/SBSEngine.pem ec2-user@<lightsail-static-ip>
```

If `Permission denied (publickey)`: the instance is using a different key
than `SBSEngine.pem`. Lightsail console → instance → Connect tab shows
which key it expects. Either download that one, or use the browser SSH to
add your key to `~/.ssh/authorized_keys`.

## First-time install (on the Lightsail instance)

### 1. Install Caddy

Caddy is **not** in Amazon Linux 2023's dnf repos. Install from binary:

```bash
CADDY_VERSION=2.8.4
curl -fsSL "https://github.com/caddyserver/caddy/releases/download/v${CADDY_VERSION}/caddy_${CADDY_VERSION}_linux_amd64.tar.gz" \
  | sudo tar -xz -C /usr/local/bin caddy
sudo chmod +x /usr/local/bin/caddy

sudo groupadd --system caddy
sudo useradd --system --gid caddy --create-home --home-dir /var/lib/caddy \
  --shell /usr/sbin/nologin --comment "Caddy web server" caddy
sudo mkdir -p /etc/caddy
sudo chown -R caddy:caddy /etc/caddy /var/lib/caddy

sudo curl -fsSL -o /etc/systemd/system/caddy.service \
  https://raw.githubusercontent.com/caddyserver/dist/master/init/caddy.service

# The official unit expects /usr/bin/caddy but we installed to /usr/local/bin/caddy
sudo sed -i 's|/usr/bin/caddy|/usr/local/bin/caddy|g' /etc/systemd/system/caddy.service

# Allow caddy to bind 80/443 without root
sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/caddy

sudo systemctl daemon-reload
```

### 2. Push the artifacts from your laptop

From your local repo:

```bash
cd ~/Desktop/workstation/sa_laboratoryo/prototypes/sbs-engine
SSH_HOST=<lightsail-ip> SSH_KEY=~/Desktop/SBSEngine.pem ./deploy.sh production push
```

This builds `main-linux`, rsyncs binary + Caddyfile + systemd unit + `.env`
to `~/sbs-engine/` on the box, and restarts the service.

### 3. Wire it up (on the Lightsail instance)

```bash
cd ~/sbs-engine
sudo cp Caddyfile /etc/caddy/Caddyfile
sudo nano /etc/caddy/Caddyfile   # replace api.example.com with your real subdomain

sudo cp sbs-engine.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now sbs-engine caddy

sudo systemctl status sbs-engine caddy --no-pager
```

Caddy will request a Let's Encrypt cert on first start. This succeeds
only if DNS already resolves to this host and ports 80/443 are open.
Watch the cert handshake: `sudo journalctl -u caddy -f`.

### 4. Smoke test

```bash
curl https://api.yourdomain.com/health
# → 200, JSON body with mongo health

curl -I https://api.yourdomain.com/volumes
# → 200, Cache-Control + ETag headers present
```

## Updates and operations

### Deploying a new binary

From your laptop:

```bash
SSH_HOST=<ip> SSH_KEY=~/Desktop/SBSEngine.pem ./deploy.sh production push
```

`deploy.sh` rebuilds, syncs, fixes perms, restarts systemd. Downtime is
~2 seconds (systemd restart + binary cold start).

### Editing environment variables

Env vars are read once at process start by the Go binary. Changes to
`.env` on the box take effect only after restart:

```bash
sudo nano ~/sbs-engine/.env
sudo systemctl restart sbs-engine
```

`systemctl reload sbs-engine` does **not** re-read the env file.

### Toggling Swagger UI

Off by default in production (`ENABLE_SWAGGER=false`) so endpoints aren't
advertised to scanners. To turn on temporarily:

```bash
sudo sed -i 's/ENABLE_SWAGGER=false/ENABLE_SWAGGER=true/' ~/sbs-engine/.env
sudo systemctl restart sbs-engine
# browse to https://api.yourdomain.com/swagger/
```

To gate it behind basic auth instead of leaving it public, add to
`/etc/caddy/Caddyfile` above the `reverse_proxy` line:

```
basic_auth /swagger/* {
    admin <bcrypt-hash-from-caddy-hash-password>
}
```

Generate the hash: `caddy hash-password`. Then `sudo systemctl reload caddy`.

### Rotating the JWT secret

The JWT secret is read once at startup. Rotation invalidates all live
tokens (users have to log in again):

```bash
NEW_SECRET=$(openssl rand -hex 32)
sudo sed -i "s/^JWT_SECRET=.*/JWT_SECRET=${NEW_SECRET}/" ~/sbs-engine/.env
sudo systemctl restart sbs-engine
```

Never embed `$(openssl rand -hex 32)` literally in `.env` — `load-env.sh`
sources the file as bash so the substitution would re-run on every load
and rotate the secret on every restart.

### Renewing TLS certs

Caddy auto-renews via Let's Encrypt ~30 days before expiry. No action
needed. Verify cert status with:

```bash
echo | openssl s_client -servername api.yourdomain.com -connect api.yourdomain.com:443 2>/dev/null \
  | openssl x509 -noout -dates
```

## Logs

```bash
sudo journalctl -u sbs-engine -f          # follow app logs
sudo journalctl -u caddy -f                # follow proxy logs
sudo journalctl -u sbs-engine --since "1 hour ago" | grep ERROR
```

The Go binary emits structured JSON via slog. Pipe through `jq` for
field-level filtering:

```bash
sudo journalctl -u sbs-engine -o cat | jq 'select(.level == "ERROR")'
```

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `caddy.service: Failed to locate executable /usr/bin/caddy` | Systemd unit points at `/usr/bin/caddy` but binary is at `/usr/local/bin/caddy` | `sudo sed -i 's\|/usr/bin/caddy\|/usr/local/bin/caddy\|g' /etc/systemd/system/caddy.service && sudo systemctl daemon-reload` |
| Caddy hangs on cert acquisition | DNS not resolving, ports 80/443 closed, or rate-limited by Let's Encrypt | `dig +short <host>`, check Lightsail firewall, check `journalctl -u caddy` for the actual ACME error |
| `503` on every write | `JWT_SECRET` empty in `.env` | Set it (see "Rotating the JWT secret"), restart |
| `connection refused` on /health | sbs-engine not running | `sudo systemctl status sbs-engine`, then `journalctl -u sbs-engine -n 50` |
| Mongo connection timeout in logs | Lightsail static IP not in Atlas Network Access | Atlas → Security → Network Access → add the IP |
| 403 from Caddy | CORS write rejected from non-allowlisted origin | Add origin to `CORS_ALLOWED_ORIGINS` in `.env`, restart |
| Permission denied on SSH | Wrong key or wrong user | See "SSH access" section |

## Security posture

What this deployment provides:

- TLS 1.2+ via Let's Encrypt (Caddy auto-config)
- Per-IP rate limiting at the app layer (`RATE_LIMIT_RPS=5`, burst 10)
- 64 KB body limit on writes
- CORS allowlist for write methods
- JWT-gated writes (fails closed with 503 if `JWT_SECRET` is empty)
- bcrypt password hashing for users (cost 12)
- `Server: ""`, security headers, panic recovery
- `.env` mode 600, owned by `ec2-user`

What this deployment does **not** provide (acceptable tradeoffs at this tier):

- No WAF (relies on app-layer limiter alone)
- No CloudFront edge caching (relies on Caddy's local response cache)
- No multi-AZ — instance loss = downtime until restored
- No automated backups (Atlas M0 has no PITR; binary state is stateless so only the host config matters)
- Secrets stored on the box, not in Secrets Manager

## When to graduate from this topology

Move to the [DEPLOYMENT.md](DEPLOYMENT.md) topology (CloudFront + ALB + WAF)
when any of these become true:

- Monthly traffic > 1 TB egress (Lightsail overage = $0.09/GB)
- Outbound abuse alarm fires more than once per month (need WAF)
- Need multiple environments (staging) — second Lightsail nano = +$3.50/mo, still cheaper than ALB
- Need multi-AZ failover (any single ALB-fronted setup beats this)
- Atlas M0's 512 MB cap is hit — upgrade to M2 ($9/mo) for backups

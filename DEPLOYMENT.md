# SBS Engine — AWS Deployment Guide

The API is public and unauthenticated. Most cost-protection comes from infrastructure, not application code. This document captures the deploy-side recommendations that pair with the application changes already in the repo.

## Topology

Recommended:

```
Internet ──► CloudFront (cache + WAF) ──► ALB (TLS) ──► EC2 (Go binary) ──► MongoDB Atlas
```

Why each layer:

- **CloudFront** caches GETs at the edge. With the `Cache-Control: s-maxage=...` headers the app already sends, ~99% of repeat traffic is served without ever touching the origin.
- **WAF** (attached to CloudFront) provides global rate limiting, AWS managed rule sets, and bot/UA blocking. Cheaper than handling this at the app layer.
- **ALB** terminates TLS and shields the EC2 instance behind a security group that only allows the ALB's SG. AWS Shield Standard is automatic at the ALB layer.
- **EC2** runs the Go binary via `sbs-engine.service` (already in the repo). A `t4g.small` (ARM, ~$12/mo) is plenty for this workload after caching is in place.

## CloudFront

- Origin: ALB (preferred) or EC2 IP (acceptable for a v0).
- Cache policy: `CachingOptimized` works; CloudFront will respect `Cache-Control` and `ETag` headers the app already emits.
- Origin request policy: forward `Origin`, `Accept-Encoding`, and `Authorization` (latter is a no-op today but future-proof).
- Compress objects automatically (CloudFront gzip + brotli). The app also has gzip middleware as a fallback for direct hits.
- Geo-restriction: if your audience is regional (e.g. SE Asia + US), allowlist those countries. This is a large cost reduction against global scrapers.

## AWS WAF

Attach a WebACL to the CloudFront distribution. Free baseline:

- `AWS-AWSManagedRulesCommonRuleSet` — common attack patterns.
- `AWS-AWSManagedRulesKnownBadInputsRuleSet` — known malicious inputs.

Custom rules:

- **Rate-based rule**: 2000 requests / 5 min per IP. The app-level limiter (5 RPS / IP by default) is the precision filter; this is the dumb global cap.
- **Block empty User-Agent** and known scraper UAs.
- **Block requests where `Origin` matches obvious junk** (data URIs, file://, etc.).

## ALB

- Single HTTPS listener (443) with an ACM certificate (free).
- HTTP → HTTPS redirect on port 80.
- Target group: EC2 instance(s), health check on `GET /health` (the app's health endpoint pings MongoDB before responding).
- Security group: only the ALB SG can talk to the EC2 instance on port 8080.
- Set `TRUSTED_PROXY_CIDRS` in the EC2 environment to the ALB SG's source CIDR so `X-Forwarded-For` is trusted.

## EC2

Already documented:

- `make build-linux` produces a stripped (`-ldflags="-s -w" -trimpath`) binary.
- `sbs-engine.service` runs it as a systemd daemon under `ec2-user`.
- `deploy.sh` and `ec2-setup.sh` exist for manual deploy.

Hardening to consider:

- **Secrets**: `.env.production` currently lives on the instance. Move credentials to AWS Secrets Manager or SSM Parameter Store and have the systemd unit read them at start. Rotate the leaked Atlas password if it has ever been outside the repo.
- **systemd resource limits**: add `MemoryMax=512M` and `TasksMax=512` to the unit so a runaway process cannot starve the host.
- **Ulimits**: `LimitNOFILE=65535` for high-connection workloads.
- **Auto-recovery**: enable EC2 status-check auto-recovery.

## MongoDB Atlas

- Use `mongodb+srv://` (already wired via `MONGO_USE_SRV=true`).
- Use Atlas IP allowlist — restrict to the EC2's elastic IP (or NAT gateway IP if private).
- Enable Atlas Performance Advisor and review suggested indexes weekly.
- For light public traffic, M0 (free) or M2 is fine. M10 if Performance Advisor flags sustained CPU.

## CloudWatch alarms

Set these as a starting kit:

| Alarm | Threshold |
|-------|-----------|
| 5xx rate | > 1% over 5 min |
| 4xx rate | > 10% over 5 min (likely scrape signal) |
| ALB target unhealthy | any |
| EC2 CPU | > 80% for 5 min |
| EC2 outbound bytes | > N GB/hour (set N from baseline) |
| Atlas CPU | > 70% for 5 min |
| AWS Budget | 50%, 80%, 100% of monthly budget |

The most useful single alarm for an unauthenticated API is the **outbound-bytes** alarm — it catches both legitimate viral traffic and abuse before they show up on the bill.

## Cost notes

Rough monthly costs at modest traffic (~1M requests/mo, 99% cached):

| Item | Cost |
|------|------|
| EC2 `t4g.small` (1 yr RI) | ~$8 |
| ALB | ~$16 |
| CloudFront (10 GB egress, mostly cached) | ~$1 |
| WAF (1 ACL + 3 rules) | ~$11 |
| Atlas M0 | $0 |
| **Total** | **~$36** |

The WAF is the largest single line. If budget is tighter than this, drop WAF and keep CloudFront + ALB; the app's per-IP limiter still does the heavy lifting.

## Application-level safeguards already in place

These were added in the same change set as this document; they pair with the infra above so the API is defensible even if a layer is misconfigured:

- Per-IP token-bucket rate limiter (default 5 RPS / 10 burst, env-tunable; expensive routes have stricter overrides).
- 64 KB default body limit on POST/PUT/PATCH (`BODY_LIMIT_BYTES`).
- 1 MiB header limit; 5 s `ReadHeaderTimeout` to defeat Slowloris.
- Panic recovery middleware — Mongo blips no longer crash the binary.
- TTL caches on `/stats`, `/languages`, `/volumes`, `/donate`, `/volumes/{id}`.
- `$text` index search replacing the regex COLLSCAN; results capped at 50.
- Result caps on `GetVolumes` (200) and `GetBooksByVolume` (200).
- `Cache-Control` and weak `ETag` on safe responses; `If-None-Match` → 304.
- Gzip compression for clients that advertise it.
- CORS open for GETs, env-allowlist for writes.
- Swagger UI gated behind `ENABLE_SWAGGER=true` (off by default in production).
- Generic `Server: ""`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` headers.
- `/robots.txt` advertising `Disallow: /`.
- Programmatic Mongo index creation on startup.
- Mongo client tuned: `MaxPoolSize=50`, `MinPoolSize=5`, server-selection / connect / socket timeouts set, retryable reads + writes enabled.
- Structured `slog` JSON logging with request IDs.

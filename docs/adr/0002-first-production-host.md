# 2. First production host — one small VPS in Singapore

**Status:** Accepted — 2026-08-10

Applies [ADR 1](0001-optimise-for-exit-cost.md) to a concrete purchase. That ADR
holds for decades; this one is expected to be superseded when the host changes,
which is the point of separating them.

## Context

Hearth needs somewhere to run. Slice 2 (Money) is complete and browser-walked,
self-serve sign-up works, so a stranger can already create a household and use
the product — there is something worth deploying.

Constraints:

- **Cost.** Lower than the ~S$20/month already going to AWS.
- **Region.** The household is in Singapore and the product is SGD/IDR, so
  South-East Asia beats Europe or the US for latency.
- **Shape.** The app is a Compose stack — nginx serving the built SPA *and*
  proxying `/api` same-origin, a Go API, Postgres on a volume. Anything that
  splits those apart costs a rewrite (ADR 1).
- **Availability.** One household, no HA requirement. An hour of downtime is an
  inconvenience, not an incident.

## Decision

| Piece | Choice | Why |
|---|---|---|
| Compute | **Hetzner Cloud CPX11, Singapore region** | Cheapest credible VPS with a SEA region. Runs the whole stack; a household's Postgres is a few MB for years |
| TLS | **Caddy in front, as the edge** | Automatic Let's Encrypt issuance *and renewal*, forever, with no certbot cron to rot. `web/nginx.conf` stays exactly as it is |
| Database | **Postgres in the Compose stack, on the same box** | ADR 1 — plain Postgres we own. Not a managed service, and explicitly not a second provider |
| Backups | **Nightly `pg_dump` in plain SQL to Backblaze B2 or Cloudflare R2**, plus Hetzner snapshots | The off-provider dump is the real backup; snapshots are for fast recovery. Free at this data size |
| Mail | **Resend**, free plan | 3,000/month, 100/day, one verified domain, SMTP relay. Fits the existing `SMTP_ADDR` / `SMTP_USERNAME` / `SMTP_PASSWORD` configuration surface with no code change |
| Domain | An at-cost registrar (Cloudflare Registrar, or Porkbun), registered long, auto-renew on | See "the domain is the fragile part" below |

Roughly **S$10–13/month** all in, against S$20 today. Prices moved twice in 2026
(Hetzner raised in April and again on 15 June) — **confirm the current figure at
purchase rather than trusting this table.**

## Consequences

**Caddy in front of nginx is two proxies, and that breaks the sign-up rate
limiter unless configured.** `web/nginx.conf` overwrites `X-Real-IP` with
`$remote_addr` and strips `True-Client-IP` specifically so a client cannot spoof
the per-IP limiter's key. With Caddy in front, `$remote_addr` becomes *Caddy's*
address on every request, so `middleware.RealIP` keys every caller to one value
and the per-IP limit degrades into a single global bucket. The mitigation is two
lines in `nginx.conf` — `set_real_ip_from <Caddy's address>` and
`real_ip_header X-Forwarded-For` — and it is mandatory, not optional. See
`docs/SYSTEM_DESIGN.md` §1.

> **Implemented 2026-08-11.** Three lines, not two: `real_ip_recursive off` is
> stated explicitly because it is load-bearing. `set_real_ip_from` trusts the
> whole `172.28.0.0/16` compose subnet rather than a `/32` for Caddy, since
> Docker assigns Caddy's address from that subnet — `docs/SYSTEM_DESIGN.md` §1
> records why the wider range is accepted. This ADR also assumed Caddy appends
> the peer to `X-Forwarded-For`; it replaces the header instead, preserving a
> caller's list only for callers in `trusted_proxies`, of which none are
> configured. The mitigation still holds — `off` takes the last entry either
> way — but the reason differs from the one written above.

**One box is a single point of failure, accepted.** Recovery is: new box, clone
the repo, restore the dump, `docker compose up`, repoint DNS. This should be
performed once and timed before it is needed for real.

**The domain is the fragile part, not the server.** `APP_BASE_URL` is embedded
in every magic link and invite; SPF/DKIM bind to the domain; the cookie origin
is the domain. A dead server restores in an hour. A domain that lapses and is
re-registered by someone else is gone permanently, taking the only
account-recovery path this product has with it. Register long, auto-renew, and
send expiry notices to an address that is *not* hosted on that domain.

**Production still cannot administer itself.** The prod image is
`distroless/static-debian12:nonroot` with `ENTRYPOINT ["/app/api"]` — no shell,
no `goose`, no `adminctl`. As shipped, nobody can run a migration, unlock a
locked-out household, reset a password or prune retention data in production.
This must be closed before deploying; see `docs/HANDOVER.md` §5.

> **Closed 2026-08-11.** `api/Dockerfile` gained a third target, `admin`, on the
> same distroless base as `prod`, carrying `/app/goose`, `/app/adminctl` and
> `/app/migrations`. The `api` image is unchanged and still has no shell — the
> tools live in a *second* image rather than being added to the first, so the
> serving surface grew by nothing. `deploy/docker-compose.prod.yml` wires it
> twice: as the one-shot `migrate` service that `api` waits on with
> `service_completed_successfully`, and as a `profiles: [manual]` `admin`
> service, never started by `up`, for `unlock-household`, `reset-password`,
> `create-invite` and `prune`. Every command is written out in
> `deploy/README.md`. The paragraph above stands as the record of why this was
> treated as a prerequisite rather than a follow-up; it is no longer a
> description of the current state.

## Alternatives considered

- **AWS Lightsail** — the same VPS shape for more money. Reasonable if an AWS
  invoice is worth something later; it is not today.
- **AWS EC2 + RDS + ALB** — the load balancer alone costs more than the whole
  VPS, and buys HA a single-household app does not need.
- **GCP** — the always-free `e2-micro` is US-regions-only and 1 GB; Cloud SQL's
  cheapest instance is roughly the whole budget in compute alone.
- **Oracle Always Free** — genuinely free, and reclaims instances idle over a
  7-day window (95th-percentile CPU under 20%, network under 10%, memory under
  10%). A household dashboard meets all three most weeks. The free allowance was
  also halved on 15 June 2026 with no announcement, which is itself the signal.
- **Supabase / Vercel** — rejected on architecture, not price. See ADR 1.

## Supersede this when

Traffic outgrows one box, the region changes, or Hetzner stops being the right
value. Write the replacement as ADR 3 rather than editing this one — the record
of *why* a host was picked is useful even after it is wrong.

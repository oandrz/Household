# 2. First production host — one small VPS in the EU

**Status:** Accepted — 2026-08-10. **Region and machine amended 2026-08-15,
before the purchase was ever made** — see "Amendment" under Decision. The
original read "one small VPS in Singapore"; the machine it named cannot be
bought and the region it chose costs more than the AWS bill this ADR exists to
escape.

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
| Compute | ~~**Hetzner Cloud CPX11, Singapore region**~~ → **Hetzner Cloud CX23, Falkenstein** (amended 2026-08-15) | Cheapest credible VPS that runs the whole stack; a household's Postgres is a few MB for years. The region and machine both changed at purchase — see the amendment below |
| TLS | **Caddy in front, as the edge** | Automatic Let's Encrypt issuance *and renewal*, forever, with no certbot cron to rot. `web/nginx.conf` stays exactly as it is |
| Database | **Postgres in the Compose stack, on the same box** | ADR 1 — plain Postgres we own. Not a managed service, and explicitly not a second provider |
| Backups | **Nightly `pg_dump` in plain SQL to Backblaze B2 or Cloudflare R2**, plus Hetzner snapshots | The off-provider dump is the real backup; snapshots are for fast recovery. Free at this data size |
| Mail | ~~**Resend**, free plan~~ **— SUPERSEDED by [ADR 3](0003-mail-stays-on-the-box.md), 2026-08-12.** Mail runs on Mailpit inside the stack and is read by hand | Resend needs a domain whose DNS accepts `TXT` records for DKIM, and this install's free DDNS hostname does not. The configuration surface below was the reason for choosing it and is still the reason the switch back is cheap: `SMTP_ADDR` / `SMTP_USERNAME` / `SMTP_PASSWORD` with no code change, in either direction |
| Domain | An at-cost registrar (Cloudflare Registrar, or Porkbun), registered long, auto-renew on | See "the domain is the fragile part" below |

Roughly **S$10/month** all in, against S$20 today. Prices moved twice in 2026
(Hetzner raised in April and again on 15 June) — **confirm the current figure at
purchase rather than trusting this table.** That instruction is the only reason
the amendment below happened before the money was spent rather than after.

### Amendment, 2026-08-15 — the region moved to the EU

Two facts found at the order form, neither of them true when this ADR was
written on 2026-08-10:

**`CPX11` no longer exists.** Hetzner standardised its lines on 15 June 2026 and
renamed the shared-vCPU tiers. Its nearest successor, `CPX12`, is a *smaller*
machine — 1 vCPU rather than 2 — so the rename moved specs, not just labels.

**The cheap line is not sold in Singapore.** Singapore offers only `CPX`. The
`CX` line, which is what makes Hetzner cheap, exists in the EU locations alone.
This is an availability fact, not a preference that could be argued with.

Priced at the console, both currencies as shown there:

| | Singapore `CPX12` | Falkenstein `CX23` |
|---|---|---|
| vCPU | 1 | **2** |
| RAM | 2 GB | **4 GB** |
| Disk | 40 GB | 40 GB |
| Traffic | 0.5 TB | **20 TB** |
| Price | $19.61/mo | **$7.07/mo** |

Singapore was **twice the price of the AWS bill this ADR exists to escape**, for
half the machine. The Context above lists cost as a hard constraint and region
as a preference — "South-East Asia *beats* Europe or the US for latency". Beats,
not requires. The hard constraint is the one that was failing, so the preference
gave way.

**Falkenstein, not Helsinki — measured, not assumed.** Hetzner's three EU
locations price the `CX` line identically, so latency to the household is the
only thing separating them. It was measured from the owner's actual network in
Singapore against Hetzner's per-location speed-test hosts, twice, at 8 and 20
packets:

| Location | min | avg | stddev | max |
|---|---|---|---|---|
| `fsn1` Falkenstein | 193.8 ms | **195.5 / 196.3 ms** | **2.1 / 1.7** | 199.7 ms |
| `nbg1` Nuremberg | 191.9 ms | 195.0 / 197.8 ms | 1.8 / 9.8 | 239.8 ms |
| `hel1` Helsinki | 215.6 ms | 246.0 / 239.6 ms | 28.3 / 29.1 | 307.3 ms |

Helsinki is **~22 ms worse at the floor and ~45 ms worse on average, with
roughly fifteen times the jitter** — and both runs agree, so this is routing,
not a transient. The minimum is the honest number here because it is the figure
least polluted by momentary congestion, and Helsinki's floor is genuinely
further away.

Falkenstein and Nuremberg are within 1–2 ms of each other, which is noise.
Falkenstein takes it on stability — the tightest spread in both runs — and
because it is Hetzner's largest and oldest site, so capacity and feature
availability are best there over a long horizon. Nuremberg would be an equally
defensible pick; Helsinki would not.

Reproduce this before assuming it still holds — routes change:

```bash
for h in fsn1-speed.hetzner.com nbg1-speed.hetzner.com hel1-speed.hetzner.com; do
  printf "%-28s " "$h"; ping -c 20 -q "$h" | tail -1
done
```

**`CX23`, not `CX33`.** 4 GB is roughly four times what this stack idles at
(Postgres, a static Go binary, nginx, Caddy, Mailpit). Hetzner's June 2026 terms
grandfather an existing server's price **unless it is rescaled**, so sizing is
worth getting right once — but that cuts both ways: buying headroom now costs
the same as buying it later, and paying for four years of unused RAM to avoid
one resize is not a saving.

## Consequences

**Every request from the household now crosses ~195 ms of ocean, accepted.**
That figure is measured from the owner's network, not estimated — see the table
in the amendment. This is the cost of moving to the EU and it is not free. It is
tolerable here for reasons specific to this product: the SPA is static files
nginx serves once and the browser caches, so only JSON calls pay the round trip;
there are two users; and nothing in Hearth is latency-sensitive the way a game
or a trading screen is.

**If this ever feels slow in real use, that is data, not noise** — record it. It
is the one thing this amendment traded away, and ADR 1's whole point is that
moving back is a new box, a restored dump and a DNS change. Two shapes to reach
for before moving, in that order, because both are cheaper than a migration: put
a CDN in front of the static bundle so only the API crosses the ocean, and batch
or optimistically render the writes that feel worst. Neither has been needed
yet, and neither should be built speculatively.

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

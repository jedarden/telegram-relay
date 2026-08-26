# telegram-relay — plan

## Problem

Several independent workloads (`cnpg-backup-watchdog`, `leaderboard-staleness-monitor`,
and now `hetzner-auction-dashboard`'s filter alerting) each want to send Telegram
alerts, and the established pattern is for each one to hold its own copy of a
Telegram bot token in its own OpenBao path plus its own `ExternalSecret`. That
works but duplicates the credential per consumer and per cluster. `telegram-relay`
gives any workload a single internal endpoint to `POST` a message to instead.

## Scope

One small stateless HTTP service. It does not do templating, rate limiting,
message history, or multi-bot routing — those are all deliberately out of
scope until a real need appears. It forwards one message to one Telegram bot
via one HTTP call.

## Architecture

```
caller (any pod on the tailnet)
  |
  | POST /send {chat_id?, text, parse_mode?}
  v
telegram-relay (Deployment, ardenone-cluster, namespace "telegram-relay")
  |
  | injects TELEGRAM_BOT_TOKEN (OpenBao -> ExternalSecret -> env var)
  v
api.telegram.org/bot<token>/sendMessage
```

- Single replica, stateless. A crash loses nothing — no in-flight state to
  recover.
- Deployed on `ardenone-cluster`, not `iad-ci`: `iad-ci`'s own CLAUDE.md says
  to keep long-lived services off the CI/build cluster, and
  `hetzner-auction-dashboard`'s pipeline (the first real caller) turned out to
  already live on `ardenone-cluster` rather than `iad-ci`. `ardenone-cluster`
  has its own local, independently-writable OpenBao instance.
- CI still builds on `iad-ci` — that's the org-wide CI policy regardless of
  where a service is deployed.
- Exposed on the tailnet through `ardenone-cluster`'s existing Traefik `vpn`
  entrypoint (not a new `tailscale.com/expose` — see declarative-config's
  standing "one Tailscale-exposed Service per cluster" rule), so any cluster
  or box on the tailnet can reach it, not just pods on `ardenone-cluster`.
- No public (`websecure`) exposure. This service can send messages to a
  Telegram chat; there is no reason for it to be internet-reachable.

## Security model

- The bot token never leaves this pod. It arrives via OpenBao -> ExternalSecret
  -> env var, the same mechanism every other credential in the fleet uses, and
  is never logged or echoed back to a caller.
- Network isolation (Tailscale-only reachability) is the primary access
  control, matching how `devpod-observer` proxies and other internal-only
  services in this fleet are already treated.
- An optional `RELAY_AUTH_TOKEN` shared secret adds defense-in-depth for
  `/send` without requiring per-caller credentials or a real auth system —
  proportionate to a single-operator, tailnet-only service.

## Data model

No persistent state. Request/response shape is documented in `README.md`.

## Deployment

- `containers/` convention isn't used — this repo *is* the container (single
  `Dockerfile` at root), matching `b2-usage-exporter`'s shape, not the
  multi-container `containers/<name>/` layout used by monorepo-style repos.
- CI: `telegram-relay-build` WorkflowTemplate in `declarative-config`
  (`k8s/iad-ci/argo-workflows/`), modeled on `b2-usage-exporter-build` —
  resolve-version (reads/auto-bumps `VERSION`) then a kaniko build pushing
  `ronaldraygun/telegram-relay:<semver>`. No auto-trigger sensor for v1;
  manual `kubectl create -f` submission is the accepted fallback, same as
  `hetzner-auction-dashboard`'s Phase 6 criteria.
- Manifests: `k8s/ardenone-cluster/telegram-relay/` (namespace, deployment,
  service, ExternalSecret + template, vpn-entrypoint IngressRoute — following
  the `lab-health` pattern of a Certificate + IngressRoute living alongside
  the app's own namespace rather than centralized in `traefik/`).
- Secrets, both on ardenone-cluster's own OpenBao instance:
  - `secret/ardenone-cluster/telegram/ardenone_bot` (field `token`) — reused
    from the retired `telegram-bridge` project rather than provisioning a new
    bot; its k8s wiring was decommissioned 2026-08-24 but the OpenBao value
    was left in place.
  - `secret/ardenone-cluster/telegram-relay/config` (field
    `default_chat_id`) — telegram-relay-specific, since the old bridge never
    needed a default.
  - `RELAY_AUTH_TOKEN` is not wired into an ExternalSecret at all for v1 — it's
    a purely optional env var an operator can set later if the network-isolation
    boundary alone turns out not to be enough.

## Open questions

None — every fork above is decided. If a second bot or multi-tenant routing
is ever needed, that's a new decision to make then, not a speculative feature
now.

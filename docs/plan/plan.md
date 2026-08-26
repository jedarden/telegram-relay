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
telegram-relay (Deployment, iad-ci, namespace "telegram-relay")
  |
  | injects TELEGRAM_BOT_TOKEN (OpenBao -> ExternalSecret -> env var)
  v
api.telegram.org/bot<token>/sendMessage
```

- Single replica, stateless. A crash loses nothing — no in-flight state to
  recover.
- Deployed on `iad-ci`: it's where the first real caller
  (`hetzner-auction-dashboard`'s pipeline) already runs, and `iad-ci` already
  has a live `openbao` `ClusterSecretStore` reading `rs-manager`'s OpenBao.
- Exposed on the tailnet through `iad-ci`'s existing Traefik `vpn` entrypoint
  (not a new `tailscale.com/expose` — see declarative-config's standing "one
  Tailscale-exposed Service per cluster" rule), so any cluster or box on the
  tailnet can reach it, not just pods inside `iad-ci`.
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
- Manifests: `k8s/iad-ci/telegram-relay/` (namespace, deployment, service,
  ExternalSecret + template) and `k8s/iad-ci/traefik/telegram-relay-ingressroute.yml`
  (Certificate + vpn-entrypoint IngressRoute, following the `victorialogs`
  pattern of living in the `traefik` namespace so it deploys with the
  always-reconciled `traefik-ns-iad-ci` app).
- Secret: `secret/rs-manager/iad-ci/telegram-relay` in OpenBao (fields
  `telegram-bot-token`, `telegram-chat-id`, optional `relay-auth-token`) —
  owned by rs-manager's OpenBao per "write to the OpenBao that owns the path".

## Open questions

None — every fork above is decided. If a second bot or multi-tenant routing
is ever needed, that's a new decision to make then, not a speculative feature
now.

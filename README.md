# telegram-relay

A small internal HTTP endpoint that forwards `{chat_id, text}` requests to the
Telegram Bot API. The bot token lives only as an environment variable inside
this service's own pod (injected from OpenBao); callers never need their own
copy of it. Any workload that wants to send a Telegram alert can `POST` here
instead of holding a bot token itself.

## API

`GET /healthz` — liveness/readiness check, always `200 ok`.

`POST /send`

```json
{
  "chat_id": "123456789",
  "text": "message body",
  "parse_mode": "Markdown"
}
```

- `text` is required.
- `chat_id` is optional if `TELEGRAM_DEFAULT_CHAT_ID` is configured on the
  server; otherwise required.
- `parse_mode` is optional and passed through to Telegram unmodified.
- The response is Telegram's own `sendMessage` response, status and body,
  passed straight through — errors from Telegram (bad chat id, rate limit,
  etc.) surface directly to the caller.

If `RELAY_AUTH_TOKEN` is configured, `POST /send` requires
`Authorization: Bearer <token>`. Network isolation (this service is reachable
only over Tailscale, never the public internet) is the primary boundary; the
token is a defense-in-depth option, not a requirement.

## Configuration

| Env var | Required | Purpose |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | yes | Bot token from BotFather |
| `TELEGRAM_DEFAULT_CHAT_ID` | no | Used when a request omits `chat_id` |
| `RELAY_AUTH_TOKEN` | no | If set, `/send` requires a matching bearer token |
| `PORT` | no | Listen port, default `8080` |

## Structure

- `docs/notes/` — features, constraints, design decisions
- `docs/research/` — external reference material and prior art
- `docs/plan/plan.md` — complete application plan

---

*This GitHub repo is a read-only mirror of git.ardenone.com/jedarden/telegram-relay — issues and PRs are welcome here either way.*

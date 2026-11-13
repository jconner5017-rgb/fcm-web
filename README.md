# FireCardManager Web

The web companion to the FireCardManager desktop apps (Sports and TCG):
accounts, linking a PC, and two-way cloud sync of a seller's inventory.
Spec: "FCM Web — Phase 1 Spec: Cloud Sync + Web Inventory".

- `cmd/api`, `internal/...`: the Go API (Postgres).
- `web/`: the Next.js website (app.firecardmanager.com). The browser talks to
  the API through `/api/...` on the same site.
- `render.yaml`: Render Blueprint for staging.

## API overview

| Endpoint | Who | What |
| --- | --- | --- |
| `POST /v1/auth/signup`, `/login`, `/mfa`, `/logout` | website | accounts, with optional authenticator codes |
| `POST /v1/auth/forgot-password`, `/reset-password`, `/verify-email` | website | email links |
| `GET /v1/me`, `POST /v1/me/password`, `/v1/me/mfa/*` | website | the signed-in account |
| `POST /v1/devices/link/start` | desktop | license-checked link request: returns a code to show |
| `GET /v1/devices/link/{code}`, `POST /v1/devices/link/approve` | website | the seller approves that code |
| `POST /v1/devices/link/finish` | desktop | polled until approved; returns the device token once |
| `GET /v1/devices`, `DELETE /v1/devices/{id}` | website | linked PCs |
| `GET /v1/sync/status`, `POST /v1/sync/push`, `GET /v1/sync/pull?after=` | desktop (Bearer device token) | sync |
| `GET /v1/admin/accounts`, `POST /v1/admin/accounts/{id}/addon`, `/trial` | owner (admin + 2FA) | web add-on and trials |

Sync rows are `box`, `card` and `sold`. Field names are lower_snake_case.
The server reads `status` (`NotForSale`, `Ready`, `EBA`, `EBF`, `Sold`) and
`box_id` (the box's sync id) on cards; everything else is stored as sent.

## Tests

    TEST_DATABASE_URL="postgres://user@/fcmweb_test?host=/tmp&port=5432&sslmode=disable" go test ./... -p 1

The tests wipe every table: never point them at a real database.

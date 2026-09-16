# Mail Checker Go Server

The server owns the Zoho OAuth client secret and refresh tokens. It issues a signed JWT to the extension and uses encrypted refresh tokens only on the server to request Zoho Mail data.

## Architecture

The code follows a layered architecture with dependencies pointing inward:

- `cmd/server`: composition root and HTTP server startup.
- `internal/transport`: HTTP request validation, authentication middleware, and JSON responses.
- `internal/application`: OAuth and mail use cases.
- `internal/domain`: entities and ports implemented without HTTP or storage details.
- `internal/infrastructure`: Zoho HTTP client, encrypted file repository, and JWT implementation.

The default file repository is appropriate for one self-hosted instance. Replace `domain.CredentialRepository` with a database-backed implementation before running multiple instances.

## Real-time delivery

The extension keeps an authenticated WebSocket connection to `GET /events`. When the server receives a valid mail webhook, it pushes `{"type":"mail.received"}` to the matching extension, which immediately refreshes its unread count and preview.

Configure your Zoho webhook integration to call:

`POST https://your-server.example/webhooks/zoho/{accountID}`

The `{accountID}` value is saved during OAuth and displayed in the extension beneath the account email. Zoho sends `X-Hook-Secret` only with the first webhook request. The server saves this secret, encrypted, in `TOKEN_STORE_PATH` and validates each `X-Hook-Signature` as a base64 HMAC-SHA256 digest of the raw request body. The endpoint returns `204 No Content` for a known account, `401` for an invalid signature, and `404` for an unknown account.

The webhook endpoint must be publicly reachable through HTTPS. This server does not poll mail automatically; the existing manual refresh remains available in the extension.

To confirm delivery, inspect the server logs after a new email: a successful request logs `Zoho webhook accepted for account ...`. The first successful request also logs `Zoho webhook secret initialized`. A `401` means that `X-Hook-Signature` did not validate; a `404` means the URL's account ID is not the account saved during OAuth.

## Run locally

1. Create a Zoho OAuth client and register `http://localhost:8080/auth/zoho/callback` as its redirect URI.
2. Copy `.env.example` to `.env`, set the values, then load them into your shell.
3. Run `go test ./...` and `go run .` from this directory.

In the extension, open Settings and enter your server URL. Chrome asks for permission to contact the selected origin; Firefox requests this permission when installed. Changing the server clears the old local JWT and account cache, then you can connect the account again.

For local development, use `http://localhost:8080`. For production, serve this application behind HTTPS and configure the production callback URL in both Zoho and `ZOHO_REDIRECT_URL`.

`TOKEN_ENCRYPTION_KEY` must be 32 ASCII bytes. Generate a suitable value with a password manager or `openssl rand -base64 24`; preserve it across restarts, or stored refresh tokens and the Zoho webhook secret cannot be read.
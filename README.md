# shareserver

A small, terminal-style file share web app in Go. Server-rendered pages, no
SPA, no CDN. Every client zips and encrypts its payload before upload. The
server stores opaque encrypted payloads plus metadata through Ent on SQLite.

Detailed protocol examples, architecture, and agent instructions live in the
[LLM_WIKI reference](./LLM_WIKI.md).

## What it does

- Upload one or more files → get a short link (`/s/{uuid}`).
- Shares are **public** (listed on the home page) or **private** (unlisted;
  findable only with a private key, though the direct UUID link always works).
- Every Share is **encrypted in the client** before upload. The password stays
  client-side. The server accepts only its canonical Base64 SHA-256 value and
  stores a separate salted bcrypt verifier rather than that value or the key.
- Shares expire (default 6h, max 24h for anonymous uploads).
- Admin panel at `/admin` for inspecting/deleting shares and seeing storage
  usage + uploader IP.

## Hard constraints

These are baked in and not negotiable without changing what this is:

- **Local resources only.** All JS/CSS/fonts are served from `/static`. No
  CDN, no external URLs in rendered output.
- **Server never decrypts.** Shares stay opaque and encrypted at rest;
  decryption happens only in the browser.
- **Payload access requires the password hash.** The server compares each
  canonical SHA-256 value against its separate verifier before returning bytes.
- **Every download is a POST.** Responses have no artificial delay.
  More than 10 failed password requests from one IP in one minute create a
  persistent 24-hour ban with random `-3600..18000` second jitter.
- **Every upload is zip-backed**, even a single file.
- **CSRF on browser-session mutations.** Stateless API calls cannot gain admin
  policy without a matching session-bound token.
- **Private ≠ encrypted.** Private means unlisted; the private key only
  discovers/lists private shares, it is not the encryption password.
- **Anonymous errors are generic.** Cap/disk failures never leak internal
  state to anonymous users.

## Quick start

### Run with Docker

Use a configured `.env` file based on [`.env.example`](./.env.example), with
`APP_SECRET` and `ADMIN_PASSWORD` set for production.

```sh
docker run --detach \
  --name shareserver \
  --restart unless-stopped \
  --env-file .env \
  --env DEBUG=false \
  --env ADDR=0.0.0.0:8080 \
  --env DB_PATH=/app/data/shareserver.db \
  --env BLOB_DIR=/app/data/blobs \
  --publish 8080:8080 \
  --mount type=volume,source=shareserver-data,target=/app/data \
  --entrypoint /app/shareserver \
  ghcr.io/parksnoopy/shareserver:latest
```

The named volume persists the SQLite database and uploaded blobs. The entrypoint
override runs the binary directly instead of regenerating `APP_SECRET` at each
startup; keep the configured secret stable across restarts. Place a trusted HTTPS
reverse proxy in front of the HTTP listener for API operations.

### Run Docker Compose with self-signed HTTPS

The [Compose deployment](./deploy/docker-compose.yaml) uses the same production
`.env` configuration as above. Run the commands below from the repository root.
It publishes only HTTPS on port 8443; the
[Caddy proxy](./deploy/Caddyfile) shares the app's network namespace so the HTTP
listener remains on loopback and forwarded HTTPS/client-IP headers are trusted.
Docker Compose 2.17+, Bash, OpenSSL, and curl are required.

Run the [interactive deployment script](./deploy/deploy.sh) to choose the
certificate domain/IP, published HTTPS port (default 8443), and certificate
lifetime (default 365 days):

```sh
bash deploy/deploy.sh
```

The script requires the configured application `.env`, asks before applying
settings or replacing an existing certificate, and saves only public deployment
settings in the ignored `deploy/.env`. It preserves application credentials and
stored data, recreates the Compose containers to apply port/certificate changes,
and verifies HTTPS using the selected certificate before reporting success.
To prepare certificates/settings without Docker, use `--prepare-only`.
Subsequent manual Compose commands should load both environment files:

```sh
docker compose --env-file .env --env-file deploy/.env \
  -f deploy/docker-compose.yaml up -d
```

Alternatively, prepare everything manually. Generate a self-signed P-521
certificate for the hostname clients will use
(`localhost` below; replace it with your deployment hostname):

```sh
umask 077
mkdir -p data/tls
TLS_HOST=localhost
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:secp521r1 \
  -sha512 -nodes -days 365 \
  -subj "/CN=${TLS_HOST}" \
  -addext "subjectAltName=DNS:${TLS_HOST},IP:127.0.0.1,IP:::1" \
  -addext "basicConstraints=critical,CA:FALSE" \
  -addext "keyUsage=critical,digitalSignature" \
  -addext "extendedKeyUsage=serverAuth" \
  -keyout data/tls/server.key \
  -out data/tls/server.crt

export HTTPS_UID="$(id -u)" HTTPS_GID="$(id -g)"
docker compose --env-file .env -f deploy/docker-compose.yaml up -d
curl -fsSL --cacert data/tls/server.crt "https://${TLS_HOST}:${HTTPS_PORT:-8443}/api/"
```

Use `IP:<address>` in the certificate's Subject Alternative Name if clients
connect by a non-loopback IP address. Keep the private key private; the proxy
runs with the certificate owner's UID/GID and mounts certificates read-only.
Export `HTTPS_UID` and `HTTPS_GID` as shown for every Compose invocation, or
persist their values in `.env`.
Trust the certificate explicitly on each client: a self-signed certificate
encrypts traffic but is not automatically trusted by browsers. Do not disable
certificate verification as a deployment solution.

Set `HTTPS_PORT` to change the published port. Database and blobs persist in a
named volume, and certificates persist under the ignored `data/tls` directory.
For script-managed deployments, rerun `bash deploy/deploy.sh` and confirm
certificate replacement before expiry; saved port/hostname defaults are retained.
For manual deployments, regenerate the certificate and run
`docker compose --env-file .env -f deploy/docker-compose.yaml restart https`;
distribute the new certificate to clients and
preserve `APP_SECRET` and the data volume.

### Run Go binary directly

Needs the Go toolchain declared in [the module](./go.mod) and a C toolchain
for cgo (`go-sqlite3`). Bun is needed for the client test suite, not server runtime.

```sh
# 1. build
go build -o shareserver ./cmd/shareserver

# 2. configure (copy and edit)
cp .env.example .env   # then set APP_SECRET, ADMIN_PASSWORD, etc.
mkdir -p data/blobs    # directory for DB_PATH and BLOB_DIR

# 3. run
./shareserver
# -> listening on :8080 (or ADDR from .env)
```

Show installed version without starting the server:

```sh
./shareserver --version
```

For local play you can skip `.env`: with `DEBUG=1` an ephemeral `APP_SECRET`
is generated and a default admin is created. Do **not** run `DEBUG=1` in
production — it logs a warning and uses a throwaway secret.

## Configuration

The [configuration loader](./internal/config/config.go) reads environment
variables and an optional local `.env` file; real environment variables win.
The table below corresponds to the [environment example](./.env.example):

| Var | Example/default | Purpose |
| --- | --- | --- |
| `DEBUG` | `true` | `1`/`true` allows an ephemeral secret + default admin; `0`/`false` requires `APP_SECRET` + `ADMIN_PASSWORD` |
| `ADDR` | `0.0.0.0:8080` | listen address |
| `DB_PATH` | `data/shareserver.db` | SQLite path |
| `BLOB_DIR` | `data/blobs` | where uploaded blobs are stored |
| `APP_SECRET` | `[REDACTED]` | HMAC key for private-key hashing; **required in prod** |
| `ADMIN_USER` | commented out | initial admin username |
| `ADMIN_PASSWORD` | commented out | initial admin password; accepted only from runtime configuration |
| `MAX_UPLOAD_BYTES` | `314572800` | per-blob upload limit |
| `STORAGE_CAP_BYTES` | `419430400` | global stored-blob cap |
| `TRUST_PROXY_HEADERS` | `true` | trust `X-Forwarded-For`/`X-Real-IP`/`X-Forwarded-Proto` from a verified local or Railway proxy |
| `TZ` | `Asia/Shanghai` | timezone for purge scheduling and display |

The browser derives the admin authorization value before login submission;
plaintext `ADMIN_PASSWORD` remains accepted only from runtime configuration.

## API

| Method | Route | Interface |
| --- | --- | --- |
| `GET` | `/api/` | HTML public archive list and concise API contract |
| `POST` | `/api/v0/upload` | Ordered multipart upload of a client-encrypted ZIP |
| `POST` | `/api/v0/list` | Body-only private archive discovery |
| `POST` | `/api/v0/download/{uuid}` | Password-hash-authorized ciphertext response |

The [canonical API reference](./LLM_WIKI.md#public-route-inventory) contains
transport and credential contracts, field/size limits, response schemas,
public/private curl recipes, local encryption/decryption, byte ranges,
failure handling, and retry policy. Keep protocol details there rather than
maintaining a second copy in this README.

## How to reproduce (tests)

Tests are split by runtime; no shell test runner is required.

```sh
go test ./...
bun test
```

This runs:

1. **`go test ./...`** — unit and route-level [server tests](./internal/test/) for
   Ent-backed metadata, uploads, sessions, expiry/404 pages, password-gated
   payload downloads, API rate limits, localization, and storage reconciliation.
2. **`bun test`** — [client-side tests](./web/test/) for Progress state,
   text normalization, encryption metadata bounds, and mobile-safe download
   filenames.

Storage integrity keeps the blob directory and database in sync: archive and
admin pages reconcile both sides before rendering, a missing blob removes its
database row, and a stored file with no database row is deleted from disk.
Admins can select multiple Shares and remove each selected pair in one action.

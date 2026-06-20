# shareserver

A small, terminal-style file share web app in Go. Server-rendered pages, no
SPA, no CDN. Files are zipped (and optionally encrypted) in the browser; the
server only ever stores opaque blobs. Designed to run on one box with SQLite.

## What it does

- Upload one or more files → get a short link (`/s/{uuid}`).
- Shares are **public** (listed on the home page) or **private** (unlisted;
  findable only with a private key, though the direct UUID link always works).
- Shares can be **encrypted**: the password stays in the browser, the server
  cannot decrypt the content.
- Shares expire (default 6h, max 24h for anonymous uploads).
- Admin panel at `/admin` for inspecting/deleting shares and seeing storage
  usage + uploader IP.

## Hard constraints

These are baked in and not negotiable without changing what this is:

- **Local resources only.** All JS/CSS/fonts are served from `/static`. No
  CDN, no external URLs in rendered output.
- **Server never decrypts.** Encrypted shares are opaque bytes server-side;
  decryption happens in the browser with a password the server never sees.
- **Every upload is zip-backed**, even a single file.
- **CSRF on every mutation.** POSTs require a session-bound token.
- **Private ≠ encrypted.** Private means unlisted; the private key only
  discovers/lists private shares, it is not the encryption password.
- **Anonymous errors are generic.** Cap/disk failures never leak internal
  state to anonymous users.

## Quick start

Needs Go 1.26+ (cgo, for `go-sqlite3`) and a C toolchain.

```sh
# 1. build
go build -o shareserver ./cmd/shareserver

# 2. configure (copy and edit)
cp .env.example .env   # then set APP_SECRET, ADMIN_PASSWORD, etc.

# 3. run
./shareserver
# -> listening on :8080 (or ADDR from .env)
```

For local play you can skip `.env`: with `ENV=dev` an ephemeral `APP_SECRET`
is generated and a default admin is created. Do **not** run `ENV=dev` in
production — it logs a warning and uses a throwaway secret.

## Configuration

All config comes from environment variables (a `.env` file is loaded if
present; real env vars win over the file). `README.md` below matches
`.env.example`:

| Var | Example/default | Purpose |
| --- | --- | --- |
| `ENV` | `dev` | `dev` allows an ephemeral secret + default admin; `prod` requires `APP_SECRET` + `ADMIN_PASSWORD` |
| `ADDR` | `0.0.0.0:8080` | listen address |
| `DB_PATH` | `data/shareserver.db` | SQLite path |
| `BLOB_DIR` | `data/blobs` | where uploaded blobs are stored |
| `APP_SECRET` | `!INSECURE!_qweruiop12347890` | HMAC key for private-key hashing; **required in prod** |
| `ADMIN_USER` | commented out | initial admin username |
| `ADMIN_PASSWORD` | commented out | initial admin password |
| `MAX_UPLOAD_BYTES` | `314572800` | per-blob upload limit |
| `STORAGE_CAP_BYTES` | `419430400` | global stored-blob cap |
| `TRUST_PROXY_HEADERS` | `false` | trust `X-Forwarded-For`/`X-Real-IP`/`X-Forwarded-Proto` |
| `TZ` | `Asia/Shanghai` | timezone for purge scheduling and display |

## How to reproduce (tests)

Three layers, all run by one script:

```sh
bash test/run.sh
```

This runs:

1. **`go test ./...`** — unit tests for the share store, upload module,
   session cleanup, and blob expiry gating.
2. **`node test/progress.mjs`** — client-side Progress state machine (reset on
   retry, no stale failed/working lines).
3. **`bash test/smoke.sh`** — a real curl integration suite that builds the
   binary, starts an isolated server on a throwaway port/DB, and checks:
   - routes render, admin gate redirects, bad UUIDs 404
   - CSRF rejection
   - public + private (with/without key) upload
   - admin login → session id rotates (fixation fix), old sid loses admin
   - Secure cookie flag under `X-Forwarded-Proto: https`
   - admin delete removes blob + row
   - **real-time expiry**: upload → exists → wait 10s → blob gone (410) and
     share removed from the public list

The smoke suite cleans up its own temp DB, blobs, and server process. It does
not touch your dev data.

### Running just one layer

```sh
go test ./...
node test/progress.mjs
bash test/smoke.sh
```

### The expiry timing test

The upload API only accepts `expiry_hours` (min 1h), so the suite can't set a
5-second expiry over curl. Instead it uploads normally, then uses a tiny
helper to shorten the share's `expires_at` to `now+5s` directly in the test
DB, and watches the active → expired transition over real HTTP:

```sh
go run ./test/setexpiry <db> <id> <seconds>
```

You don't normally need to run this by hand — `smoke.sh` does it for you.

For the *why* behind the code (deep modules, locality, security as a property of the flow), read `plan.md`.

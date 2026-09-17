# shareserver

A small, terminal-style file share web app in Go. Server-rendered pages, no
SPA, no CDN. Every client zips and encrypts its payload before upload. The
server stores opaque encrypted payloads plus metadata through Ent on SQLite.

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
- **Every download is a POST.** Payload responses wait at least 2 seconds.
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

Needs Go 1.26+ (cgo, for `go-sqlite3`), Bun, and a C toolchain.

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

All config comes from environment variables (a `.env` file is loaded if
present; real env vars win over the file). `README.md` below matches
`.env.example`:

| Var | Example/default | Purpose |
| --- | --- | --- |
| `DEBUG` | `true` | `1`/`true` allows an ephemeral secret + default admin; `0`/`false` requires `APP_SECRET` + `ADMIN_PASSWORD` |
| `ADDR` | `0.0.0.0:8080` | listen address |
| `DB_PATH` | `data/shareserver.db` | SQLite path |
| `BLOB_DIR` | `data/blobs` | where uploaded blobs are stored |
| `APP_SECRET` | `!INSECURE!_qweruiop12347890` | HMAC key for private-key hashing; **required in prod** |
| `ADMIN_USER` | commented out | initial admin username |
| `ADMIN_PASSWORD` | commented out | initial admin password; accepted only from runtime configuration |
| `MAX_UPLOAD_BYTES` | `314572800` | per-blob upload limit |
| `STORAGE_CAP_BYTES` | `419430400` | global stored-blob cap |
| `TRUST_PROXY_HEADERS` | `true` | trust `X-Forwarded-For`/`X-Real-IP`/`X-Forwarded-Proto` from a verified local or Railway proxy |
| `TZ` | `Asia/Shanghai` | timezone for purge scheduling and display |

The browser derives the admin authorization value before login submission;
plaintext `ADMIN_PASSWORD` remains accepted only from runtime configuration.

## API

`GET /api/` renders the active public archive list and usage contract. Crawlers
may index `/api/*`; `robots.txt` disallows every other path.

API operation calls need no browser session and require HTTPS. API upload accepts
only client-encrypted payloads as `multipart/form-data` at `POST /api/v0/upload`:

The server accepts direct TLS or `X-Forwarded-Proto: https` only from a trusted
loopback proxy, or Railway's internal proxy network when Railway runtime markers
are present, and only when `TRUST_PROXY_HEADERS=true`.

- `blob`: client-encrypted ZIP bytes;
- `password_hash`: browser/API authorization hash; Base64
  SHA-256 of `shareserver-download-password`, one `0x00` byte, then the
  NFC-normalized password;
- `encrypted`: must be `1`;
- `cipher_meta`: JSON describing
  `PBKDF2-SHA-384` and `AES-256-GCM` parameters;
- `title`, `visibility`, `private_key`, `expiry_hours`, and `zip_manifest`:
  same metadata used by the web upload form.

Send all metadata fields before `blob`, as shown below.

Successful upload returns HTTP `201` with `id`, Share `url`, `download_url`,
stored `size`, `expires_at`, `encryption` (`client`), and `cipher_meta`.

### Upload with curl

Create and encrypt the ZIP locally before sending it.

```sh
curl -fsSL \
  --request POST \
  --form 'title=<title>' \
  --form 'visibility=public/private' \
  --form-string 'password_hash=<base64-password-authorization-hash>' \
  --form 'encrypted=1' \
  --form-string 'cipher_meta=<cipher-metadata-json>' \
  --form 'expiry_hours=1/6/12/24' \
  --form 'blob=@<encrypted-zip>;type=application/octet-stream' \
  'https://<Server Domain>/api/v0/upload'
```

Choose one slash-delimited value for `visibility` and `expiry_hours`. For a
private Share, choose `private` and add
`--form-string 'private_key=<private-key>'`.

Use fresh random salt and nonce values for every encrypted upload. Plaintext
password fields and `encrypted=0` are rejected.

Upload responses:

- `201 Created`: payload and Share metadata stored; JSON response contains
  `id`, `url`, `download_url`, `size`, `expires_at`, `encryption`, and
  `cipher_meta`.
- `400 Bad Request`: malformed multipart data, missing payload, missing required
  password hash or private key, invalid encryption mode or metadata, or
  client-encrypted payload too short to contain an AES-GCM authentication tag.
- `403 Forbidden`: an `X-CSRF-Token` header was supplied but does not match the
  attached browser session. Command-line clients should omit this header.
- `413 Content Too Large`: uploaded source, resulting encrypted payload, or
  submitted metadata exceeds its configured limit.
- `415 Unsupported Media Type`: request is not `multipart/form-data`.
- `426 Upgrade Required`: request did not arrive through direct TLS or a trusted
  proxy reporting HTTPS.
- `500 Internal Server Error`: payload or metadata storage failed.
- `507 Insufficient Storage`: configured server storage capacity is exhausted.

`POST /api/v0/list` accepts exactly one `private_key` value in JSON or
`application/x-www-form-urlencoded` body data. Query-string keys, duplicate
values, unknown JSON fields, and other content types are rejected. A successful
request returns up to 100 active private Shares matching the key as
`{"archives":[...]}`. Each entry contains `id`, `title`, Share `url`,
`download_url`, stored `size`, `expires_at`, `created_at`, `cipher_meta`, and
`encryption`. A key with no matches returns `200 OK` with an empty list.

```sh
curl -fsSL \
  --request POST \
  --json '{"private_key":"<private-key>"}' \
  'https://<Server Domain>/api/v0/list'
```

Treat the private key as sensitive lookup material. The server uses it only to
derive its secret-scoped lookup hash and never returns or stores the raw value.

`POST /api/v0/download/{uuid}` accepts only `password_hash` in JSON or form
data. It is the Base64-encoded SHA-256 digest described above; plaintext archive
passwords are rejected. A correct credential returns the raw encrypted payload
as `application/octet-stream`; clients own decryption.
Wrong passwords return `401` without payload bytes. Every response waits at
least 2 seconds. The eleventh failed password request from one IP within a
rolling minute creates a persistent `24h + random(-3600s..18000s)` ban;
subsequent responses return `429` with `Retry-After`.

### Download with curl

Use the returned `download_url`, or place its `id` in `<uuid>`. The saved file
remains encrypted; decrypt it locally using its matching cipher metadata.

```sh
curl -fsSL \
  --request POST \
  --json '{"password_hash":"<base64-password-authorization-hash>"}' \
  --output '<filename>' \
  'https://<Server Domain>/api/v0/download/<uuid>'
```

Treat password hashes as reusable credentials; do not save them in scripts or
shell history.

Download responses:

- `200 OK`: password matched; response body is the encrypted payload.
- `206 Partial Content`: password matched and a valid `Range` header requested
  part of the encrypted payload.
- `401 Unauthorized`: password is missing, malformed, or incorrect; UUID is
  unknown; or Share lacks required download protection. No payload bytes are
  returned.
- `404 Not Found`: authorized payload disappeared before streaming began.
- `410 Gone`: password matched, but Share expired.
- `426 Upgrade Required`: request did not arrive through direct TLS or a trusted
  proxy reporting HTTPS.
- `429 Too Many Requests`: client IP is under a persistent failed-password ban;
  `Retry-After` reports remaining ban time in seconds.
- `416 Range Not Satisfiable`: requested byte range is outside payload bounds.

All API operation routes return `404 Not Found` for an unknown path and `405
Method Not Allowed` when called with a method other than `POST`.

On first startup after upgrading from versions without password-gated payload
downloads, legacy Shares lacking a download-password verifier are removed with
their blobs. They cannot be migrated safely because the server never stored
their passwords.

## How to reproduce (tests)

Tests are split by runtime; no shell test runner is required.

```sh
go test ./...
bun test
```

This runs:

1. **`go test ./...`** — unit and route-level tests under `internal/test/` for
   Ent-backed metadata, uploads, sessions, expiry/404 pages, password-gated
   payload downloads, API rate limits, localization, and storage reconciliation.
2. **`bun test`** — client-side tests under `web/test/` for Progress state,
   text normalization, encryption metadata bounds, and mobile-safe download
   filenames.

Storage integrity keeps the blob directory and database in sync: archive and
admin pages reconcile both sides before rendering, a missing blob removes its
database row, and a stored file with no database row is deleted from disk.
Admins can select multiple Shares and remove each selected pair in one action.

---

## Instruction For AI Agent

Read the [AI-agent API and project guide](./LLM_WIKI.md) before operating the
HTTP API or changing its request, encryption, storage, or response contracts.

- Security is the highest priority; if safety conflicts with speed, convenience, UI polish, or cleanup, choose safety and keep the tradeoff explicit.
- Admin auth must fail closed: unknown users, password-check errors, session rotation errors, CSRF failures, and ban checks must never create or preserve admin access.
- Files must stay secure on the network and at rest: preserve HTTPS/proxy trust boundaries, safe cookies, CSP, opaque encrypted blobs, sanitized filenames, and no internal UUID/blob names as user-facing download names.
- Keep the app small, boring, and easy to operate: one Go server, server-rendered pages, SQLite metadata, filesystem blobs, no SPA, no CDN, no unnecessary framework layer.
- Treat each share as one logical object made from two durable parts: metadata in SQLite and opaque bytes in the blob directory; every create, delete, purge, and repair path must keep both sides consistent.
- Keep browser and server responsibilities sharply separated: clients zip and encrypt uploads, decrypt previews/downloads, and preserve user-facing filenames; the server accepts only encrypted bytes and canonical SHA-256 password values, stores encrypted bytes and metadata, and owns verification, authorization, sessions, and audit records.
- Never make the server depend on plaintext encrypted-share contents; encrypted uploads must remain opaque server-side, and any metadata stored for them must be intentionally safe to reveal.
- Favor deep, cohesive modules over shallow plumbing: upload policy lives in upload code, share querying lives in the share store, auth rules live in auth/session code, and storage repair lives with cleanup.
- Keep HTTP handlers thin and boring: parse request, call the owning module, choose response status or redirect, and avoid embedding storage, auth, or database policy in route code.
- Use Ent for first-party database access; do not reintroduce ad-hoc raw SQL query strings outside generated or migration-owned code.
- Keep SQLite restricted and safe over fast: simple schema, explicit constraints, foreign keys, conservative connection behavior, deterministic migrations, and no hidden external service dependency.
- Treat security as an end-to-end flow property, not a middleware checkbox: CSRF, cookies, admin sessions, private keys, IP bans, proxy trust, content security policy, encrypted-share handling, storage policy, and database access must agree.
- Fail closed on ambiguous access: missing shares, purged shares, expired shares, wrong private keys, bad sessions, oversized metadata, and storage-cap pressure should not leak more than needed.
- Keep public and private share behavior distinct: public shares may appear in listings, private shares require their key path, and direct UUID links should not weaken private-key checks.
- Preserve expiry semantics consistently across list, detail, blob download, cleanup, admin views, and tests; an expired share should not remain reachable through a forgotten path.
- Keep blob cleanup idempotent and safe: missing files delete stale rows, orphan files are removed, and failed filesystem operations should not pretend metadata was cleaned.
- Prefer clean cutovers over compatibility shims: migrate every caller, remove obsolete helpers, and leave no alias path unless a real user-facing compatibility need exists.
- Keep UI source readable and terminal-styled: templates stay legible, CSS classes carry shared sizing/layout meaning, and JavaScript modules stay small enough to inspect without build machinery.
- Treat mobile behavior as first-class, especially Android download behavior, sidebar state, touch navigation, and visible filename preservation.
- Keep client downloads user-centered: preserve uploaded basenames where possible, sanitize only dangerous filename characters, and avoid exposing internal UUID/blob names as the normal download name.
- Do not add mocks for core flows; prefer route-level, store-level, upload-level, cleanup-level, and browser-helper tests that exercise real behavior and real failure branches.
- Keep tests split by runtime and purpose: Go tests cover server policy, metadata, sessions, expiry, cleanup, and upload behavior; Bun tests cover client-only text, crypto metadata, progress, and download helpers.
- Prefer explicit environment configuration with safe defaults; secrets belong in runtime config, examples must not contain real secrets, and missing or invalid required settings should be obvious.
- Keep time handling deliberate: store and compare expiry values consistently, use UTC for durable timestamps, and use configured timezone only for display or scheduled maintenance boundaries.
- Keep admin features operational rather than ornamental: admin pages should expose enough state to inspect, delete, and understand storage without creating new unsafe mutation paths.
- Preserve audit usefulness without overlogging sensitive data: record who, where, action, target, and safe metadata; never log passwords, private keys, plaintext encrypted contents, or browser-only secrets.
- Never remove comments; when code changes, update inaccurate comments so they remain true instead of deleting human context.
- Write comments for human maintainers first and future AI agents second: concise but complete, covering what each function/struct is for and any non-obvious invariant.
- Prefer deletion of commented-out dead code over preserving it; never remove explanatory comments to make live code look shorter.
- Optimize for the next maintainer reading the repository cold: local names should reflect domain concepts, branches should map to user-visible behavior, and every module boundary should reduce what a caller must know.

# shareserver

A small, terminal-style file share web app in Go. Server-rendered pages, no
SPA, no CDN. Files are zipped and encrypted in the browser; the server stores
opaque payloads plus metadata through Ent on SQLite.

## What it does

- Upload one or more files → get a short link (`/s/{uuid}`).
- Shares are **public** (listed on the home page) or **private** (unlisted;
  findable only with a private key, though the direct UUID link always works).
- Every Share is **encrypted**. The password encrypts and decrypts in the
  browser. The server receives it over HTTPS only to authorize payload access,
  storing a separate salted bcrypt verifier rather than the password or
  browser encryption key.
- Shares expire (default 6h, max 24h for anonymous uploads).
- Admin panel at `/admin` for inspecting/deleting shares and seeing storage
  usage + uploader IP.

## Hard constraints

These are baked in and not negotiable without changing what this is:

- **Local resources only.** All JS/CSS/fonts are served from `/static`. No
  CDN, no external URLs in rendered output.
- **Server never decrypts.** Shares stay opaque and encrypted at rest;
  decryption happens only in the browser.
- **Payload access requires the password.** The server compares each download
  password against its separate verifier before returning any encrypted bytes.
- **Every download is a POST.** Payload requests wait at least 1 second and are
  rate-limited per client IP to slow brute-force attempts.
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
| `ADMIN_PASSWORD` | commented out | initial admin password |
| `MAX_UPLOAD_BYTES` | `314572800` | per-blob upload limit |
| `STORAGE_CAP_BYTES` | `419430400` | global stored-blob cap |
| `DOWNLOAD_ATTEMPTS_PER_MINUTE` | `5` | maximum payload download attempts per client IP each rolling minute |
| `TRUST_PROXY_HEADERS` | `false` | trust `X-Forwarded-For`/`X-Real-IP`/`X-Forwarded-Proto` |
| `TZ` | `Asia/Shanghai` | timezone for purge scheduling and display |

## API

API calls need no browser session and require HTTPS. API upload accepts a
client-built, encrypted payload as `multipart/form-data` at
`POST /api/v0/upload`:

The server accepts direct TLS or `X-Forwarded-Proto: https` only from a trusted
loopback proxy when `TRUST_PROXY_HEADERS=true`.

- `blob`: encrypted payload bytes;
- `password`: password used for download authorization and client decryption;
- `encrypted`: `1`;
- `cipher_meta`: JSON describing `PBKDF2-SHA-384` and `AES-256-GCM` parameters;
- `title`, `visibility`, `private_key`, `expiry_hours`, and `zip_manifest`:
  same metadata used by the web upload form.

Successful upload returns HTTP `201` with `id`, Share `url`, `download_url`,
stored `size`, and `expires_at`.

### Upload with curl

`curl` uploads an already encrypted payload; it does not encrypt source files.
Replace `<filename>` with a payload prepared using AES-256-GCM. Its
`<cipher-metadata-json>` must contain the matching PBKDF2 salt, iteration count,
and AES-GCM nonce. Use fresh random salt and nonce values for every upload.

```sh
curl -fsSL \
  --request POST \
  --form 'title=<title>' \
  --form 'visibility=public/private' \
  --form-string 'password=<password>' \
  --form 'encrypted=1' \
  --form-string 'cipher_meta=<cipher-metadata-json>' \
  --form 'zip_manifest=[]' \
  --form 'expiry_hours=1/6/12/24' \
  --form 'blob=@<filename>;type=application/octet-stream' \
  'https://<Server Domain>/api/v0/upload'
```

Choose one slash-delimited value for `visibility` and `expiry_hours`. For a
private Share, choose `private` and add
`--form-string 'private_key=<private-key>'`.

Upload responses:

- `201 Created`: payload and Share metadata stored; JSON response contains
  `id`, `url`, `download_url`, `size`, and `expires_at`.
- `400 Bad Request`: malformed multipart data, missing payload, missing required
  password or private key, invalid encryption metadata, or payload too short to
  contain an AES-GCM authentication tag.
- `403 Forbidden`: an `X-CSRF-Token` header was supplied but does not match the
  attached browser session. Command-line clients should omit this header.
- `413 Content Too Large`: encrypted payload or submitted metadata exceeds its
  configured limit.
- `415 Unsupported Media Type`: request is not `multipart/form-data`.
- `426 Upgrade Required`: request did not arrive through direct TLS or a trusted
  proxy reporting HTTPS.
- `500 Internal Server Error`: payload or metadata storage failed.
- `507 Insufficient Storage`: configured server storage capacity is exhausted.

`POST /api/v0/download/{uuid}` accepts either JSON
`{"password":"..."}` or form field `password`. A correct password returns the
raw encrypted payload as `application/octet-stream`; clients own decryption.
Wrong passwords return `401` without payload bytes. The endpoint returns `429`
with `Retry-After` after the per-IP limit is reached.

### Download with curl

Use the returned `download_url`, or place its `id` in `<uuid>`. The saved file
remains encrypted; decrypt it locally using its matching cipher metadata.

```sh
curl -fsSL \
  --request POST \
  --data-urlencode 'password=<password>' \
  --output '<filename>' \
  'https://<Server Domain>/api/v0/download/<uuid>'
```

Supply passwords through a shell secret manager or protected environment
rather than saving real values in scripts or shell history.

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
- `429 Too Many Requests`: client IP exceeded configured rolling attempt limit;
  `Retry-After` reports wait time in seconds.
- `416 Range Not Satisfiable`: requested byte range is outside payload bounds.

Both API routes return `404 Not Found` for an unknown path and `405 Method Not
Allowed` when called with a method other than `POST`.

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

- Security is the highest priority; if safety conflicts with speed, convenience, UI polish, or cleanup, choose safety and keep the tradeoff explicit.
- Admin auth must fail closed: unknown users, password-check errors, session rotation errors, CSRF failures, and ban checks must never create or preserve admin access.
- Files must stay secure on the network and at rest: preserve HTTPS/proxy trust boundaries, safe cookies, CSP, opaque encrypted blobs, sanitized filenames, and no internal UUID/blob names as user-facing download names.
- Keep the app small, boring, and easy to operate: one Go server, server-rendered pages, SQLite metadata, filesystem blobs, no SPA, no CDN, no unnecessary framework layer.
- Treat each share as one logical object made from two durable parts: metadata in SQLite and opaque bytes in the blob directory; every create, delete, purge, and repair path must keep both sides consistent.
- Keep browser and server responsibilities sharply separated: the browser zips files, always encrypts, decrypts previews/downloads, and preserves user-facing filenames; the server stores bytes, metadata, password verifiers, download authorization policy, sessions, and audit records.
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

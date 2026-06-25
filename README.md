# shareserver

A small, terminal-style file share web app in Go. Server-rendered pages, no
SPA, no CDN. Files are zipped (and optionally encrypted) in the browser; the
server stores opaque blobs plus metadata through Ent on SQLite.

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

Needs Go 1.26+ (cgo, for `go-sqlite3`), Bun, and a C toolchain.

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

Tests are split by runtime; no shell test runner is required.

```sh
go test ./...
bun test
```

This runs:

1. **`go test ./...`** — unit and route-level tests under `internal/test/` for
   Ent-backed metadata, uploads, sessions, expiry/404 pages, blob serving, and
   storage reconciliation.
2. **`bun test`** — client-side tests under `web/test/` for Progress state,
   text normalization, encryption metadata bounds, and Android-safe download
   filenames.

Storage cleanup keeps the blob directory and database in sync: a missing blob
file removes its database row, and a `.blob` file with no database row is
deleted from disk.

---

## Instruction For AI Agent

- Security is the highest priority; if safety conflicts with speed, convenience, UI polish, or cleanup, choose safety and keep the tradeoff explicit.
- Admin auth must fail closed: unknown users, password-check errors, session rotation errors, CSRF failures, and ban checks must never create or preserve admin access.
- Files must stay secure on the network and at rest: preserve HTTPS/proxy trust boundaries, safe cookies, CSP, opaque encrypted blobs, sanitized filenames, and no internal UUID/blob names as user-facing download names.
- Keep the app small, boring, and easy to operate: one Go server, server-rendered pages, SQLite metadata, filesystem blobs, no SPA, no CDN, no unnecessary framework layer.
- Treat each share as one logical object made from two durable parts: metadata in SQLite and opaque bytes in the blob directory; every create, delete, purge, and repair path must keep both sides consistent.
- Keep browser and server responsibilities sharply separated: the browser zips files, encrypts when requested, decrypts previews/downloads, and preserves user-facing filenames; the server stores bytes, metadata, policy, sessions, and audit records.
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

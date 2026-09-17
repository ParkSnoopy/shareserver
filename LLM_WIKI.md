# ShareServer Guide for AI Agents

## Purpose and authority

This document is the repository-level operating guide for AI agents that need
to understand ShareServer, call its HTTP API correctly, inspect its responses,
or modify the implementation without weakening its security model.

The guide describes the current implementation. When a statement here conflicts
with executable behavior, use the implementation and tests as the immediate
authority, then update this document in the same change. Start with the
[project README](./README.md), [release history](./CHANGELOG.md),
[route table](./internal/http/router.go), [HTTP handlers](./internal/http/handlers.go),
and [API route tests](./internal/test/api_test.go).

The term **Share** means one logical archive consisting of:

1. one SQLite metadata row; and
2. one opaque encrypted blob on the local filesystem.

The server never needs the plaintext archive password or plaintext archive
contents. Clients create a ZIP, encrypt the complete ZIP locally, and send only
the ciphertext plus safe metadata. The only plaintext-password exception in
the project is startup administration configuration through `ADMIN_PASSWORD`;
HTTP authentication still sends a derived hash, as implemented by the
[authentication module](./internal/auth/auth.go).

## Non-negotiable client rules

An AI agent using the API must follow all of these rules:

1. Use a verified HTTPS origin. Do not disable certificate verification.
2. Never send an archive password in a URL, query string, header, JSON property,
   form field, multipart field, log message, tool transcript, or error report.
3. Send only the canonical password authorization hash described below.
4. Encrypt the ZIP locally before upload. The server does not offer plaintext
   upload or server-side archive encryption.
5. Keep the plaintext password local and use it independently for encryption
   and decryption. The authorization hash is not the AES key.
6. Generate a fresh cryptographically random salt and nonce for every upload.
7. Put all multipart metadata before `blob`, and make `blob` the final part.
8. Treat `password_hash`, the plaintext password, and a private lookup key as
   sensitive. Never emit their values in normal diagnostics.
9. Preserve the upload response, especially `id`, `url`, `download_url`,
   `expires_at`, and `cipher_meta`. The download response contains ciphertext,
   not decryption metadata.
10. Do not blindly retry an upload after an ambiguous network failure. Uploads
    have no idempotency key, so a retry can create a second Share.
11. Do not probe passwords. Failed download authorization is persisted per IP,
    and the eleventh failure inside a strict rolling minute creates a durable ban.
12. Treat a decrypted ZIP as untrusted input. Enforce extraction limits, reject
    path traversal and unsafe links, avoid overwriting files, and never execute
    extracted content automatically.

## Project identity

ShareServer is a small Go web application with server-rendered HTML and native
browser JavaScript modules. Its main components are:

| Layer | Responsibility | Primary files |
| --- | --- | --- |
| Process entry | Load configuration, open SQLite, initialize integrity services, start cleanup, serve HTTP | [server entry point](./cmd/shareserver/main.go), [application container](./internal/app/app.go) |
| Configuration | Read the environment and optional local environment file, apply development defaults, fail closed for production secrets | [configuration loader](./internal/config/config.go), [environment example](./.env.example) |
| Routing and middleware | Register routes; apply security headers, request clocks, stateless API behavior, CSRF policy, body limits, proxy trust, and client-IP resolution | [route table](./internal/http/router.go), [middleware](./internal/http/middleware.go) |
| HTTP adaptation | Parse multipart and credential bodies, map domain errors to HTTP statuses, and stream responses | [HTTP handlers](./internal/http/handlers.go) |
| Upload policy | Validate metadata, reserve capacity, derive a verifier, stage ciphertext, commit the blob, insert metadata, and roll back failures | [upload module](./internal/upload/upload.go) |
| Download protection | Delay responses, persist failed attempts, create and expire IP bans, and produce `Retry-After` | [download protection](./internal/http/download.go) |
| Authentication | Validate canonical password hashes, store bcrypt verifiers, hash private lookup keys, and protect admin login | [authentication module](./internal/auth/auth.go) |
| Share model and queries | Define active/expired semantics and isolate Ent query behavior | [Share model](./internal/share/model.go), [Share store](./internal/share/store.go) |
| Blob storage | Stage, hash, size-limit, atomically commit, count, remove, purge, and reconcile files | [blob store](./internal/storage/blobstore.go), [integrity service](./internal/storage/integrity.go) |
| Database | Open SQLite through Ent, run schema creation, and serialize DB access through one connection | [database bootstrap](./internal/db/db.go), [Share schema](./internal/ent/schema/share.go) |
| Browser client | ZIP files, derive authorization hashes, encrypt/decrypt ciphertext, invoke the API, and render archive entries | [upload client](./web/static/js/upload.js), [crypto client](./web/static/js/crypto.js), [Share client](./web/static/js/share.js), [archive client](./web/static/js/archive.js), [ZIP helpers](./web/static/js/zip.js) |
| Public documentation UI | Render the active public archive list and concise API contract | [API template](./web/templates/api.html) |

The module and dependency versions are declared in [the Go module](./go.mod).
The container build is described by [the Dockerfile](./Dockerfile), and the
container starts through [the entrypoint](./entrypoint.sh).

## Public route inventory

The current routes are registered in the [route table](./internal/http/router.go).

| Method | Path | Purpose | Session behavior |
| --- | --- | --- | --- |
| `GET` | `/api/` | HTML archive list and human-readable API usage page | Sessionless; an existing cookie may be read, but no session is created |
| `POST` | `/api/v0/upload` | Store one client-encrypted ZIP payload | Sessionless; an existing admin session is optional |
| `POST` | `/api/v0/list` | Return active private Shares matching a private lookup key | Sessionless |
| `POST` | `/api/v0/download/{uuid}` | Authorize and stream one encrypted payload | Sessionless |
| `GET` | `/s/{uuid}` | Render the browser Share page with current cipher metadata | Browser session route |
| `GET` | `/` and `/s/` | Public archive browsing and private-key lookup UI | Browser session route |
| `GET` | `/upload` | Browser ZIP/encryption upload UI | Browser session route |
| `GET` | `/robots.txt` | Allow crawler access only to `/api/` | Sessionless |
| `GET`/`POST` | `/admin/...` | Administrative login, inspection, and deletion | Browser session and CSRF protected |

`GET /api/` is HTML and lists at most the 100 newest active public Shares.
`POST /api/v0/list` is the machine-readable private-key lookup and returns at
most the 100 newest active private Shares matching that key. Both queries are
defined by the [Share store](./internal/share/store.go), while the public page is
rendered by the [API template](./web/templates/api.html). The crawler policy is
the [robots file](./web/robots.txt).

Unknown paths return `404`. The API operation routes are registered only for
`POST`; another method receives the router's `405 Method Not Allowed` response.

## Transport, proxy, and session model

### HTTPS requirement

All three operation endpoints reject insecure requests with `426 Upgrade Required`
and the text body `HTTPS required`.

A request is secure when either:

- Go receives direct TLS; or
- `TRUST_PROXY_HEADERS` is enabled, the direct peer is trusted, and
  `X-Forwarded-Proto` equals `https` case-insensitively.

Trusted proxy peers are loopback addresses. In a Railway environment, a peer in
`100.0.0.0/8` is also trusted only when `RAILWAY_ENVIRONMENT_ID` is present.
Untrusted clients cannot make a request secure merely by adding forwarding
headers. The exact rules live in [HTTP middleware](./internal/http/middleware.go),
with proxy regression coverage in [API route tests](./internal/test/api_test.go).

### Client IP

The IP used for upload audit events and download bans is resolved in this order:

1. first valid address in `X-Forwarded-For`, but only from a trusted proxy;
2. `X-Real-IP`, under the same trust condition; or
3. the direct remote address with its port removed.

An agent must not send forwarding headers unless it is itself the configured
trusted reverse proxy.

### Stateless API behavior

The `/api/`, upload, private-list, download, and crawler-policy routes do not
create browser session rows or issue session cookies. If a valid `sid` cookie
already exists, the server may attach that session to the request without
creating a new one.
This behavior is implemented in [HTTP middleware](./internal/http/middleware.go).

Ordinary machine clients should omit cookies and `X-CSRF-Token`. Upload has one
intentional browser-admin extension: a valid existing admin session plus its
matching `X-CSRF-Token` allows the longer administrator expiry ceiling. If an
agent sends any `X-CSRF-Token` and it does not match the attached session, the
upload is rejected with `403 Forbidden`. A stateless agent cannot gain admin
policy by inventing a token.

### Common response headers

Middleware sets these headers on every normal application response:

- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: same-origin`
- `X-Frame-Options: DENY`
- a same-origin Content Security Policy with no external script, style, font,
  object, or form-action origins

The application intentionally does not add HSTS. TLS termination and HSTS, if
desired, belong to deployment infrastructure. Header behavior is implemented in
[HTTP middleware](./internal/http/middleware.go) and covered by
[HTTP tests](./internal/test/http_test.go).

## Password authorization contract

### Exact derivation

For archive upload and download authorization, derive:

```text
normalized = NFC(password)
message = UTF8("shareserver-download-password") || 0x00 || UTF8(normalized)
digest = SHA-256(message)
password_hash = standard padded Base64(digest)
```

The result must be the canonical standard Base64 encoding of exactly 32 digest
bytes. In practice this is a 44-character padded string. URL-safe Base64,
unpadded Base64, hexadecimal text, a SHA-256 digest without the domain prefix,
or a hash of non-normalized text is not the same credential.

The browser implementation is `downloadPasswordHash` in the
[crypto client](./web/static/js/crypto.js). Server-side canonical validation and
verifier comparison are in the [authentication module](./internal/auth/auth.go).

### Security meaning

`password_hash` is a reusable authorization credential. It is not plaintext,
but anyone holding it can ask the server for the encrypted payload. Treat it as
sensitive and redact it from logs, prompts, traces, command history, tickets,
and persisted agent memory.

The server does not store this value directly. It decodes the canonical Base64
digest and stores a salted bcrypt verifier in the Share row. On download it
validates the supplied canonical value and compares the decoded digest against
that verifier. The metadata field retains the historical name
`download_password_hash`; semantically it contains the bcrypt verifier.

The authorization hash does not encrypt or decrypt the archive. Encryption uses
the locally held plaintext password through PBKDF2. An agent must therefore keep
the plaintext password only in ephemeral local state when it needs to encrypt or
decrypt, while transmitting only `password_hash`.

## Client-side payload format

### Step 1: construct a ZIP

The plaintext passed to encryption must be a valid ZIP archive, even for one
file. The first-party browser uses level-6 compression and sanitizes entry names
through the [ZIP helpers](./web/static/js/zip.js). The server cannot verify this
because it never decrypts the payload.

Before encryption, an AI client should:

- preserve intended relative paths without absolute paths;
- remove `.` and `..` path components;
- avoid duplicate entry names or resolve them deterministically;
- avoid placing credentials in filenames or ZIP comments;
- verify that the resulting ZIP opens locally; and
- ensure the encrypted result will fit the configured upload limit.

### Step 2: derive the encryption key

The current client encryption format is:

| Parameter | Required value |
| --- | --- |
| Password bytes | UTF-8 bytes of `NFC(password)` |
| KDF | PBKDF2-HMAC-SHA-384 |
| Iterations | First-party client uses `600000`; server accepts `100000` through `1200000` inclusive |
| Salt | 16 fresh cryptographically random bytes |
| Output key | 256 bits |
| Cipher | AES-256-GCM |
| Nonce | 12 fresh cryptographically random bytes |
| Additional authenticated data | none |
| Authentication tag | 16 bytes, appended to ciphertext by the Web Crypto representation |

The normative implementation for this repository is `encryptBlob` in the
[crypto client](./web/static/js/crypto.js). Do not copy the legacy framed
decryption support as an upload format: current uploads accept only the single
AES-256-GCM payload format.

### Step 3: serialize `cipher_meta`

Serialize a JSON object with exactly the intended values:

```json
{
  "kdf": "PBKDF2-SHA-384",
  "iterations": 600000,
  "salt": "<standard padded Base64 of 16 bytes>",
  "cipher": "AES-256-GCM",
  "nonce": "<standard padded Base64 of 12 bytes>"
}
```

The server checks the KDF and cipher names, iteration range, Base64 decoding,
salt length, and nonce length in the [upload module](./internal/upload/upload.go).
It does not decrypt the blob or validate the GCM tag. A syntactically accepted
upload can therefore still be undecryptable if the client encrypted incorrectly.
The client must perform a local round-trip or equivalent cryptographic
verification before upload when reliability matters.

### `zip_manifest`

`zip_manifest` is currently accepted for wire compatibility and size-checked,
but new Share rows store `[]`. Do not depend on it for archive recovery or MIME
metadata. Clients must inspect ZIP entries after decryption. This behavior is
visible in the [upload module](./internal/upload/upload.go).

## Private archive-list endpoint

### Request

```text
POST /api/v0/list
```

The lookup endpoint requires verified HTTPS and accepts exactly one non-empty
`private_key` string in either JSON or URL-encoded form data. It is sessionless,
does not require CSRF state, and does not create a browser session.

#### JSON

```http
Content-Type: application/json

{"private_key":"<private lookup key>"}
```

#### URL-encoded form

```http
Content-Type: application/x-www-form-urlencoded

private_key=<percent-encoded private lookup key>
```

The endpoint rejects:

- any query string;
- an empty or missing key;
- more than one JSON property;
- an unknown JSON property;
- duplicate JSON or form values;
- a second JSON value after the request object;
- unsupported content types; and
- a key larger than 64 KiB.

The strict body parser is shared with download authorization and lives in the
[HTTP handlers](./internal/http/handlers.go). A private key belongs only in the
request body. Never place it in a URL, because URLs commonly enter browser
history, reverse-proxy logs, analytics, and referrer data.

### Lookup and filtering

After parsing the key, the server:

1. reconciles Share rows and blob files so missing or orphaned storage is not
   advertised;
2. computes `HMAC-SHA-256(APP_SECRET, private_key)`;
3. queries only active Shares whose visibility is exactly `private`;
4. requires each result to be encrypted and download-password protected;
5. orders matching Shares newest first; and
6. limits the response to 100 entries.

The raw key is used transiently for HMAC derivation. It is never written to the
Share row, audit log, or response. Lookup hashing is implemented by the
[authentication module](./internal/auth/auth.go); filtering is implemented by
the [Share store](./internal/share/store.go).

### Success response

Both a matching key and a key with no matches return `200 OK`,
`Content-Type: application/json`, and `Cache-Control: no-store`. An empty result
does not reveal whether the key was previously valid.

```json
{
  "archives": [
    {
      "id": "<uuid>",
      "title": "<share title>",
      "url": "/s/<uuid>",
      "download_url": "/api/v0/download/<uuid>",
      "size": 12345,
      "expires_at": "<UTC RFC3339Nano timestamp>",
      "created_at": "<UTC RFC3339Nano timestamp>",
      "cipher_meta": {
        "kdf": "PBKDF2-SHA-384",
        "iterations": 600000,
        "salt": "<Base64>",
        "cipher": "AES-256-GCM",
        "nonce": "<Base64>"
      },
      "encryption": "client"
    }
  ]
}
```

The response intentionally excludes the raw private key, private-key HMAC,
download-password verifier, uploader IP, blob path, blob checksum, and audit
metadata. Relative URLs must be resolved against the same verified HTTPS origin.
The included `cipher_meta` lets an API client decrypt a matching private Share
after separately authorizing and downloading its payload.

The private key lists Shares; it does not authorize payload bytes. Each returned
archive still requires the corresponding archive password hash at
`POST /api/v0/download/{uuid}` and the plaintext password locally for AES-GCM
decryption.

### Errors and retry behavior

| Status | Meaning | Agent action |
| --- | --- | --- |
| `400 Bad Request` | The body, key count, key size, query string, or content type violates the strict request contract | Fix the request; do not retry unchanged |
| `426 Upgrade Required` | The request lacked direct TLS or trusted forwarded HTTPS | Correct TLS or proxy configuration |

The endpoint has no wrong-key error and no pagination token. A successful empty
list is terminal for that exact key at that instant. Do not brute-force private
keys or send concurrent key guesses. The endpoint has no fixed two-second delay;
the delay and persistent failure-ban policy apply to payload authorization, not
archive lookup.

## Upload endpoint

### Request

```text
POST /api/v0/upload
Content-Type: multipart/form-data; boundary=...
```

The request body must be a multipart stream. Metadata parts must precede the
file part, and `blob` must be final. The parser rejects duplicate fields,
unknown fields, extra file fields, a `blob` without a filename, and any part
after `blob`. Parsing is implemented by `uploadParts` in the
[HTTP handlers](./internal/http/handlers.go).

### Multipart fields

| Field | Required | Contract |
| --- | --- | --- |
| `title` | No | Empty becomes `untitled share`; maximum 512 bytes |
| `visibility` | No | Exact `private` stays private; every other value becomes public. A correct client must send exact `public` or `private` rather than relying on fallback |
| `private_key` | For private Shares | Lookup key sent to the server; the server stores only an HMAC derived with `APP_SECRET`. This key is not the archive password and does not encrypt the payload |
| `password_hash` | Yes | Canonical authorization value described above; plaintext `password` is an unknown field and is rejected |
| `encrypted` | Yes | Must be `1` or `true`; any other value is rejected |
| `cipher_meta` | Yes | JSON metadata for PBKDF2-SHA-384 and AES-256-GCM |
| `zip_manifest` | No | Maximum 64 KiB; currently normalized to `[]` in storage |
| `expiry_hours` | No | Base-10 integer hours. Missing, invalid, or non-positive becomes 6. Anonymous maximum is 24; authenticated administrator maximum is 2160 |
| `csrf` | No | Accepted as multipart metadata for browser compatibility, but API admin elevation is authorized by the `X-CSRF-Token` header and existing session |
| `blob` | Yes | Final multipart part, must have a filename, must contain client-encrypted ZIP bytes, and must be at least 16 bytes |

Each metadata part is bounded to 64 KiB by the HTTP parser. The deeper upload
policy applies tighter limits to title, cipher metadata, and password-hash input.
The whole HTTP request is bounded to the configured maximum payload plus 4 MiB
of multipart overhead. The authoritative code is in
[HTTP middleware](./internal/http/middleware.go),
[HTTP handlers](./internal/http/handlers.go), and the
[upload module](./internal/upload/upload.go).

### Recommended construction order

An agent should append parts in this order:

1. `title`
2. `visibility`
3. `private_key` when visibility is private
4. `password_hash`
5. `encrypted=1`
6. `cipher_meta`
7. `expiry_hours`
8. optional `zip_manifest=[]`
9. `blob` with an application/octet-stream content type and a non-empty filename

Multipart libraries often preserve append order, but an agent must verify that
its chosen library does so. Do not use a map whose iteration order is undefined
to serialize the request.

### Minimal request shape

```sh
curl -fsS \
  --request POST \
  --form-string 'title=<safe title>' \
  --form 'visibility=public' \
  --form-string 'password_hash=<canonical Base64 SHA-256 value>' \
  --form 'encrypted=1' \
  --form-string 'cipher_meta=<compact JSON object>' \
  --form 'expiry_hours=6' \
  --form 'blob=@<local encrypted ZIP>;type=application/octet-stream' \
  'https://<host>/api/v0/upload'
```

This example deliberately uses placeholders. Never materialize a real password,
authorization hash, private key, or secret in repository files or agent output.
For diagnostics, capture status, headers, and a redacted body rather than using
verbose HTTP traces that expose multipart values.

### Server processing sequence

The upload path is deliberately staged:

1. Verify HTTPS.
2. Resolve the trusted client IP.
3. If `X-CSRF-Token` exists, compare it with the attached session and determine
   whether the requester has administrator expiry privileges.
4. Require multipart content.
5. Parse unique allowed metadata fields before the blob and expose the blob as a
   stream; require the blob to be final.
6. Validate title size, password-hash shape, encryption mode, cipher metadata,
   visibility, private-key requirement, and expiry policy before writing bytes.
7. Purge unprotected legacy Shares and expired Shares before capacity
   calculation.
8. Reserve at most the configured per-upload maximum and remaining global
   capacity. Reservations prevent concurrent uploads from overcommitting space.
9. Decode the authorization hash and compute a salted bcrypt verifier. Verifier
   work is serialized to bound anonymous CPU usage.
10. Stream the ciphertext into a private `.staging` file, enforce the reserved
    byte limit, and compute the ciphertext SHA-256 while writing.
11. Recheck actual storage usage under the integrity lock.
12. Atomically rename the staged file to `<uuid>.blob`.
13. Insert the Share metadata row. If insertion fails, remove the committed blob.
14. Write a safe upload audit event.
15. Return `201 Created` JSON.

The orchestration is in the [upload module](./internal/upload/upload.go), staging
and atomic rename are in the [blob store](./internal/storage/blobstore.go), and
metadata insertion is in the [Share store](./internal/share/store.go).

### Success response

The server returns `201 Created`, `Content-Type: application/json`, and
`Cache-Control: no-store`.

```json
{
  "id": "<uuid-v4>",
  "url": "/s/<uuid-v4>",
  "download_url": "/api/v0/download/<uuid-v4>",
  "size": 12345,
  "expires_at": "<UTC RFC3339Nano timestamp>",
  "cipher_meta": {
    "kdf": "PBKDF2-SHA-384",
    "iterations": 600000,
    "salt": "<Base64>",
    "cipher": "AES-256-GCM",
    "nonce": "<Base64>"
  },
  "encryption": "client"
}
```

`url` and `download_url` are origin-relative, not absolute. Resolve them against
the exact HTTPS origin used for upload. `size` is the stored ciphertext size,
not the original files' total size or plaintext ZIP size. `cipher_meta` is a JSON
object because the handler emits the validated raw JSON value rather than a
quoted JSON string.

Persist the response atomically with the local information needed for later
decryption. At minimum retain the Share ID, HTTPS origin, cipher metadata, and a
secure reference to the password. The endpoint does not provide recovery for a
lost password.

### Upload errors

Error bodies produced directly by the application are plain text and normally
end with a newline. Do not expect a JSON error envelope.

| Status | Meaning | Agent action |
| --- | --- | --- |
| `400 Bad Request` | Malformed multipart body, unknown/duplicate/trailing field, missing blob, missing filename, missing private key, missing/invalid hash, invalid encryption mode, invalid cipher metadata, or payload shorter than an AES-GCM tag | Fix the request; do not retry unchanged |
| `403 Forbidden` | A supplied CSRF header did not match the attached session | Remove browser-only auth state or acquire a valid session through the browser flow |
| `413 Request Entity Too Large` | Metadata, HTTP body, or ciphertext exceeded a limit | Reduce archive size or metadata; do not chunk into multiple requests unless separate Shares are intended |
| `415 Unsupported Media Type` | Request is not multipart/form-data | Rebuild the body with a multipart library and its generated boundary |
| `426 Upgrade Required` | Direct TLS or trusted forwarded HTTPS was absent | Correct the origin or proxy deployment; never bypass TLS checks |
| `500 Internal Server Error` | Staging, verifier, database, or filesystem storage failed | Retry only with bounded backoff and only when duplicate creation risk is acceptable |
| `507 Insufficient Storage` | Global capacity could not admit the upload | Stop and notify the operator; repeated retries cannot create capacity |

Because the endpoint has no idempotency token, a connection failure after the
request body was sent is ambiguous. Before retrying, check any captured complete
response. There is no reliable API lookup by title, checksum, or client request
ID, so automatic retries can duplicate data.

## Download endpoint

### Request

```text
POST /api/v0/download/{uuid}
```

`{uuid}` must have canonical UUID structure. Invalid, unknown, missing,
unprotected, and wrong-password targets deliberately collapse into the same
authorization failure path.

The endpoint accepts exactly one of these body encodings:

#### JSON

```http
Content-Type: application/json

{"password_hash":"<canonical Base64 SHA-256 value>"}
```

The decoder rejects unknown JSON fields, missing or empty `password_hash`, an
oversized value, malformed JSON, and any second JSON value after the object.

#### URL-encoded form

```http
Content-Type: application/x-www-form-urlencoded

password_hash=<percent-encoded canonical Base64 SHA-256 value>
```

The form must contain exactly one key and exactly one value. Duplicate values or
additional form keys are rejected.

Any query string is rejected, even if it contains unrelated data. Plaintext
`password`, multipart bodies, `text/plain`, absent content type, and other media
types are rejected. Rejections use the same denial flow as a wrong credential
and therefore count toward the IP failure limit. Parsing is implemented by
`downloadPasswordHash` in the [HTTP handlers](./internal/http/handlers.go).

### Server decision sequence

The server handles a download in this order:

1. Start a monotonic response-delay timer.
2. Verify HTTPS.
3. Resolve the trusted client IP.
4. Look up the namespaced persistent ban for that IP. Delete it if it expired.
5. Parse the body using one accepted content type.
6. Validate the UUID, load the Share and blob path, require an encrypted and
   protected Share, validate the canonical hash, and compare its bcrypt verifier.
7. For any denial through step 6, persist one failed attempt. If this is the
   eleventh attempt in the strict rolling window, create the ban immediately.
8. After successful authorization, classify expiry. An expired Share returns
   `410 Gone` and does not add a failed-password event.
9. Wait until at least two seconds have elapsed since handler entry.
10. Set download headers and delegate file streaming to Go's file server.

The handler lives in [HTTP handlers](./internal/http/handlers.go), and persistent
failure/ban policy lives in [download protection](./internal/http/download.go).

### Success response

A full successful response is normally:

```http
HTTP/1.1 200 OK
Content-Type: application/octet-stream
Content-Disposition: attachment; filename="<uuid>.payload"
Cache-Control: no-store
Content-Length: <ciphertext bytes>

<raw encrypted payload bytes>
```

The body is exactly the stored ciphertext; it is not a ZIP until the client
decrypts it. The filename is an internal payload name, not an original archive
entry name. The server can also honor valid byte ranges through Go's file-serving
implementation, yielding `206 Partial Content`; clients must reassemble exact
ciphertext bytes before AES-GCM decryption.

Every response generated by the download POST handler waits at least two
seconds, including success, denial, expiry, insecure transport, and active-ban
responses. Network transfer time comes after that floor.

### Download errors

| Status | Meaning | Agent action |
| --- | --- | --- |
| `401 Unauthorized` | Body was malformed, hash was missing/invalid/incorrect, UUID was invalid/unknown, or Share lacked required protection | Do not distinguish targets; check local inputs once, then stop rather than probing |
| `404 Not Found` | Authorization succeeded but the blob disappeared before file serving | Treat as server integrity failure; do not repeatedly submit credentials |
| `410 Gone` | Credential matched, but the Share is expired | Stop; expiry is terminal |
| `416 Range Not Satisfiable` | Requested byte range is outside the ciphertext | Correct or remove the Range header |
| `426 Upgrade Required` | HTTPS trust requirement failed | Correct TLS or proxy configuration |
| `429 Too Many Requests` | IP is banned, ban-state lookup failed closed, or failure persistence failed | Parse `Retry-After`, stop all attempts from that IP, and wait at least that many seconds |

The text `download denied` intentionally does not reveal which authorization
condition failed. Do not infer Share existence from timing: the fixed delay is
designed to reduce this distinction.

## Failed-download persistence and bans

Download protection is independent from administrator-login protection. Its
database keys are prefixed with `download:` so both policies can share the
failure and ban tables without colliding.

Current fixed policy:

- one failed request creates one persistent failure event;
- only events strictly newer than `now - 1 minute` count;
- exactly ten failures do not ban;
- the eleventh failure in that rolling window creates the ban;
- duration is 24 hours plus random whole-second jitter from -3600 through
  +18000 seconds, inclusive, producing a 23-to-29-hour duration;
- the ban survives process restarts;
- `Retry-After` is the positive ceiling of remaining seconds;
- expired ban rows are deleted when the next request checks them;
- stale namespaced failure events are pruned during failure recording; and
- a successful download does not reset prior download failures.

That last rule matters: after ten recent failures, a successful request can
succeed, but the next failed request still becomes the eleventh failure if the
earlier events remain in the rolling window.

If reading or parsing durable ban state fails, the server fails closed and
treats the IP as banned. If recording a failure fails, the response is `429`
with a conservative `Retry-After: 60`.

The policy is implemented in [download protection](./internal/http/download.go).
Persistence is defined by the
[failure-event schema](./internal/ent/schema/loginfailureevent.go) and
[IP-ban schema](./internal/ent/schema/ipban.go). Its key behavior is covered by
[API route tests](./internal/test/api_test.go).

## Decrypting a downloaded payload

To recover the ZIP:

1. Obtain the exact `cipher_meta` associated with the upload. The download
   response does not include it.
2. Validate the metadata before allocating expensive KDF or buffer work.
3. Normalize the plaintext password to NFC and encode it as UTF-8.
4. Decode the 16-byte salt and 12-byte nonce from standard Base64.
5. Run PBKDF2-HMAC-SHA-384 for the recorded iteration count to derive a 256-bit
   key.
6. AES-GCM-decrypt the complete ciphertext with the nonce, no additional data,
   and the final 16 bytes as the authentication tag.
7. If authentication fails, report one generic wrong-password-or-corruption
   result. Do not retry variants automatically.
8. Parse the plaintext as ZIP only after GCM authentication succeeds.
9. Apply archive-bomb, path, link, overwrite, and execution protections before
   extraction.

The first-party flow downloads to a pre-sized byte buffer, decrypts directly to
bytes, then unzips in-browser to reduce duplicate memory pressure. See the
[Share client](./web/static/js/share.js), [archive client](./web/static/js/archive.js),
[crypto client](./web/static/js/crypto.js), and [ZIP helpers](./web/static/js/zip.js).

For a private Share, `POST /api/v0/list` returns `cipher_meta` after a matching
private-key lookup. There is no equivalent JSON metadata lookup for a public
Share. The browser Share page embeds metadata in HTML through the
[Share template](./web/templates/share.html), but that markup is a UI
implementation detail rather than a stable machine API. A robust automation
workflow should retain metadata at upload time or obtain it through the private
list endpoint rather than scraping HTML.

## Public and private Shares

Encryption and visibility are separate controls:

- Every current Share is encrypted and password-authorized.
- A public Share appears in public archive listings while active.
- A private Share is omitted from public listings.
- `POST /api/v0/list` returns active private Shares matching the submitted key.
- The private lookup key is submitted to the server and converted to
  `HMAC-SHA-256(APP_SECRET, private_key)` for matching.
- The raw private lookup key is not stored in the Share row.
- The private key is not used by PBKDF2, AES-GCM, or download authorization.
- A direct `/s/{uuid}` link can render a private Share; "private" means unlisted,
  not inaccessible by UUID.

The listing and lookup queries live in the [Share store](./internal/share/store.go),
and private-key hashing lives in the [authentication module](./internal/auth/auth.go).
An agent creating a private Share must retain both the private lookup key and the
archive password for their distinct purposes.

## Data persistence

### SQLite

The server uses SQLite through Ent and `go-sqlite3`. It enables foreign keys,
uses a five-second busy timeout, and restricts the connection pool to one open
connection. Schema creation runs at startup. See the
[database bootstrap](./internal/db/db.go) and [Go module](./go.mod).

The Share row records:

- UUID, title, and visibility;
- optional HMAC private-key lookup value;
- optional bcrypt download-password verifier;
- encryption flag and cipher metadata;
- normalized ZIP manifest;
- ciphertext size, path, and SHA-256;
- uploader IP;
- expiry and creation timestamps; and
- optional purge marker retained for compatibility with model status logic.

The authoritative schema is the [Share schema](./internal/ent/schema/share.go),
with hand-written domain mapping in the [Share model](./internal/share/model.go)
and [Share store](./internal/share/store.go).

Other durable tables include administrator credentials, browser sessions, audit
events, login/download failure events, and namespaced IP bans. Their definitions
are the [administrator schema](./internal/ent/schema/admin.go),
[session schema](./internal/ent/schema/session.go),
[audit-event schema](./internal/ent/schema/auditevent.go),
[failure-event schema](./internal/ent/schema/loginfailureevent.go), and
[IP-ban schema](./internal/ent/schema/ipban.go).

### Blob filesystem

Ciphertext is written under the configured blob directory:

1. a fresh UUID identifies the Share;
2. staging creates `.staging/<uuid>.tmp` with mode `0600`;
3. streaming computes ciphertext SHA-256 and enforces the reservation;
4. commit atomically renames the file to `<uuid>.blob`; and
5. metadata insertion completes the logical Share.

Late failures remove staged or committed bytes. The implementation is in the
[blob store](./internal/storage/blobstore.go) and [upload module](./internal/upload/upload.go).

### Capacity and concurrency

Before reading a potentially slow upload, the server reserves the smaller of:

- the configured per-upload maximum; or
- global capacity minus committed blob bytes and other active reservations.

The final committed size is checked again under a process-wide capacity mutex
and the integrity lock. This prevents concurrent requests from independently
observing the same free space. Storage limits are configured through the
[configuration loader](./internal/config/config.go).

## Expiry, purge, and reconciliation

A Share is active when it is not marked purged and its optional expiry is later
than the request's canonical UTC timestamp. Expiry at exactly `now` is expired.
The rule is centralized in the [Share model](./internal/share/model.go).

The server performs cleanup:

- synchronously at startup;
- before storage-cap decisions during upload;
- before public/archive and admin pages render; and
- daily at the next midnight in configured `TZ`.

Cleanup removes abandoned staging files at startup, unprotected legacy Shares,
expired Share blob/row pairs, metadata whose blob is missing, orphan blob files,
and expired browser sessions. Removal deletes a regular blob before deleting its
metadata row; unexpected filesystem errors preserve metadata for a later retry.
See the [cleanup scheduler](./internal/http/cleanup.go) and
[integrity service](./internal/storage/integrity.go).

## Audit behavior

Uploads and selected administrator actions produce audit rows containing actor,
IP, action, target, safe metadata, and timestamp. Audit write failure does not
fail the primary operation. Audit metadata must never include password values,
authorization hashes, private lookup keys, plaintext archive contents, or other
browser-only secrets. The implementation is the [audit logger](./internal/audit/audit.go)
and [audit-event schema](./internal/ent/schema/auditevent.go).

## AI-agent request algorithm

For a new upload, use this decision procedure:

1. Confirm the exact HTTPS base origin.
2. Read the local files without executing them.
3. Build and locally validate a safe ZIP.
4. Obtain the password through a secret-safe channel; never print it.
5. Normalize the password to NFC.
6. Generate a fresh 16-byte salt and 12-byte nonce.
7. Derive the AES key with PBKDF2-HMAC-SHA-384.
8. Encrypt the full ZIP with AES-256-GCM.
9. Derive the separate domain-scoped SHA-256 authorization hash.
10. Validate locally that this hash is canonical padded Base64 of 32 bytes.
11. Construct multipart metadata in deterministic order.
12. Append the encrypted blob last.
13. Send once over verified HTTPS.
14. Parse status before parsing a body as JSON.
15. On `201`, validate required response fields and atomically retain decryption
    metadata.
16. On an ambiguous transport failure, do not automatically retry.
17. Redact all credential-equivalent values from the task report.

For an existing Share download:

1. Confirm the HTTPS origin, UUID, retained cipher metadata, and password source.
2. Derive one authorization hash locally.
3. Send one JSON POST with exactly `password_hash` and no query string.
4. Wait for the deliberately delayed response; do not time out below the
   two-second policy floor plus normal network allowance.
5. On `200` or `206`, stream ciphertext to bounded local storage or memory.
6. On `401`, check local formatting once and stop; do not guess.
7. On `429`, honor `Retry-After` and stop concurrent attempts from the same IP.
8. On `410`, mark the Share terminally expired.
9. Verify complete ciphertext length before decryption.
10. Decrypt and authenticate locally, then inspect/extract the ZIP safely.

For private-key discovery before download:

1. Send one body-only private key to `POST /api/v0/list` over verified HTTPS.
2. Treat `200 OK` with an empty `archives` array as a normal no-match result.
3. Select a returned Share by stable `id`, not by potentially duplicated title.
4. Retain its `cipher_meta` and use its `download_url` for separate password-hash
   authorization.
5. Do not log the private key or attempt key enumeration.

## Retry and timeout guidance

- Set download timeouts above the fixed two-second delay and expected payload
  transfer duration.
- Do not use aggressive generic HTTP retry middleware on upload.
- A `400`, `401`, `403`, `410`, `413`, `415`, `416`, `426`, or `507` response
  requires input, state, deployment, or capacity correction rather than an
  unchanged retry.
- A `429` must use `Retry-After`; do not add parallel retries.
- A `500` may be transient, but upload outcome can be ambiguous. Require an
  operator decision or an application-level deduplication plan before retry.
- Download POST is read-only with respect to the Share, but denied attempts
  mutate persistent rate-limit state.

## Safe observability

Permitted diagnostics include:

- endpoint path with UUID redacted when unnecessary;
- response status;
- request/response byte counts;
- elapsed duration;
- content type;
- retry-after seconds;
- local phase names such as ZIP, encrypt, upload, download, decrypt, and unzip;
  and
- exception class without sensitive inputs.

Do not log:

- plaintext passwords;
- `password_hash` values;
- raw private lookup keys;
- cookies or CSRF tokens;
- full multipart bodies;
- decrypted file contents;
- cipher keys; or
- environment secrets.

The browser's expected high-level sequence is visible in the
[upload client](./web/static/js/upload.js) and [Share client](./web/static/js/share.js).

## Modifying the project safely

An AI coding agent changing this repository should preserve these boundaries:

1. Keep HTTP parsing and response selection in the
   [HTTP handlers](./internal/http/handlers.go).
2. Keep upload validation, capacity, and rollback policy in the
   [upload module](./internal/upload/upload.go).
3. Keep password, private-key, and admin-login rules in the
   [authentication module](./internal/auth/auth.go).
4. Keep active/expired classification in the
   [Share model](./internal/share/model.go).
5. Keep database query composition in the [Share store](./internal/share/store.go)
   or Ent-generated accessors rather than adding ad hoc SQL.
6. Keep blob/row consistency under the
   [integrity service](./internal/storage/integrity.go).
7. Preserve the stateless API boundary and trusted-proxy checks in
   [HTTP middleware](./internal/http/middleware.go).
8. Update browser and server contracts together when changing encryption or
   password derivation.
9. Preserve generic anonymous error behavior.
10. Add route-level tests for request/response changes and client-side tests for
    browser-only cryptography or archive behavior.

The strongest executable references are:

- [API route tests](./internal/test/api_test.go) for plaintext rejection,
  multipart ordering, HTTPS, download delay, bans, and ban expiry;
- [HTTP tests](./internal/test/http_test.go) for session, login-hash, page, and
  security-header behavior;
- [upload tests](./internal/test/upload_test.go) for policy, rollback, size, and
  capacity behavior; and
- [crypto tests](./web/test/crypto.test.mjs) for password derivation and cipher
  metadata behavior.

Run the current validation commands from the repository root:

```sh
go test ./...
go vet ./...
bun test
```

## Linked file index

Every project file referenced by this guide is linked below for direct agent
navigation.

### Project and build

- [README](./README.md)
- [CHANGELOG](./CHANGELOG.md)
- [Go module](./go.mod)
- [environment example](./.env.example)
- [Dockerfile](./Dockerfile)
- [container entrypoint](./entrypoint.sh)

### Server wiring and HTTP

- [server entry point](./cmd/shareserver/main.go)
- [application container](./internal/app/app.go)
- [configuration loader](./internal/config/config.go)
- [route table](./internal/http/router.go)
- [HTTP middleware](./internal/http/middleware.go)
- [HTTP handlers](./internal/http/handlers.go)
- [download protection](./internal/http/download.go)
- [cleanup scheduler](./internal/http/cleanup.go)

### Domain, persistence, and security

- [authentication module](./internal/auth/auth.go)
- [audit logger](./internal/audit/audit.go)
- [database bootstrap](./internal/db/db.go)
- [Share model](./internal/share/model.go)
- [Share store](./internal/share/store.go)
- [upload module](./internal/upload/upload.go)
- [blob store](./internal/storage/blobstore.go)
- [integrity service](./internal/storage/integrity.go)
- [Share schema](./internal/ent/schema/share.go)
- [administrator schema](./internal/ent/schema/admin.go)
- [session schema](./internal/ent/schema/session.go)
- [audit-event schema](./internal/ent/schema/auditevent.go)
- [failure-event schema](./internal/ent/schema/loginfailureevent.go)
- [IP-ban schema](./internal/ent/schema/ipban.go)

### Browser implementation and templates

- [API template](./web/templates/api.html)
- [Share template](./web/templates/share.html)
- [upload client](./web/static/js/upload.js)
- [crypto client](./web/static/js/crypto.js)
- [Share client](./web/static/js/share.js)
- [archive client](./web/static/js/archive.js)
- [ZIP helpers](./web/static/js/zip.js)
- [robots file](./web/robots.txt)

### Executable contract tests

- [API route tests](./internal/test/api_test.go)
- [HTTP tests](./internal/test/http_test.go)
- [upload tests](./internal/test/upload_test.go)
- [crypto tests](./web/test/crypto.test.mjs)
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

Security takes priority over speed, convenience, UI polish, and cleanup. Keep
tradeoffs explicit. This guide owns agent instructions and the detailed API
contract; the README owns build, configuration, and deployment procedures.

## Navigation

- [Architecture and ownership](#project-identity)
- [Transport, sessions, and proxy trust](#transport-proxy-and-session-model)
- [Credential derivation](#password-authorization-contract)
- [Example prerequisites and workspace](#example-prerequisites-and-workspace)
- [ZIP and encryption preparation](#client-side-payload-format)
- [Public and private uploads](#upload-endpoint)
- [Private archive discovery](#private-archive-list-endpoint)
- [Downloads, byte ranges, and decryption](#download-endpoint)
- [HTTP errors and safe retries](#http-errors-and-retry-policy)
- [Persistent download bans](#failed-download-persistence-and-bans)
- [Persistence and cleanup](#data-persistence)
- [Logging and audit boundaries](#audit-and-safe-observability)
- [AI coding-agent instructions](#modifying-the-project-safely)

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
| Download protection | Persist failed attempts, create and expire IP bans, and produce `Retry-After` | [download protection](./internal/http/download.go) |
| Authentication | Validate canonical password hashes, store bcrypt verifiers, hash private lookup keys, and protect admin login | [authentication module](./internal/auth/auth.go) |
| Share model and queries | Define active/expired semantics and isolate Ent query behavior | [Share model](./internal/share/model.go), [Share store](./internal/share/store.go) |
| Blob storage | Stage, hash, size-limit, atomically commit, count, remove, purge, and reconcile files | [blob store](./internal/storage/blobstore.go), [integrity service](./internal/storage/integrity.go) |
| Database | Open SQLite through Ent, run schema creation, and serialize DB access through one connection | [database bootstrap](./internal/db/db.go), [Share schema](./internal/ent/schema/share.go) |
| Browser client | ZIP files, derive authorization hashes, encrypt/decrypt ciphertext, invoke the API, and render archive entries | [upload client](./web/static/js/upload.js), [crypto client](./web/static/js/crypto.js), [Share client](./web/static/js/share.js), [archive client](./web/static/js/archive.js), [ZIP helpers](./web/static/js/zip.js) |
| Public documentation UI | Render the active public archive list and concise API contract | [API template](./web/templates/api.html) |

The module and dependency versions are declared in [the Go module](./go.mod).
The container build is described by [the Dockerfile](./Dockerfile), and the
container starts through [the entrypoint](./entrypoint.sh). For process and
HTTPS deployment commands, use the [README deployment sections](./README.md#quick-start),
[Compose definition](./deploy/docker-compose.yaml),
[Caddy configuration](./deploy/Caddyfile), and
[interactive deployment script](./deploy/deploy.sh).

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
It is not a public JSON lookup endpoint. Listing queries are
defined by the [Share store](./internal/share/store.go), while the public page is
rendered by the [API template](./web/templates/api.html). The crawler policy is
the [robots file](./web/robots.txt).

The API operation routes are registered only for `POST`; another method receives
`405 Method Not Allowed`. Router-level unknown paths return `404`, but middleware
can reject an unsafe request before routing. Do not assume every path beginning
with `/api/` is sessionless: only the routes recognized by `isAPIPath` and
`isSessionlessPath` receive that treatment.

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

1. the first `X-Forwarded-For` entry, if it is a valid IP and the proxy is trusted;
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

## Body-only JSON and form envelopes

Private listing and download authorization share `requestBodyValue` in the
[HTTP handlers](./internal/http/handlers.go). Their body contract is defined
here once; the endpoint sections below provide complete curl examples.

| Endpoint | Only accepted property/form key | Maximum decoded UTF-8 bytes | Invalid-body outcome |
| --- | --- | --- | --- |
| `POST /api/v0/list` | `private_key` | 64 KiB | `400`, no download failure event |
| `POST /api/v0/download/{uuid}` | `password_hash` | 4 KiB | Download denial, including a persisted failure event |

- Accept `application/json` or `application/x-www-form-urlencoded`; parameters
  such as `charset=utf-8` are parsed, but unrelated/vendor media types are not
  aliases. Multipart, `text/plain`, and missing content types are rejected.
- JSON must be one object with exactly the named property and one non-empty
  string value. Reject unknown properties, duplicate properties, arrays,
  non-string values, malformed JSON, and a second JSON value after the object.
- Form data must have exactly one key and exactly one non-empty value. Reject
  unknown keys and duplicate values. Percent-encode values with a real encoder.
- Non-empty URL query data is rejected, including unrelated query keys.
  Sensitive values belong in the body, not URLs, headers, or request diagnostics.
- The parser bounds JSON at `maximum * 6 + 256` bytes and encoded form data at
  `maximum * 3 + 256`, then checks the decoded value length. Non-upload API
  request bodies also have a 1 MiB HTTP limit.

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
decrypt, while transmitting only `password_hash`. Canonical validation checks
the digest encoding, not how a client derived it; the domain and NFC rules above
are required for interoperating with the first-party client.

### Distinct credentials and identifiers

| Value | Purpose | Crosses the HTTP boundary? | Persistence |
| --- | --- | --- | --- |
| Archive password | Local PBKDF2 encryption/decryption and authorization-hash derivation | Never | Only a caller-controlled secure reference; not logs, source, or agent memory |
| `password_hash` | Authorize encrypted-payload upload/download | Body only | Server stores a bcrypt verifier of the decoded digest, not the supplied value |
| Private lookup key | Discover unlisted Shares | Upload/list body only | Server stores its secret-scoped HMAC, not the raw key |
| Share UUID | Address a Share and its payload endpoint | Route and response | Share primary key; it is not a password or decryption key |
| `cipher_meta` | Supply public KDF/cipher parameters for local decryption | Upload body and upload/list responses | Stored alongside ciphertext; must be retained per Share |

Private means unlisted, not inaccessible by UUID. A direct `/s/{uuid}` link may
render a private Share, but payload access still requires its authorization
hash. The private lookup key is not an AES password, does not authorize payload
bytes, and is not an alternative credential for the download endpoint.

## Example prerequisites and workspace

The shell examples run in Bash from the repository root. They use curl 7.84+
for response-header write-outs, jq for JSON construction/validation, and Bun
for the existing client crypto module. ZIP preparation also uses `zip` and
`unzip`. Run against a small, disposable integration fixture, not unknown
production archives. Start an HTTPS deployment using the
[README](./README.md#run-docker-compose-with-self-signed-https) first.

For a new Share, follow workspace setup, ZIP/encryption preparation, one upload
recipe, optional private lookup, target resolution/download, and decryption.
The form and range blocks are optional alternative checks, not extra required
operations. API sections are references; private lookup needs an existing Share
if the optional ID-selection check is to find a result.

Initialize this context once. Replace the three input paths with caller-provided
local paths; do not put actual credential values into the commands or this guide.
Secret source files must contain exactly the intended UTF-8 strings: a trailing
newline is part of a password or private key, not automatically stripped.

```bash
set -euo pipefail
set +x
umask 077

BASE_URL='https://localhost:8443'
CA_CERT='data/tls/server.crt'
SOURCE_DIR='<absolute path to a small trusted fixture directory>'
PASSWORD_INPUT='<protected local archive-password source>'
PRIVATE_KEY_FILE='<protected local private-lookup-key source>'

# Keep generated test artifacts out of the repository and other users' reach.
: "${TMPDIR:?Set TMPDIR to a private local scratch directory}"
WORK_DIR="$(mktemp -d "${TMPDIR}/shareserver-api.XXXXXXXX")"
PLAIN_ZIP="${WORK_DIR}/source.zip"
PAYLOAD_FILE="${WORK_DIR}/archive.encrypted"
CIPHER_META_FILE="${WORK_DIR}/cipher-meta.json"
PASSWORD_HASH_FILE="${WORK_DIR}/password-hash"
export PLAIN_ZIP PAYLOAD_FILE CIPHER_META_FILE PASSWORD_HASH_FILE
```

These examples use the local self-signed certificate with `--cacert`. For an
origin certified by a trusted public CA, omit that option and use the system
trust store. Never substitute `--insecure`. `--proto '=https'` rejects a plain
HTTP origin; `--max-redirs 0` prevents the `-L` in `-fsSL` from forwarding a
credential-bearing body to another location. Do not add cookies, CSRF headers,
proxy-forwarding headers, HTTP traces, or automatic POST retries.

The shown 10-second connection and 300-second total timeouts are example client
choices, not server policy. Adjust for the actual bounded payload and deployment.
The examples read sensitive multipart values from files and JSON bodies from
stdin/files so credentials are not placed in curl's process arguments. Files
under the private workspace still require secret-safe handling and cleanup.

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

For the explicitly trusted, bounded fixture from the example context:

```bash
test ! -e "$PLAIN_ZIP"
(
  cd "$SOURCE_DIR"
  zip -q -r "$PLAIN_ZIP" .
)
unzip -tq "$PLAIN_ZIP"
```

This is a fixture constructor, not a safe crawler of arbitrary directories.
Choose its inputs deliberately; exclude secrets and unsafe symlinks.

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

### Executable encryption and hash preparation

Curl transports bytes; it does not create ZIPs or perform this AES-GCM format.
Use the repository's existing client implementation rather than inventing an
OpenSSL `enc` command: that command does not implement this authenticated wire
format. This Bun adapter imports `encryptBlob`, `decryptBlob`, and
`downloadPasswordHash` from the [crypto client](./web/static/js/crypto.js).
It performs a local authenticated round-trip before writing the upload inputs.

```bash
bun --eval '
import {
  encryptBlob,
  decryptBlob,
  downloadPasswordHash,
} from "./web/static/js/crypto.js";

const password = await Bun.stdin.text();
const plaintext = new Uint8Array(
  await Bun.file(process.env.PLAIN_ZIP).arrayBuffer(),
);
const encrypted = await encryptBlob(new Blob([plaintext]), password);
const ciphertext = new Uint8Array(await encrypted.blob.arrayBuffer());
const opened = await decryptBlob(ciphertext, password, encrypted.meta, {
  returnBytes: true,
});
if (!Buffer.from(opened).equals(Buffer.from(plaintext))) {
  throw new Error("local encrypted ZIP round-trip failed");
}

await Bun.write(process.env.PAYLOAD_FILE, ciphertext);
await Bun.write(process.env.CIPHER_META_FILE, JSON.stringify(encrypted.meta));
await Bun.write(
  process.env.PASSWORD_HASH_FILE,
  await downloadPasswordHash(password),
);
' < "$PASSWORD_INPUT"
```

The password is read from stdin, never sent to the server, and never printed.
The hash file has no trailing newline; its complete contents are the canonical
authorization value. Each invocation generates new salt/nonce values. Invoke
this block again before each new upload, including when switching from the
public example to the private example below. This adapter reads the fixture
into memory; apply resource limits before using it with large or untrusted data.

## Private archive-list endpoint

### Private-list request

```text
POST /api/v0/list
```

The lookup endpoint requires verified HTTPS and accepts exactly one non-empty
`private_key` string in either JSON or URL-encoded form data. It is sessionless,
does not require CSRF state, and does not create a browser session.

### JSON lookup with curl

Construct JSON with jq so quotes, Unicode, and newlines in the key are encoded
correctly. The key stays out of the URL and process arguments.

```bash
LIST_JSON_RESPONSE="${WORK_DIR}/private-list-json.json"
jq -n --rawfile private_key "$PRIVATE_KEY_FILE" \
  '{private_key: $private_key}' |
  curl -fsSL \
    --proto '=https' \
    --max-redirs 0 \
    --connect-timeout 10 \
    --max-time 300 \
    --cacert "$CA_CERT" \
    --request POST \
    --header 'Accept: application/json' \
    --header 'Content-Type: application/json' \
    --data-binary @- \
    --dump-header "${WORK_DIR}/private-list-json.headers" \
    --output "$LIST_JSON_RESPONSE" \
    --write-out 'private_list_json_status=%{http_code}\n' \
    "${BASE_URL}/api/v0/list"

jq -e '(.archives | type == "array")' "$LIST_JSON_RESPONSE" > /dev/null
```

### URL-encoded lookup with curl

Use curl's file-input URL encoder; never interpolate the raw key into a form
string. This alternate request has the same response contract as JSON.

```bash
LIST_FORM_RESPONSE="${WORK_DIR}/private-list-form.json"
curl -fsSL \
  --proto '=https' \
  --max-redirs 0 \
  --connect-timeout 10 \
  --max-time 300 \
  --cacert "$CA_CERT" \
  --request POST \
  --header 'Accept: application/json' \
  --data-urlencode "private_key@${PRIVATE_KEY_FILE}" \
  --dump-header "${WORK_DIR}/private-list-form.headers" \
  --output "$LIST_FORM_RESPONSE" \
  --write-out 'private_list_form_status=%{http_code}\n' \
  "${BASE_URL}/api/v0/list"

jq -e '(.archives | type == "array")' "$LIST_FORM_RESPONSE" > /dev/null
```

The [shared body contract](#body-only-json-and-form-envelopes) defines accepted
encodings, strict cardinality, query rejection, and size bounds. A private key
belongs only in the request body: URLs commonly enter browser history,
reverse-proxy logs, analytics, and referrer data.

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

### Private-list success response

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

The endpoint has no wrong-key error and no pagination token. An empty list is a
normal terminal result, not `401`. The download failure-ban policy does not
apply to this endpoint; that is not permission to enumerate lookup keys.
For request errors, use the [shared HTTP error policy](#http-errors-and-retry-policy).

### Select an uploaded private Share without scraping HTML

After the [private upload example](#private-upload-with-curl) succeeds, its
`UPLOAD_RECEIPT` supplies the stable ID to select. Titles are not unique. The
following optional check demonstrates discovery and preserves the matching
entry without printing the key or complete response:

```bash
SHARE_ID="$(jq -er '.id' "$UPLOAD_RECEIPT")"
jq -e --arg id "$SHARE_ID" \
  '.archives[] | select(.id == $id)' \
  "$LIST_JSON_RESPONSE" > "${WORK_DIR}/selected-private-share.json"

jq -e '
  .encryption == "client" and
  (.cipher_meta | type == "object") and
  .download_url == ("/api/v0/download/" + .id)
' "${WORK_DIR}/selected-private-share.json" > /dev/null
```

When choosing an existing rather than newly uploaded Share, select an intended
returned UUID explicitly and retain that entry's metadata. Do not assume the
first array element is the desired Share, and do not invent an ID for an empty
result.

## Upload endpoint

### Upload request

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
| `password_hash` | Yes | Canonical authorization value described above; maximum input 4 KiB; plaintext `password` is an unknown field and is rejected |
| `encrypted` | Yes | Must be `1` or `true`; any other value is rejected |
| `cipher_meta` | Yes | JSON metadata for PBKDF2-SHA-384 and AES-256-GCM; maximum 4 KiB |
| `zip_manifest` | No | Maximum 64 KiB; accepted for compatibility but stored as `[]`, so it cannot recover filenames or MIME metadata |
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

### Public upload with curl

Run [client preparation](#executable-encryption-and-hash-preparation) first.
The literal public visibility and six-hour expiry are actual valid values,
not slash-delimited alternatives. Curl generates the multipart boundary.
`--form-string` protects literal text from curl's special form syntax;
`field=<file` loads field contents without sending a file part; `blob=@file`
sends the final file part with a filename.

```bash
UPLOAD_RECEIPT="${WORK_DIR}/public-upload.json"
UPLOAD_STATUS="$(
  curl -fsSL \
    --proto '=https' \
    --max-redirs 0 \
    --connect-timeout 10 \
    --max-time 300 \
    --cacert "$CA_CERT" \
    --request POST \
    --header 'Accept: application/json' \
    --form-string 'title=public integration fixture' \
    --form-string 'visibility=public' \
    --form "password_hash=<${PASSWORD_HASH_FILE}" \
    --form-string 'encrypted=1' \
    --form "cipher_meta=<${CIPHER_META_FILE}" \
    --form-string 'expiry_hours=6' \
    --form-string 'zip_manifest=[]' \
    --form "blob=@${PAYLOAD_FILE};type=application/octet-stream" \
    --dump-header "${WORK_DIR}/public-upload.headers" \
    --output "${WORK_DIR}/upload-response.part" \
    --write-out '%{http_code}' \
    "${BASE_URL}/api/v0/upload"
)"
test "$UPLOAD_STATUS" = 201

jq -e '
  (.id | test("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")) and
  .url == ("/s/" + .id) and
  .download_url == ("/api/v0/download/" + .id) and
  .encryption == "client" and
  (.cipher_meta | type == "object") and
  (.size | type == "number") and .size >= 16 and
  (.expires_at | type == "string")
' "${WORK_DIR}/upload-response.part" > /dev/null
mv "${WORK_DIR}/upload-response.part" "$UPLOAD_RECEIPT"
printf 'public_upload_status=%s\n' "$UPLOAD_STATUS"
```

Keep `blob` last even when adding optional metadata. Libraries must preserve
multipart append order; do not serialize parts from an unordered map.
There is no browser-session or plaintext-password field in this request.

### Private upload with curl

Invoke [client preparation](#executable-encryption-and-hash-preparation) again
to get a fresh salt/nonce and corresponding ciphertext, even if the ZIP and
password are unchanged. The private key is separate from that password and is
read from its protected source file.

```bash
UPLOAD_RECEIPT="${WORK_DIR}/private-upload.json"
UPLOAD_STATUS="$(
  curl -fsSL \
    --proto '=https' \
    --max-redirs 0 \
    --connect-timeout 10 \
    --max-time 300 \
    --cacert "$CA_CERT" \
    --request POST \
    --header 'Accept: application/json' \
    --form-string 'title=private integration fixture' \
    --form-string 'visibility=private' \
    --form "private_key=<${PRIVATE_KEY_FILE}" \
    --form "password_hash=<${PASSWORD_HASH_FILE}" \
    --form-string 'encrypted=1' \
    --form "cipher_meta=<${CIPHER_META_FILE}" \
    --form-string 'expiry_hours=6' \
    --form-string 'zip_manifest=[]' \
    --form "blob=@${PAYLOAD_FILE};type=application/octet-stream" \
    --dump-header "${WORK_DIR}/private-upload.headers" \
    --output "${WORK_DIR}/upload-response.part" \
    --write-out '%{http_code}' \
    "${BASE_URL}/api/v0/upload"
)"
test "$UPLOAD_STATUS" = 201

jq -e '
  (.id | test("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")) and
  .url == ("/s/" + .id) and
  .download_url == ("/api/v0/download/" + .id) and
  .encryption == "client" and
  (.cipher_meta | type == "object") and
  (.size | type == "number") and .size >= 16 and
  (.expires_at | type == "string")
' "${WORK_DIR}/upload-response.part" > /dev/null
mv "${WORK_DIR}/upload-response.part" "$UPLOAD_RECEIPT"
printf 'private_upload_status=%s\n' "$UPLOAD_STATUS"
```

Each recipe keeps a separate receipt; retain the matching metadata for each
Share, not just the most recently generated local metadata file. A local
validation or filesystem failure after `201` does not undo the server upload.
Use the [HTTP error/retry policy](#http-errors-and-retry-policy) rather than
rerunning the command blindly.

### Server processing sequence

The upload path is deliberately staged:

After the transport/session checks and ordered multipart parsing described
above, the owning upload module performs these storage transitions:

1. Validate metadata before writing bytes.
2. Purge unprotected legacy Shares and expired Shares before capacity
   calculation.
3. Reserve at most the configured per-upload maximum and remaining global
   capacity. Reservations prevent concurrent uploads from overcommitting space.
4. Decode the authorization hash and compute a salted bcrypt verifier. Verifier
   work is serialized to bound anonymous CPU usage.
5. Stream the ciphertext into `.staging/<uuid>.tmp` with mode `0600`, enforce
   the reserved byte limit, and compute ciphertext SHA-256. Slow uploads do not
   hold the capacity or integrity locks while streaming.
6. Recheck actual storage usage under the capacity and integrity locks,
   atomically rename to `<uuid>.blob`, and insert the Share row. Row insertion
   failure removes the committed blob; earlier failures remove staged bytes.
7. Write a safe audit event and return `201 Created` JSON.

The orchestration is in the [upload module](./internal/upload/upload.go), staging
and atomic rename are in the [blob store](./internal/storage/blobstore.go), and
metadata insertion is in the [Share store](./internal/share/store.go).

### Upload success response

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

Response JSON above is a schema illustration with placeholders, not a captured
server result. Error statuses and upload ambiguity are defined once in the
[HTTP error/retry policy](#http-errors-and-retry-policy).

## Download endpoint

### Download request

```text
POST /api/v0/download/{uuid}
```

`{uuid}` must have canonical UUID structure. Invalid, unknown, missing,
unprotected, and wrong-password targets deliberately collapse into the same
authorization failure path.

Use the [shared body contract](#body-only-json-and-form-envelopes) with only
`password_hash`. Parsing denials count toward download bans just like a wrong
credential; they are not harmless validation probes.

### Resolve the target and construct the request body

Use either a successful upload receipt or the selected private-list entry.
The uploaded fixture already has a hash file. For an existing Share, derive
only its authorization hash from the matching local password; do not rerun
encryption merely to download it:

```bash
bun --eval '
import { downloadPasswordHash } from "./web/static/js/crypto.js";
process.stdout.write(await downloadPasswordHash(await Bun.stdin.text()));
' < "$PASSWORD_INPUT" > "$PASSWORD_HASH_FILE"
```

To use a discovered existing private Share, set `UPLOAD_RECEIPT` to the selected
private-entry JSON from the lookup recipe; it contains the required ID, path,
size, and cipher metadata. For a newly uploaded Share, keep its original receipt.

```bash
SHARE_ID="$(jq -er '.id' "$UPLOAD_RECEIPT")"
DOWNLOAD_PATH="$(jq -er '.download_url' "$UPLOAD_RECEIPT")"
test "$DOWNLOAD_PATH" = "/api/v0/download/${SHARE_ID}"
DOWNLOAD_REQUEST="${WORK_DIR}/download-request.json"
DOWNLOADED_PAYLOAD="${WORK_DIR}/downloaded.encrypted"

jq -n --rawfile password_hash "$PASSWORD_HASH_FILE" \
  '{password_hash: $password_hash}' > "$DOWNLOAD_REQUEST"
```

Validate the returned path rather than blindly requesting an absolute URL from
response data. The credentials must stay on the same verified HTTPS origin.

### JSON download with curl

The output remains ciphertext. Check both the response status and byte count
before promoting a completed local file; do not save an error page as an archive.

```bash
DOWNLOAD_STATUS="$(
  curl -fsSL \
    --proto '=https' \
    --max-redirs 0 \
    --connect-timeout 10 \
    --max-time 300 \
    --cacert "$CA_CERT" \
    --request POST \
    --header 'Accept: application/octet-stream' \
    --header 'Content-Type: application/json' \
    --data-binary "@${DOWNLOAD_REQUEST}" \
    --dump-header "${WORK_DIR}/download-json.headers" \
    --output "${WORK_DIR}/download.part" \
    --write-out '%{http_code}' \
    "${BASE_URL}${DOWNLOAD_PATH}"
)"
test "$DOWNLOAD_STATUS" = 200
test "$(wc -c < "${WORK_DIR}/download.part")" -eq \
  "$(jq -er '.size' "$UPLOAD_RECEIPT")"
mv "${WORK_DIR}/download.part" "$DOWNLOADED_PAYLOAD"
printf 'download_json_status=%s\n' "$DOWNLOAD_STATUS"
```

### URL-encoded download with curl

Use the same target with the alternate accepted body format. Curl URL-encodes
the complete hash read from the file, including `+`, `/`, and `=` correctly.

```bash
DOWNLOAD_STATUS="$(
  curl -fsSL \
    --proto '=https' \
    --max-redirs 0 \
    --connect-timeout 10 \
    --max-time 300 \
    --cacert "$CA_CERT" \
    --request POST \
    --header 'Accept: application/octet-stream' \
    --data-urlencode "password_hash@${PASSWORD_HASH_FILE}" \
    --dump-header "${WORK_DIR}/download-form.headers" \
    --output "${WORK_DIR}/download-form.part" \
    --write-out '%{http_code}' \
    "${BASE_URL}${DOWNLOAD_PATH}"
)"
test "$DOWNLOAD_STATUS" = 200
cmp "$DOWNLOADED_PAYLOAD" "${WORK_DIR}/download-form.part"
printf 'download_form_status=%s\n' "$DOWNLOAD_STATUS"
```

The `cmp` check is useful in this fixture exercise: both encodings must return
identical ciphertext, not independently transformed or re-encrypted bytes.

### Byte ranges and exact reassembly with curl

Every range request is still an authenticated POST. For this small fixture,
split the retained ciphertext size into two adjacent, non-overlapping ranges.
Both responses must be `206`; a server that ignores `Range` and returns `200`
must not be treated as a valid resumed segment.
Send an explicit `Range` header for authenticated POSTs rather than relying on
curl's `--range` transfer option.

```bash
TOTAL_BYTES="$(jq -er '.size | select(. >= 16)' "$UPLOAD_RECEIPT")"
SPLIT_AT="$((TOTAL_BYTES / 2))"
FIRST_END="$((SPLIT_AT - 1))"

RANGE_STATUS="$(
  curl -fsSL \
    --proto '=https' \
    --max-redirs 0 \
    --connect-timeout 10 \
    --max-time 300 \
    --cacert "$CA_CERT" \
    --request POST \
    --header 'Content-Type: application/json' \
    --header "Range: bytes=0-${FIRST_END}" \
    --data-binary "@${DOWNLOAD_REQUEST}" \
    --dump-header "${WORK_DIR}/range-first.headers" \
    --output "${WORK_DIR}/range-first.part" \
    --write-out '%{http_code}' \
    "${BASE_URL}${DOWNLOAD_PATH}"
)"
test "$RANGE_STATUS" = 206
test "$(wc -c < "${WORK_DIR}/range-first.part")" -eq "$SPLIT_AT"
printf 'first_range_status=%s\n' "$RANGE_STATUS"

RANGE_STATUS="$(
  curl -fsSL \
    --proto '=https' \
    --max-redirs 0 \
    --connect-timeout 10 \
    --max-time 300 \
    --cacert "$CA_CERT" \
    --request POST \
    --header 'Content-Type: application/json' \
    --header "Range: bytes=${SPLIT_AT}-" \
    --data-binary "@${DOWNLOAD_REQUEST}" \
    --dump-header "${WORK_DIR}/range-last.headers" \
    --output "${WORK_DIR}/range-last.part" \
    --write-out '%{http_code}' \
    "${BASE_URL}${DOWNLOAD_PATH}"
)"
test "$RANGE_STATUS" = 206
test "$(wc -c < "${WORK_DIR}/range-last.part")" -eq \
  "$((TOTAL_BYTES - SPLIT_AT))"

cp "${WORK_DIR}/range-first.part" "${WORK_DIR}/reassembled.encrypted"
dd if="${WORK_DIR}/range-last.part" \
  of="${WORK_DIR}/reassembled.encrypted" \
  bs=1 seek="$SPLIT_AT" conv=notrunc
cmp "$DOWNLOADED_PAYLOAD" "${WORK_DIR}/reassembled.encrypted"
printf 'last_range_status=%s\n' "$RANGE_STATUS"
```

Byte-sized `dd` makes the offset explicit and portable for a small test fixture;
use a buffered byte-aware reassembler for large payloads. In production, also
validate `Content-Range` against the requested offset and retained total length.
AES-GCM authentication must cover the complete reassembled ciphertext; never
decrypt a single range as if it were an independently authenticated frame.

### Server decision sequence

The server handles a download in this order:

1. Verify HTTPS.
2. Resolve the trusted client IP.
3. Look up the namespaced persistent ban for that IP. Delete it if it expired.
4. Parse the body using one accepted content type.
5. Validate the UUID, load the Share and blob path, require an encrypted and
   protected Share, validate the canonical hash, and compare its bcrypt verifier.
6. For body parsing or authorization denials, persist one failed attempt.
   The eleventh attempt in the strict rolling window creates the ban immediately.
7. After successful authorization, classify expiry. An expired Share returns
   `410 Gone` and does not add a failed-password event.
8. Set download headers and delegate file streaming to Go's file server.

The handler lives in [HTTP handlers](./internal/http/handlers.go), and persistent
failure/ban policy lives in [download protection](./internal/http/download.go).

### Download success response

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

The download POST handler adds no artificial response delay. Success, denial,
expiry, insecure-transport, and active-ban responses return after their required
authorization and persistence checks; network transfer time remains variable.

The text `download denied` intentionally does not reveal which authorization
condition failed. Response times are not intentionally equalized and may vary
with password verification, storage access, and network conditions.
Use the [shared HTTP error policy](#http-errors-and-retry-policy) for terminal
errors, range correction, and bans.

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

Use the [payload format](#client-side-payload-format) in reverse with the exact
metadata retained for this Share. The download response does not supply it.
Validate metadata before expensive KDF/buffer work, authenticate the complete
ciphertext, and only then treat plaintext as ZIP. Authentication failure is one
generic wrong-password-or-corruption result, not permission to try guesses.

This example accepts the current unframed format and calls the existing client
decoder. It does not enable legacy framed formats as a new upload option.

```bash
DECRYPTED_ZIP="${WORK_DIR}/decrypted.zip"
export UPLOAD_RECEIPT DOWNLOADED_PAYLOAD DECRYPTED_ZIP

bun --eval '
import { cipherIterations, decryptBlob } from "./web/static/js/crypto.js";

const receipt = await Bun.file(process.env.UPLOAD_RECEIPT).json();
const meta = receipt.cipher_meta;
if (!meta || meta.kdf !== "PBKDF2-SHA-384" || meta.cipher !== "AES-256-GCM") {
  throw new Error("unsupported encryption metadata");
}
cipherIterations(meta);
const salt = Buffer.from(meta.salt, "base64");
const nonce = Buffer.from(meta.nonce, "base64");
if (salt.length !== 16 || nonce.length !== 12 ||
    salt.toString("base64") !== meta.salt ||
    nonce.toString("base64") !== meta.nonce) {
  throw new Error("invalid salt or nonce metadata");
}

const ciphertext = new Uint8Array(
  await Bun.file(process.env.DOWNLOADED_PAYLOAD).arrayBuffer(),
);
if (ciphertext.byteLength !== receipt.size) {
  throw new Error("downloaded ciphertext size does not match receipt");
}
const password = await Bun.stdin.text();
const plaintext = await decryptBlob(ciphertext, password, meta, {
  returnBytes: true,
});
await Bun.write(process.env.DECRYPTED_ZIP, plaintext);
' < "$PASSWORD_INPUT"

# These checks are for the small known fixture, not arbitrary remote ZIPs.
unzip -tq "$DECRYPTED_ZIP"
cmp "$PLAIN_ZIP" "$DECRYPTED_ZIP"
```

For an existing remote Share, omit the final `cmp` unless an original ZIP is
available. Treat unknown decrypted archives as untrusted: cap entry counts,
decompressed size and compression ratios; reject traversal, absolute paths,
unsafe links and collisions; prevent overwrites; never execute extracted files
automatically. Do not blindly extract or ZIP-test an unbounded archive.

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

## HTTP errors and retry policy

Parse the HTTP status before deciding whether the body is JSON, ciphertext, or
an error. Errors generated directly by application handlers are plain text,
normally newline-terminated; there is no JSON error envelope. A file-serving
precondition failure may have an empty body. Curl `-f` returns exit code 22 for
HTTP errors and suppresses their bodies. Inspect captured status/headers, not a supposedly
complete JSON file left by a failed command.

| Status | Endpoint or scope | Meaning | Required next action |
| --- | --- | --- | --- |
| `400` | Upload | Malformed/duplicate/unknown/trailing multipart fields; missing blob/filename/private key/hash; invalid hash, encryption metadata/mode; ciphertext shorter than 16 bytes | Correct the request without an unchanged retry |
| `400` | Private list | Violated the strict body, encoding, query, cardinality, or size contract | Fix the envelope; do not enumerate keys |
| `401` | Download | Malformed body, missing/invalid/incorrect hash, invalid/unknown UUID, missing blob during precheck, or unprotected Share | Check local inputs once, then stop; denial persists a failure event |
| `403` | Upload | Supplied CSRF header mismatched the attached session | Omit browser-only auth state or use a valid existing session/token pair |
| `404` | Routing; download race | Unknown routed path, or an authorized blob disappeared after its precheck | Correct routing or report an integrity race; do not repeatedly submit credentials |
| `405` | Known operation route | Method was not `POST` | Correct the method |
| `410` | Download | Credential matched but the Share expired | Treat expiry as terminal |
| `412` | Authorized download | A file-serving precondition such as `If-Unmodified-Since` failed | Correct/remove the conditional header; it does not replace authorization |
| `413` | Upload | Metadata, request body, or ciphertext exceeded a size bound | Reduce input; splitting requests creates separate Shares, not upload chunks |
| `415` | Upload | Body is not multipart/form-data | Use ordered multipart with curl's generated boundary |
| `416` | Download | Range lies outside ciphertext bounds | Correct/remove `Range`; do not splice an error body into a download |
| `426` | All operation endpoints | Direct TLS or trusted forwarded HTTPS was absent | Correct transport/proxy deployment; never bypass verification |
| `429` | Download | Persistent IP ban or fail-closed ban/failure-storage error | Honor positive `Retry-After` and stop all parallel attempts from that IP |
| `500` | Upload | Verifier, staging, database, or filesystem storage failed | Require an explicit bounded-retry/duplicate-risk decision |
| `507` | Upload | Remaining global storage could not admit the payload | Stop and notify the operator; unchanged retries cannot create capacity |

An upload has no idempotency key and no lookup by client request ID, title, or
checksum. A timeout or connection failure after sending the body may mean the
Share was stored but its response was lost. Do not add `--retry`, rerun the
upload automatically, or assume that curl failure proves rollback. Preserve
any complete `201` receipt; decide explicitly whether duplicate creation is
acceptable before another upload attempt.

Download POST is read-only for the Share, but denials mutate failure/ban state.
Do not probe passwords or run invalid-body cases on a production client IP.
List failures have no password-ban counter, but key enumeration is still
prohibited. Connection and total timeouts must account for normal verification,
storage, and transfer latency, not an artificial wait floor.

### Capture a status and `Retry-After` without exposing credentials

This recipe uses the selected download target and protected JSON body. It
discards ciphertext intentionally because it demonstrates response handling,
not a second archive acquisition. It does not sleep/retry or print headers that
could contain cookies. Curl's exit code and HTTP status are different values.

```bash
CURL_EXIT=0
if RESULT="$(
  curl -fsSL \
    --proto '=https' \
    --max-redirs 0 \
    --connect-timeout 10 \
    --max-time 300 \
    --cacert "$CA_CERT" \
    --request POST \
    --header 'Content-Type: application/json' \
    --data-binary "@${DOWNLOAD_REQUEST}" \
    --output /dev/null \
    --write-out '%{http_code} %header{retry-after}' \
    "${BASE_URL}${DOWNLOAD_PATH}"
)"; then
  CURL_EXIT=0
else
  CURL_EXIT=$?
fi
read -r HTTP_STATUS RETRY_AFTER <<< "$RESULT"

case "$HTTP_STATUS" in
  200|206)
    printf 'download_status=%s\n' "$HTTP_STATUS"
    ;;
  429)
    if [[ ! "$RETRY_AFTER" =~ ^[1-9][0-9]*$ ]]; then
      printf 'Invalid ban response; stop and inspect deployment.\n' >&2
      exit 1
    fi
    printf 'Pause all requests from this IP for at least %s seconds.\n' \
      "$RETRY_AFTER"
    ;;
  401|410)
    printf 'Terminal download response: HTTP %s; do not retry unchanged.\n' \
      "$HTTP_STATUS" >&2
    ;;
  *)
    printf 'Request stopped: curl exit %s, HTTP %s.\n' \
      "$CURL_EXIT" "${HTTP_STATUS:-unknown}" >&2
    ;;
esac
```

### Invalid-request integration examples

Run these only against the disposable fixture. Each makes one deliberately
invalid request, expects curl exit 22, and asserts the specific HTTP status.
They do not send plaintext passwords or retain credentials in request literals.

For private listing, an extra JSON property violates strict cardinality:

```bash
jq -n --rawfile private_key "$PRIVATE_KEY_FILE" \
  '{private_key: $private_key, unexpected: true}' > "${WORK_DIR}/invalid-list.json"
CURL_EXIT=0
if HTTP_STATUS="$(
  curl -fsSL \
    --proto '=https' \
    --max-redirs 0 \
    --connect-timeout 10 \
    --max-time 300 \
    --cacert "$CA_CERT" \
    --request POST \
    --header 'Content-Type: application/json' \
    --data-binary "@${WORK_DIR}/invalid-list.json" \
    --output /dev/null \
    --write-out '%{http_code}' \
    "${BASE_URL}/api/v0/list"
)"; then
  printf 'Invalid list body was unexpectedly accepted.\n' >&2
  exit 1
else
  CURL_EXIT=$?
fi
test "$CURL_EXIT" -eq 22
test "$HTTP_STATUS" = 400
printf 'invalid_list_status=%s\n' "$HTTP_STATUS"
```

For upload, JSON is not an accepted substitute for multipart:

```bash
CURL_EXIT=0
if HTTP_STATUS="$(
  curl -fsSL \
    --proto '=https' \
    --max-redirs 0 \
    --connect-timeout 10 \
    --max-time 300 \
    --cacert "$CA_CERT" \
    --request POST \
    --header 'Content-Type: application/json' \
    --data-binary '{}' \
    --output /dev/null \
    --write-out '%{http_code}' \
    "${BASE_URL}/api/v0/upload"
)"; then
  printf 'Non-multipart upload was unexpectedly accepted.\n' >&2
  exit 1
else
  CURL_EXIT=$?
fi
test "$CURL_EXIT" -eq 22
test "$HTTP_STATUS" = 415
printf 'invalid_upload_status=%s\n' "$HTTP_STATUS"
```

For download, an unrecognized property follows the credential-denial path and
adds one persisted failure event, unlike the private-list `400` above:

```bash
CURL_EXIT=0
if HTTP_STATUS="$(
  curl -fsSL \
    --proto '=https' \
    --max-redirs 0 \
    --connect-timeout 10 \
    --max-time 300 \
    --cacert "$CA_CERT" \
    --request POST \
    --header 'Content-Type: application/json' \
    --data-binary '{"unexpected":true}' \
    --output /dev/null \
    --write-out '%{http_code}' \
    "${BASE_URL}${DOWNLOAD_PATH}"
)"; then
  printf 'Invalid download body was unexpectedly accepted.\n' >&2
  exit 1
else
  CURL_EXIT=$?
fi
test "$CURL_EXIT" -eq 22
test "$HTTP_STATUS" = 401
printf 'invalid_download_status=%s\n' "$HTTP_STATUS"
```

Do not run a shell loop to reach the ban threshold. Its restart, expiry,
namespacing, and boundary behavior are exercised by the
[API route tests](./internal/test/api_test.go), not by attacking a shared server.

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

The configured blob directory contains committed `<uuid>.blob` files and a
private `.staging` directory. The [upload processing sequence](#server-processing-sequence)
defines file creation, hashing, commit, and rollback; the
[blob store](./internal/storage/blobstore.go) implements them.

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
Legacy Shares without a download verifier cannot be migrated by recovering a
password: the server never retained their passwords. Startup removes those
unprotected blob/row pairs rather than exposing an unauthenticated payload path.
See the [cleanup scheduler](./internal/http/cleanup.go) and
[integrity service](./internal/storage/integrity.go).

## Audit and safe observability

Uploads and selected administrator actions produce audit rows containing actor,
IP, action, target, safe metadata, and timestamp. Audit write failure does not
fail the primary operation. The implementation is the
[audit logger](./internal/audit/audit.go) and
[audit-event schema](./internal/ent/schema/auditevent.go).
The following boundaries apply equally to server audit records, browser
diagnostics, agent reports, prompts, traces, tickets, and persisted memory.

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

### Architecture and ownership

- Keep one Go server, server-rendered pages, SQLite metadata, filesystem blobs,
  and locally served assets. Do not add a SPA, CDN, external service dependency,
  unnecessary framework layer, or server dependency on decrypted archive data.
- Respect the owning modules in the [architecture table](#project-identity).
  HTTP handlers parse, call the owning module, and choose a status/redirect;
  storage, authentication, query, and upload policy stay behind cohesive APIs.
- Use Ent for first-party database access. Raw SQL belongs only to generated or
  migration-owned code. Keep schema constraints, foreign keys, connection
  behavior, and migrations deliberate; choose SQLite safety over throughput.
- Migrate every affected caller in a clean cutover, remove obsolete helpers,
  and avoid aliases unless a real compatibility requirement justifies them.
- Prefer domain names and branches that reflect behavior. Module boundaries
  must reduce what a cold reader needs to know rather than add shallow plumbing.

### Security and durable state

- Review security end to end, not as a middleware checkbox: transport, cookies,
  CSP, sessions, CSRF, IP identity, bans, metadata disclosure, and storage must
  agree. Preserve generic anonymous errors on ambiguous access and cap/disk
  failures; do not leak private state or weaken one layer for convenience.
- Admin access must fail closed for unknown users, password-check errors,
  rotation errors, CSRF failures, and ban checks. No failed branch may create
  or preserve administrator access. Keep operational inspection/deletion useful
  without introducing an unsafe mutation path.
- Treat every Share as one durable blob/row pair throughout create, delete,
  purge, repair, and failure handling. Cleanup remains idempotent; filesystem
  errors must not be reported as successful metadata cleanup.
- Preserve the single active/expired rule across lists, detail, payload access,
  cleanup, admin inspection, and tests. Store durable timestamps in UTC and use
  configured `TZ` only for display or scheduled-maintenance boundaries.
- Keep public listing and private-key discovery distinct without turning the
  UUID route into a new private-key gate. Follow the
  [credential-role contract](#distinct-credentials-and-identifiers).
- Keep secrets in runtime configuration and examples symbolic. Invalid or
  missing required configuration must be obvious; never include real secrets
  in source, documentation, diagnostics, or persisted agent state.
- Change browser and server encryption/authorization contracts together.
  Stored metadata for opaque payloads must be intentionally safe to disclose;
  never require plaintext archive contents to validate server policy.

### Browser behavior and maintainability

- Preserve client ownership of ZIP construction, encryption, decryption,
  previews, and visible download names. Preserve uploaded basenames when
  possible, sanitize dangerous filename characters, and do not use internal
  UUID/blob names as normal end-user filenames.
- Keep templates legible, CSS sizing/layout classes shared, and native
  JavaScript modules small enough to inspect without extra build machinery.
  Preserve the current local design and make mobile behavior first-class,
  including Android downloads, sidebar state, touch navigation, and filenames.
- Preserve explanatory comments; update inaccurate comments rather than
  deleting context. Remove commented-out dead code instead of retaining it.
  Function/struct comments should explain purpose and non-obvious invariants
  concisely for human maintainers first and future agents second.
- Test real core flows and real failure branches, not mocks substituting for
  upload, store, route, cleanup, or browser-helper behavior. Go tests own server
  policy/persistence/session behavior; Bun tests own client-only text, crypto,
  progress, and download behavior.

### Validation references

The strongest executable references are:

- [API route tests](./internal/test/api_test.go) for plaintext rejection,
  multipart ordering, HTTPS, response latency, bans, and ban expiry;
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
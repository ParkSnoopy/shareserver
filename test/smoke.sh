#!/usr/bin/env bash
# Integration smoke tests for shareserver fixes.
# Uses curl to mimic real client behavior against a throwaway server.
#
# Covers:
#   A. Share store  — routes still serve after deepening (home/admin/delete)
#   B. Upload module — upload ok; private-no-key -> 400 + NO orphan blob
#   S1. Session fixation — sid rotates on admin login; old sid loses admin
#   S2. Secure cookie flag — X-Forwarded-Proto: https -> Secure; plain -> none
#   S3. Expired session row dropped on visitor return (no row reuse)
#   CSRF rejection, bad-uuid 404, admin gate redirect, lookup, blob fetch
#
# Client-only JS fixes (Progress reset) live in test/progress.mjs — not curlable.
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PORT=18090
ADDR=127.0.0.1:$PORT
BASE=http://$ADDR
TMP="$(mktemp -d -t shareserver-smoke-XXXXXX)"
DB="$TMP/smoke.db"
BLOB="$TMP/blobs"
BIN="$TMP/shareserver"
ADMIN_USER=admin
ADMIN_PASS=smoke-test-password
COOKIES="$TMP/cookies.txt"
PASS=0
FAIL=0
SERVER_PID=""

cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null
  wait "$SERVER_PID" 2>/dev/null
  rm -rf "$TMP"
}
trap cleanup EXIT

ok()   { echo "  ok: $1"; PASS=$((PASS+1)); }
err()  { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }

# assert_status <label> <expected> <actual>
assert_status() {
  if [ "$2" = "$3" ]; then ok "$1 (HTTP $3)"
  else err "$1: expected $2 got $3"; fi
}

# Build.
if ! command -v go >/dev/null 2>&1; then
  if [ -x /usr/local/go/bin/go ]; then export PATH=/usr/local/go/bin:$PATH
  else echo "go not found"; exit 2; fi
fi
echo "building..."
go build -o "$BIN" ./cmd/shareserver || { echo "build failed"; exit 2; }
mkdir -p "$BLOB"

# Start server with isolated config. Shell env overrides .env defaults.
echo "starting server on $ADDR..."
ENV=dev ADDR=$ADDR DB_PATH="$DB" BLOB_DIR="$BLOB" \
  ADMIN_USER="$ADMIN_USER" ADMIN_PASSWORD="$ADMIN_PASS" \
  APP_SECRET="$(openssl rand -hex 32 2>/dev/null || echo testsecret12345678901234567890123456789012)" \
  TRUST_PROXY_HEADERS=true \
  "$BIN" >"$TMP/server.log" 2>&1 &
SERVER_PID=$!
# wait for listen
for _ in $(seq 1 50); do
  curl -s -o /dev/null "$BASE/" 2>/dev/null && break
  sleep 0.1
done
if ! curl -s -o /dev/null "$BASE/"; then
  echo "server did not start"; cat "$TMP/server.log"; exit 2
fi

# fresh session jar
rm -f "$COOKIES"

echo "== route availability =="
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/")
assert_status "GET /" 200 "$s"
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/upload")
assert_status "GET /upload" 200 "$s"
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/admin/login")
assert_status "GET /admin/login" 200 "$s"

echo "== admin gate (unauth) =="
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/admin")
assert_status "GET /admin unauth -> redirect" 303 "$s"
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/admin/shares")
assert_status "GET /admin/shares unauth -> redirect" 303 "$s"

echo "== bad uuid / missing share =="
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/s/not-a-uuid")
assert_status "GET /s/not-a-uuid -> 404" 404 "$s"
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/s/00000000-0000-0000-0000-000000000000")
assert_status "GET /s/valid-uuid-missing -> 404" 404 "$s"
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/blob/00000000-0000-0000-0000-000000000000")
assert_status "GET /blob/valid-uuid-missing -> 404" 404 "$s"

echo "== CSRF rejection =="
# get a session + csrf first
curl -s -c "$COOKIES" "$BASE/upload" -o "$TMP/upload.html"
CSRF=$(grep -oE 'name="csrf" value="[^"]+"' "$TMP/upload.html" | sed 's/.*value="//;s/"//' | head -1)
[ -n "$CSRF" ] || err "no csrf token found"
# upload requires CSRF in a header so middleware does not parse a huge multipart
# body before upload limits apply.
s=$(curl -s -b "$COOKIES" -o /dev/null -w "%{http_code}" -X POST "$BASE/upload" \
  -H "X-CSRF-Token: wrong" -F "csrf=$CSRF" -F "title=t" -F "visibility=public" -F "blob=@/etc/hostname")
assert_status "POST /upload bad csrf -> 403" 403 "$s"
s=$(curl -s -b "$COOKIES" -o /dev/null -w "%{http_code}" -X POST "$BASE/upload" \
  -F "csrf=$CSRF" -F "title=t" -F "visibility=public" -F "blob=@/etc/hostname")
assert_status "POST /upload csrf field without header -> 403" 403 "$s"

echo "== upload public (B) =="
# tiny zip end-of-central-directory record (22 bytes) — server stores opaque
printf 'PK\x05\x06\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00' > "$TMP/tiny.zip"
BLOBS_BEFORE=$(ls "$BLOB" 2>/dev/null | wc -l)
resp=$(curl -s -b "$COOKIES" -X POST "$BASE/upload" \
  -H "X-CSRF-Token: $CSRF" -F "csrf=$CSRF" -F "title=smoke" -F "visibility=public" -F "expiry_hours=6" \
  -F "blob=@$TMP/tiny.zip")
echo "  upload resp: $resp"
ID=$(echo "$resp" | grep -oE '"id":"[^"]+"' | sed 's/"id":"//;s/"//')
if [ -n "$ID" ] && echo "$resp" | grep -q '"ok":true'; then ok "upload public returns id"
else err "upload public failed: $resp"; fi
BLOBS_AFTER=$(ls "$BLOB" 2>/dev/null | wc -l)
if [ "$BLOBS_AFTER" -gt "$BLOBS_BEFORE" ]; then ok "blob written for valid upload"
else err "no blob written for valid upload"; fi

echo "== share page + blob fetch after upload (A) =="
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/s/$ID")
assert_status "GET /s/{uploaded} -> 200" 200 "$s"
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/blob/$ID")
assert_status "GET /blob/{uploaded} -> 200" 200 "$s"
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/s/$ID/evil.html")
assert_status "GET /s/{uploaded}/file -> 404 (no same-origin file serving)" 404 "$s"

echo "== upload private without key -> 400 + NO orphan blob (B orphan fix) =="
BLOBS_BEFORE2=$(ls "$BLOB" 2>/dev/null | wc -l)
s=$(curl -s -b "$COOKIES" -o /dev/null -w "%{http_code}" -X POST "$BASE/upload" \
  -H "X-CSRF-Token: $CSRF" -F "csrf=$CSRF" -F "title=nokey" -F "visibility=private" -F "expiry_hours=6" \
  -F "blob=@$TMP/tiny.zip")
assert_status "POST /upload private no key -> 400" 400 "$s"
BLOBS_AFTER2=$(ls "$BLOB" 2>/dev/null | wc -l)
if [ "$BLOBS_AFTER2" = "$BLOBS_BEFORE2" ]; then ok "no orphan blob on private-no-key rejection"
else err "orphan blob left on private-no-key rejection: $BLOBS_BEFORE2 -> $BLOBS_AFTER2"; fi

echo "== upload private with key -> ok =="
resp=$(curl -s -b "$COOKIES" -X POST "$BASE/upload" \
  -H "X-CSRF-Token: $CSRF" -F "csrf=$CSRF" -F "title=pk" -F "visibility=private" -F "private_key=secretkey" -F "expiry_hours=6" \
  -F "blob=@$TMP/tiny.zip")
ID_PRIV=$(echo "$resp" | grep -oE '"id":"[^"]+"' | sed 's/"id":"//;s/"//')
if [ -n "$ID_PRIV" ] && echo "$resp" | grep -q '"ok":true'; then ok "upload private with key returns id"
else err "upload private with key failed: $resp"; fi

echo "== lookup with key (A) =="
s=$(curl -s -b "$COOKIES" -o /dev/null -w "%{http_code}" -X POST "$BASE/lookup" \
  -F "csrf=$CSRF" -F "key=secretkey")
assert_status "POST /lookup with key -> 200" 200 "$s"
# direct private uuid works without key (plan rule)
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/s/$ID_PRIV")
assert_status "GET /s/{private} direct uuid -> 200" 200 "$s"

echo "== session fixation: sid rotates on admin login (S1) =="
rm -f "$COOKIES"
# get anonymous session
curl -s -c "$COOKIES" "$BASE/admin/login" -o "$TMP/login.html"
CSRF=$(grep -oE 'name="csrf" value="[^"]+"' "$TMP/login.html" | sed 's/.*value="//;s/"//' | head -1)
SID_BEFORE=$(awk '$6=="sid"{print $7}' "$COOKIES")
echo "  anon sid: $SID_BEFORE"
# login
s=$(curl -s -b "$COOKIES" -c "$COOKIES" -o /dev/null -w "%{http_code}" -X POST "$BASE/admin/login" \
  -F "csrf=$CSRF" -F "username=$ADMIN_USER" -F "password=$ADMIN_PASS")
assert_status "admin login -> 303" 303 "$s"
SID_AFTER=$(awk '$6=="sid"{print $7}' "$COOKIES")
echo "  admin sid: $SID_AFTER"
if [ -n "$SID_BEFORE" ] && [ -n "$SID_AFTER" ] && [ "$SID_BEFORE" != "$SID_AFTER" ]; then
  ok "sid rotated on login"
else err "sid NOT rotated (fixation): before=$SID_BEFORE after=$SID_AFTER"; fi
# new sid has admin
s=$(curl -s -b "$COOKIES" -o /dev/null -w "%{http_code}" "$BASE/admin")
assert_status "GET /admin with new sid -> 200" 200 "$s"
# old sid loses admin (fixation defeated)
s=$(curl -s -b "sid=$SID_BEFORE" -o /dev/null -w "%{http_code}" "$BASE/admin")
assert_status "GET /admin with old sid -> 303 (no admin)" 303 "$s"

echo "== admin delete share (A) =="
# need fresh csrf from admin/shares page
curl -s -b "$COOKIES" "$BASE/admin/shares" -o "$TMP/admin_shares.html"
CSRF=$(grep -oE 'name="csrf" value="[^"]+"' "$TMP/admin_shares.html" | sed 's/.*value="//;s/"//' | head -1)
BLOBS_BEFORE_DEL=$(ls "$BLOB" 2>/dev/null | wc -l)
s=$(curl -s -b "$COOKIES" -o /dev/null -w "%{http_code}" -X POST "$BASE/admin/shares/$ID/delete" -F "csrf=$CSRF")
assert_status "admin delete -> 303" 303 "$s"
s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/s/$ID")
assert_status "GET /s/{deleted} -> 404" 404 "$s"
BLOBS_AFTER_DEL=$(ls "$BLOB" 2>/dev/null | wc -l)
if [ "$BLOBS_AFTER_DEL" -lt "$BLOBS_BEFORE_DEL" ]; then ok "blob removed on admin delete"
else err "blob NOT removed on admin delete: $BLOBS_BEFORE_DEL -> $BLOBS_AFTER_DEL"; fi

echo "== Secure cookie flag via proxy proto (S2) =="
# trusted proxy is on for this server; X-Forwarded-Proto: https -> Secure
sc=$(curl -s -D - -o /dev/null -H "X-Forwarded-Proto: https" "$BASE/admin/login" | grep -i '^set-cookie: sid' | head -1)
if echo "$sc" | grep -qi 'Secure'; then ok "X-Forwarded-Proto https -> Secure flag set"
else err "no Secure flag with X-Forwarded-Proto https: $sc"; fi
# plain http -> no Secure
sc=$(curl -s -D - -o /dev/null "$BASE/admin/login" | grep -i '^set-cookie: sid' | head -1)
if echo "$sc" | grep -qi 'Secure'; then err "Secure flag set on plain http: $sc"
else ok "plain http -> no Secure flag"; fi

echo "== expired session row dropped on visitor return (S3) =="
# A bogus/expired sid cookie should be replaced with a fresh sid (the expired
# row path in getOrCreateSession deletes and reissues). We simulate by sending
# an arbitrary unknown sid; server must issue a new one rather than accept it.
BOGUS_SID="deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
sc=$(curl -s -D - -o /dev/null -b "sid=$BOGUS_SID" "$BASE/admin/login" | grep -i '^set-cookie: sid' | head -1)
NEW_SID=$(echo "$sc" | grep -oE 'sid=[^;]+' | sed 's/sid=//')
if [ -n "$NEW_SID" ] && [ "$NEW_SID" != "$BOGUS_SID" ]; then
  ok "unknown/expired sid reissued (no reuse)"
else err "sid reused or not reissued: bogus=$BOGUS_SID new=$NEW_SID"; fi

echo "== expiry: upload -> exists -> wait -> gone (timing) =="
# Upload endpoint only accepts expiry_hours (min 1h), so upload normally then
# shorten expires_at to now+5s via the setexpiry helper, then observe the
# active->expired transition over curl.
resp=$(curl -s -b "$COOKIES" -X POST "$BASE/upload" -H "X-CSRF-Token: $CSRF" -F "csrf=$CSRF" -F "title=exptime" -F "visibility=public" -F "expiry_hours=6" -F "blob=@$TMP/tiny.zip")
ID_EXP=$(echo "$resp" | grep -oE '"id":"[^"]+"' | sed 's/"id":"//;s/"//')
if [ -z "$ID_EXP" ]; then err "expiry test: upload failed: $resp"
else
  go run ./test/setexpiry "$DB" "$ID_EXP" 5 >"$TMP/setexp.log" 2>&1 || { err "setexpiry failed: $(cat "$TMP/setexp.log")"; ID_EXP=""; }
fi
if [ -n "$ID_EXP" ]; then
  s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/blob/$ID_EXP")
  assert_status "GET /blob/{id} before expiry -> 200 (active)" 200 "$s"
  s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/s/$ID_EXP")
  assert_status "GET /s/{id} before expiry -> 200 (exists)" 200 "$s"
  home=$(curl -s "$BASE/")
  if echo "$home" | grep -q "/s/$ID_EXP"; then ok "share appears in public list before expiry"
  else err "share missing from public list before expiry"; fi
  echo "  waiting 10s for expiry..."
  sleep 10
  s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/blob/$ID_EXP")
  assert_status "GET /blob/{id} after expiry -> 410 (gone)" 410 "$s"
  s=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/s/$ID_EXP")
  assert_status "GET /s/{id} after expiry -> 200 (expired page)" 200 "$s"
  body=$(curl -s "$BASE/s/$ID_EXP")
  if echo "$body" | grep -qi 'expired'; then ok "share page shows expired after timeout"
  else err "share page does not show expired after timeout"; fi
  home=$(curl -s "$BASE/")
  if echo "$home" | grep -q "/s/$ID_EXP"; then err "expired share still in public list"
  else ok "expired share removed from public list"; fi
fi

echo ""
echo "================================"
echo "PASS=$PASS  FAIL=$FAIL"
echo "================================"
[ "$FAIL" -eq 0 ]

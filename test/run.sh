#!/usr/bin/env bash
# Run all test layers: Go unit tests, node Progress test, curl smoke suite.
# Exit non-zero if any layer fails.
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v go >/dev/null 2>&1; then
  if [ -x /usr/local/go/bin/go ]; then export PATH=/usr/local/go/bin:$PATH
  else echo "go not found"; exit 2; fi
fi

rc=0

echo "### go test ./..."
go test ./... || rc=1

echo
echo "### node client JS tests  (Progress + text + crypto metadata)"
if command -v node >/dev/null 2>&1; then
  node test/progress.mjs || rc=1
  node test/text.mjs || rc=1
  node test/crypto.mjs || rc=1
else
  echo "  skipped: node not installed"
fi

echo
echo "### bash test/smoke.sh  (curl integration — backend + security fixes)"
bash test/smoke.sh || rc=1

echo
if [ "$rc" -eq 0 ]; then echo "ALL TESTS PASS"; else echo "TEST FAILURES"; fi
exit "$rc"

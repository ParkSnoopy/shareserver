#!/usr/bin/env bash

set +x
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"

if [[ $# == 1 && $1 == --version ]]; then exec ./shareserver --version; fi
if [[ $# != 0 ]]; then echo "Unexpected container arguments." >&2; exit 1; fi

difficulty=${ANUBIS_DIFFICULTY:-4}
if [[ ! "$difficulty" =~ ^([1-9]|[1-5][0-9]|6[0-4])$ ]]; then
	echo "ANUBIS_DIFFICULTY must be an integer between 1 and 64." >&2
	exit 1
fi
remote_address=${ANUBIS_USE_REMOTE_ADDRESS:-true}
case "$remote_address" in
	true) export TRUST_PROXY_HEADERS=false ;;
	false) export TRUST_PROXY_HEADERS=true ;;
	*) echo "ANUBIS_USE_REMOTE_ADDRESS must be true or false." >&2; exit 1 ;;
esac

# Never expose the unchallenged backend, even with an old ADDR in .env.
export ADDR=127.0.0.1:8081
mkdir -p -- "${BLOB_DIR:-data/blobs}"

children=()
cleanup() {
	trap - EXIT TERM INT
	if [[ ${#children[@]} != 0 ]]; then
		kill -TERM "${children[@]}" 2>/dev/null || true
		wait "${children[@]}" 2>/dev/null || true
	fi
}
trap cleanup EXIT
trap 'exit 0' TERM INT

./shareserver &
children+=("$!")
anubis \
	--bind="${ANUBIS_BIND:-0.0.0.0:8080}" \
	--target=http://127.0.0.1:8081 \
	--policy-fname="$PWD/anubis-policy.yaml" \
	--difficulty="$difficulty" \
	--metrics-bind=127.0.0.1:9090 \
	--use-remote-address="$remote_address" \
	--xff-strip-private=false &
children+=("$!")

# Either child's exit takes down the pair so a restart cannot leave a bypass.
status=0
wait -n "${children[@]}" || status=$?
if [[ $status == 0 ]]; then status=1; fi
exit "$status"

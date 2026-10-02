#!/usr/bin/env bash

# Prepare self-signed TLS and deploy the existing Compose stack without sourcing .env.
set +x
set -euo pipefail
umask 077

die() { echo "Error: $*" >&2; exit 1; }

# Prompt into a named variable; EOF is not consent to modify the deployment.
ask() {
	local answer
	read -r -p "$2 [$3]: " answer || die "Input ended before setup completed."
	printf -v "$1" '%s' "${answer:-$3}"
}

confirm() {
	local answer
	read -r -p "$1 [y/N]: " answer || die "Input ended before confirmation."
	case "$answer" in
		[Yy]|[Yy][Ee][Ss]) return 0 ;;
		""|[Nn]|[Nn][Oo]) return 1 ;;
		*) die "Answer yes or no." ;;
	esac
}

prepare_only=false
case "${1:-}" in
	"") [[ $# == 0 ]] || die "Unexpected arguments." ;;
	--prepare-only) [[ $# == 1 ]] || die "Unexpected arguments."; prepare_only=true ;;
	--help)
		[[ $# == 1 ]] || die "Unexpected arguments."
		echo "Usage: bash deploy/deploy.sh [--prepare-only]"
		echo "Prompt for TLS hostname/IP, HTTPS port, and certificate lifetime."
		echo "Use --prepare-only to prepare TLS/settings without running Docker."
		exit 0 ;;
	*) die "Unknown option; use --help." ;;
esac

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(cd -- "$script_dir/.." && pwd)
settings="$script_dir/.env"
tls_dir="$root/data/tls"
cert="$tls_dir/server.crt"
key="$tls_dir/server.key"
command -v openssl >/dev/null || die "OpenSSL is required."

if ! $prepare_only; then
	command -v docker >/dev/null || die "Docker with Compose is required."
	command -v curl >/dev/null || die "curl is required for HTTPS verification."
	[[ -f "$root/.env" ]] || die "Configure the repository-root .env first."
	docker compose version >/dev/null
	docker info >/dev/null
	# Check app configuration without printing resolved credentials.
	docker compose --env-file "$root/.env" -f "$script_dir/docker-compose.yaml" config --quiet
fi

TLS_HOST=localhost
HTTPS_PORT=8443
TLS_DAYS=365
[[ ! -L "$settings" ]] || die "Deployment settings must not be a symlink."

# Serialize setup and keep failed preparation from leaving temporary private keys.
lock_dir="$script_dir/.env.lock"
work_dir=''
settings_tmp=''
cleanup() {
	if [[ -n "$work_dir" ]]; then rm -rf -- "$work_dir"; fi
	if [[ -n "$settings_tmp" ]]; then rm -f -- "$settings_tmp"; fi
	rmdir -- "$lock_dir"
}
mkdir -- "$lock_dir" || die "Deployment is locked; check deploy/.env.lock before retrying."
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ -f "$settings" ]]; then
	# This script owns only public deployment settings; never evaluate shell input.
	while IFS='=' read -r name value || [[ -n "$name" ]]; do
		case "$name" in
			TLS_HOST) TLS_HOST=$value ;;
			HTTPS_PORT) HTTPS_PORT=$value ;;
			TLS_DAYS) TLS_DAYS=$value ;;
			HTTPS_UID|HTTPS_GID|""|\#*) ;;
			*) die "Unexpected setting in deploy/.env." ;;
		esac
	done < "$settings"
fi

ask TLS_HOST "Certificate domain or IP (no scheme or port)" "$TLS_HOST"
san_type=DNS
if [[ "$TLS_HOST" == *:* ]]; then
	[[ "$TLS_HOST" =~ ^[[:xdigit:]:.]+$ ]] || die "Invalid IPv6 address."
	san_type=IP
elif [[ "$TLS_HOST" =~ ^[0-9]+(\.[0-9]+){3}$ ]]; then
	IFS='.' read -r -a octets <<< "$TLS_HOST"
	for octet in "${octets[@]}"; do
		[[ "$octet" =~ ^(0|[1-9][0-9]{0,2})$ ]] && ((10#$octet <= 255)) || die "Invalid IPv4 address."
	done
	san_type=IP
else
	[[ ${#TLS_HOST} -le 253 && "$TLS_HOST" =~ ^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$ ]] || die "Invalid certificate domain."
fi

ask HTTPS_PORT "HTTPS port to publish" "$HTTPS_PORT"
[[ "$HTTPS_PORT" =~ ^[1-9][0-9]{0,4}$ ]] && ((HTTPS_PORT <= 65535)) || die "Port must be between 1 and 65535."
ask TLS_DAYS "Certificate lifetime in days" "$TLS_DAYS"
[[ "$TLS_DAYS" =~ ^[1-9][0-9]{0,3}$ ]] && ((TLS_DAYS <= 3650)) || die "Lifetime must be between 1 and 3650 days."
HTTPS_UID=$(id -u)
HTTPS_GID=$(id -g)
export TLS_HOST HTTPS_PORT TLS_DAYS HTTPS_UID HTTPS_GID

[[ ! -L "$root/data" && ! -L "$tls_dir" && ! -L "$cert" && ! -L "$key" ]] || die "TLS storage must not use symlinks."
renew=true
if [[ -e "$cert" || -e "$key" ]]; then
	renew=false
	if confirm "Replace the existing certificate/key? Clients will need to trust the replacement"; then
		renew=true
	fi
fi
echo "Certificate name: $TLS_HOST"
echo "Published HTTPS port: $HTTPS_PORT"
if $renew; then
	echo "New certificate lifetime: $TLS_DAYS days (P-521)."
else
	echo "Reuse the existing certificate; its lifetime will not change."
fi
if ! confirm "Apply these settings"; then
	echo "Cancelled."
	exit 0
fi

if $renew; then
	mkdir -p -- "$tls_dir"
	work_dir=$(mktemp -d "$tls_dir/.prepare.XXXXXX")
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:secp521r1 \
		-sha512 -nodes -days "$TLS_DAYS" -subj "/CN=$TLS_HOST" \
		-addext "subjectAltName=$san_type:$TLS_HOST" \
		-addext "basicConstraints=critical,CA:FALSE" \
		-addext "keyUsage=critical,digitalSignature" \
		-addext "extendedKeyUsage=serverAuth" \
		-keyout "$work_dir/server.key" -out "$work_dir/server.crt"
	check_cert="$work_dir/server.crt"
	check_key="$work_dir/server.key"
else
	[[ -f "$cert" && -f "$key" ]] || die "Both certificate and key must exist; choose replacement to repair them."
	check_cert=$cert
	check_key=$key
fi

# Verify expiry, hostname/IP, and key pairing before replacing files or saving settings.
identity=(-verify_hostname "$TLS_HOST")
if [[ "$san_type" == IP ]]; then identity=(-verify_ip "$TLS_HOST"); fi
openssl verify -purpose sslserver "${identity[@]}" -CAfile "$check_cert" "$check_cert"
cert_public=$(openssl x509 -in "$check_cert" -pubkey -noout | openssl pkey -pubin -outform DER | openssl dgst -sha256)
key_public=$(openssl pkey -in "$check_key" -passin pass: -pubout -outform DER | openssl dgst -sha256)
[[ "$cert_public" == "$key_public" ]] || die "Certificate and private key do not match."
[[ -O "$check_key" ]] || die "Private key must be owned by the deploying user."

if $renew; then
	mv -f -- "$check_key" "$key"
	mv -f -- "$check_cert" "$cert"
fi
chmod 600 -- "$key"
settings_tmp=$(mktemp "$script_dir/.env.XXXXXX")
{
	echo "TLS_HOST=$TLS_HOST"
	echo "HTTPS_PORT=$HTTPS_PORT"
	echo "TLS_DAYS=$TLS_DAYS"
	echo "HTTPS_UID=$HTTPS_UID"
	echo "HTTPS_GID=$HTTPS_GID"
} > "$settings_tmp"
mv -f -- "$settings_tmp" "$settings"
settings_tmp=''
echo "Prepared certificates in data/tls and public settings in deploy/.env."
if $prepare_only; then exit 0; fi

compose=(docker compose --env-file "$root/.env" --env-file "$settings" -f "$script_dir/docker-compose.yaml")
"${compose[@]}" config --quiet
echo "Starting Compose; containers are recreated, stored data is retained."
"${compose[@]}" up -d --force-recreate
url_host=$TLS_HOST
if [[ "$san_type" == IP && "$TLS_HOST" == *:* ]]; then url_host="[$TLS_HOST]"; fi
url="https://$url_host:$HTTPS_PORT"
# Connect locally while checking the chosen certificate identity, not an insecure override.
status=$(curl -fsS --noproxy '*' --cacert "$cert" \
	--connect-to "$url_host:$HTTPS_PORT:127.0.0.1:$HTTPS_PORT" \
	--retry 10 --retry-connrefused --retry-delay 1 --retry-max-time 30 --max-time 5 \
	--output /dev/null --write-out '%{http_code}' "$url/api/")
[[ "$status" == 200 ]] || die "HTTPS verification returned HTTP $status."
echo "Deployed and verified: $url"
echo "Trust data/tls/server.crt on each client; keep server.key private."
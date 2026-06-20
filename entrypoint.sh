#!/bin/sh

set -x
cd /app

# Ensure SQLite Database Directory and BLOB Directory
mkdir -p "/app/data/blobs"

# Update App Secret
sed -i "s/^APP_SECRET =.*/APP_SECRET = $(cat \/dev\/urandom | LC_ALL=C tr -dc a-zA-Z0-9 | head -c 128)/" .env

# Run the Server
exec /app/shareserver

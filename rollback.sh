#!/bin/bash
# Rollback manual: bash /opt/isp-billing/rollback.sh /opt/isp-billing/snap-XXXX
set -euo pipefail
SNAP=${1:?usage: rollback.sh <snapshot-dir>}
APP=/opt/isp-billing/app
# Mode aaPanel: paksa compose memakai varian 127.0.0.1:8090.
export COMPOSE_FILE="$APP/compose.aapanel.yaml"
test -f "$SNAP/src.tar.gz" || { echo "snapshot tidak ada: $SNAP"; exit 1; }
chattr -i "$APP/.env" 2>/dev/null || true
tar xzf "$SNAP/src.tar.gz" -C "$APP"
cp -a "$SNAP/.env"                 "$APP/.env"                 2>/dev/null || true
cp -a "$SNAP/compose.aapanel.yaml" "$APP/compose.aapanel.yaml" 2>/dev/null || true
cp -a "$SNAP/Caddyfile.aapanel"    "$APP/Caddyfile.aapanel"    2>/dev/null || true
cd "$APP"
docker compose build --no-cache api migrate
docker compose up -d --wait --wait-timeout 180
curl -s -m 8 http://127.0.0.1:8090/ready && echo " <- ROLLBACK OK"

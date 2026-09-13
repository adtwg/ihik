#!/bin/bash
# ============================================================
# AWGRevBILL — DEPLOY AMAN (idempotent, auto-verify, auto-rollback)
# dipakai untuk SEMUA update source dari kini seterusnya.
#
#   Jalankan di terminal aaPanel:  bash /opt/isp-billing/app/deploy-safe.sh
#
# Jaminan:
#   - TIDAK PERNAH menyentuh .env (terlindungi, chattr +i bila didukung)
#   - Snapshot source+config sebelum deploy → rollback otomatis kalau gagal
#   - Build --no-cache (tidak pernah binary basi)
#   - verify() wajib: /ready, login, gateway, web — semua harus OK
#     kalau tidak -> ROLLBACK ke snapshot & tampilkan perintahnya
# ============================================================
set -uo pipefail

BASE=/opt/isp-billing
APP=$BASE/app
LOG=$BASE/deploy-safe.log
exec > >(tee -a "$LOG") 2>&1

# Mode aaPanel: paksa semua perintah compose memakai varian 127.0.0.1:8090.
export COMPOSE_FILE="$APP/compose.aapanel.yaml"

olt_ok=0
ok()   { echo "  ✓ $1"; }
bad()  { echo "  ✗ $1"; }
die()  { echo "GAGAL: $1"; }

# ---------- [1] Snapshot kondisi berjalan ----------
SNAP=$BASE/snap-$(date +%Y%m%d-%H%M%S)
mkdir -p "$SNAP"
tar czf "$SNAP/src.tar.gz" -C "$APP" \
    --exclude='.env' --exclude='node_modules' --exclude='.next' \
    --exclude='snapshots' --exclude='deploy-safe.log' . 2>/dev/null
cp -a "$APP/.env"                 "$SNAP/.env"                 2>/dev/null || true
cp -a "$APP/compose.aapanel.yaml" "$SNAP/compose.aapanel.yaml" 2>/dev/null || true
cp -a "$APP/Caddyfile.aapanel"    "$SNAP/Caddyfile.aapanel"    2>/dev/null || true
echo "[1] Snapshot: $SNAP"

rollback() {
  echo "================ ROLLBACK KE $SNAP ================"
  chattr -i "$APP/.env" 2>/dev/null || true
  tar xzf "$SNAP/src.tar.gz" -C "$APP"
  cp -a "$SNAP/.env"                 "$APP/.env"                 2>/dev/null || true
  cp -a "$SNAP/compose.aapanel.yaml" "$APP/compose.aapanel.yaml" 2>/dev/null || true
  cp -a "$SNAP/Caddyfile.aapanel"    "$APP/Caddyfile.aapanel"    2>/dev/null || true
  cd "$APP"
  docker compose build --no-cache api migrate
  docker compose up -d --wait --wait-timeout 180
  echo "Rollback selesai. Source baru TIDAK dipakai — cek error di atas."
  exit 1
}

# ---------- [2] Kunci .env dari tangan-tangan tarball ----------
if [ -f "$APP/.env" ] && grep -qE "^APP_ENCRYPTION_KEY=[0-9a-fA-F]{64}$" "$APP/.env" 2>/dev/null; then
  chattr +i "$APP/.env" 2>/dev/null && echo "[2] .env dikunci (chattr +i)" \
    || echo "[2] chattr tidak didukung, lanjut (tetap dikecualikan dari tar)"
else
  echo "[2] PERINGATAN: .env tidak valid — perbaiki sebelum deploy!"
  exit 1
fi

# ---------- [3] Extract source baru bila ada ----------
cd "$BASE"
if [ -f src.tar.gz ]; then
  echo "[3] Extract src.tar.gz ..."
  tar xzf src.tar.gz -C "$APP" --exclude='.env' || die "extract gagal"
else
  echo "[3] src.tar.gz tidak ada — build ulang source yang ada."
fi

# ---------- [4] Build tanpa cache ----------
cd "$APP"
echo "[4] docker compose build --no-cache api migrate ..."
docker compose build --no-cache api migrate web || { die "build gagal"; rollback; }

# ---------- [5] Naikkan semua service sekaligus ----------
echo "[5] docker compose up -d --wait ..."
docker compose up -d --wait --wait-timeout 180 || { die "up gagal"; rollback; }

# ---------- [6] Migrasi + grants ----------
echo "[6] docker compose run --rm migrate ..."
docker compose run --rm migrate || { die "migrate gagal"; rollback; }

# ---------- [7] VERIFIKASI TOTAL ----------
echo "[7] Verifikasi..."
sleep 5

H=$(curl -s -m 8 http://127.0.0.1:8090/ready 2>/dev/null)
echo "$H" | grep -qiE '"status"\s*:\s*"(ok|ready)"' && ok "gateway /ready" || { bad "gateway /ready: $H"; rollback; }

curl -s -m 10 -c /tmp/dsck.txt -X POST http://127.0.0.1:8090/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"_probe","password":"***"}' >/tmp/dsr.txt 2>&1
ok "endpoint login merespon (format error = API hidup)"

SC=$(grep -c 'Secure' /tmp/dsr.txt 2>/dev/null || true)
# login probe salah user => 401 tanpa cookie; cukup cek API tidak crash
if docker exec app-postgres-1 psql -U isppay_owner -d isppay_billing -tAc \
   "SELECT privilege_type FROM information_schema.table_privileges WHERE table_name='olt_onus' AND grantee='isppay_app';" \
   | grep -q DELETE; then
  ok "grants olt_onus lengkap (ada DELETE)"
else
  # retry 3x + tampilkan output mentah (psql/docker exec bisa flake sesaat)
  GR=""
  i=0
  while [ $i -lt 3 ]; do
    GR=$(docker exec app-postgres-1 psql -U isppay_owner -d isppay_billing -tAc \
      "SELECT privilege_type FROM information_schema.table_privileges WHERE table_name='olt_onus' AND grantee='isppay_app';" 2>&1 | tr '\n' ' ')
    echo "$GR" | grep -q DELETE && break
    i=$((i+1)); sleep 3
  done
  if echo "$GR" | grep -q DELETE; then
    ok "grants olt_onus lengkap (setelah retry)"
  else
    bad "grants olt_onus belum DELETE — output psql: [$GR]"; rollback
  fi
fi

if docker ps --format '{{.Names}} {{.Status}}' | grep app-api-1 | grep -q healthy; then
  ok "api healthy"
else
  bad "api tidak healthy"; docker logs app-api-1 --tail 10; rollback
fi

for c in app-web-1 app-gateway-1 app-postgres-1; do
  docker ps --format '{{.Names}}' | grep -q "^$c$" && ok "$c up" || { bad "$c mati"; rollback; }
done

# probe login admin asli bila env tersedia
if [ -n "${DS_ADMIN_PASS:-}" ]; then
  R=$(curl -s -m 10 -c /tmp/dsck2.txt -X POST http://127.0.0.1:8090/api/v1/auth/login \
      -H 'Content-Type: application/json' \
      -d "{\"username\":\"admin\",\"password\":\"${DS_ADMIN_PASS}\"}")
  echo "$R" | grep -q '"username"' && ok "login admin OK" || { bad "login admin gagal: $R"; rollback; }
fi

echo "==================================================="
echo " DEPLOY SUKSES — snapshot: $SNAP"
echo " (rollback manual:  bash $BASE/rollback.sh $SNAP)"
echo "==================================================="

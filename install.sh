#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'
umask 027

readonly INSTALLER_VERSION="1.0.0"
readonly TOTAL_STEPS=16
readonly LOG_DIR="/var/log/isp-billing"
readonly LOG_FILE="$LOG_DIR/installer.log"
readonly STATE_DIR="/var/lib/isp-billing"
readonly STATE_FILE="$STATE_DIR/installer-state"
readonly GENERATED_ADMIN_FILE="/root/isp-billing-initial-admin.txt"
readonly BACKUP_DIR="/var/backups/isp-billing"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
APP_DIR="$SCRIPT_DIR"
ENV_FILE="$APP_DIR/.env"
CURRENT_STEP="inisialisasi"
STEP_NUMBER=0
NON_INTERACTIVE=false
ALLOW_LOW_RESOURCES=false
GENERATE_ADMIN_PASSWORD=false
APP_DOMAIN=""
ACME_EMAIL=""
ADMIN_USERNAME=""
ADMIN_PASSWORD=""
ADMIN_PASSWORD_FILE=""
DEPLOY_USER="deploy"
DEPLOY_HOME=""
DEPLOY_USER_EXPLICIT=false
SSH_PORT_OVERRIDE=""
TIMEZONE="Asia/Jakarta"
EXISTING_ENV=false
EXISTING_INSTALL=false
INSTALL_STATE_LOADED=false
INSTALL_STATE_STATUS=""
ADMIN_CREATED=false
ADMIN_PASSWORD_GENERATED=false
ADMIN_PASSWORD_RECOVERED=false
EARLY_BACKUP_DONE=false
EMPTY_UNINITIALIZED_VOLUME=false
DATABASE_INITIALIZED=false
DATABASE_VOLUME_NAME=""
DATABASE_VOLUME_ID=""
PUBLIC_IPV4=""
GIT_COMMIT="unknown"
TEMP_FILES=()

usage() {
  cat <<'EOF'
ISP Billing production installer untuk Ubuntu Server 24.04 LTS.

Penggunaan:
  sudo bash install.sh
  sudo bash install.sh --non-interactive \
    --domain billing.example.com \
    --acme-email admin@example.com \
    --admin-user admin \
    --generate-admin-password

Opsi:
  --domain DOMAIN               Domain publik tanpa skema atau port
  --acme-email EMAIL            Email notifikasi sertifikat HTTPS
  --admin-user USERNAME         Username Super Admin pertama
  --admin-password-file FILE    File satu baris, owner root, mode 600
  --generate-admin-password     Generate password Super Admin otomatis
  --deploy-user USER            User Linux operasional (default: deploy)
  --ssh-port PORT               Port SSH aktif bila tidak dapat dideteksi
  --timezone ZONE               Timezone (default: Asia/Jakarta)
  --non-interactive             Tidak menampilkan prompt
  --allow-low-resources         Izinkan VPS di bawah rekomendasi minimum
  -h, --help                    Tampilkan bantuan

Password database selalu digenerate acak. Password Super Admin tidak dapat
diberikan sebagai argumen literal agar tidak masuk shell history/process list.
EOF
}

ui() {
  printf '%s\n' "$*"
}

warn() {
  printf 'PERINGATAN: %s\n' "$*" >&2
}

fatal() {
  printf 'GAGAL [%s]: %s\n' "$CURRENT_STEP" "$*" >&2
  printf 'Log: %s\n' "$LOG_FILE" >&2
  exit 1
}

progress() {
  STEP_NUMBER=$((STEP_NUMBER + 1))
  CURRENT_STEP="$1"
  printf '\n[%02d/%02d] %s\n' "$STEP_NUMBER" "$TOTAL_STEPS" "$CURRENT_STEP"
}

cleanup() {
  ADMIN_PASSWORD=""
  unset BOOTSTRAP_ADMIN_USERNAME BOOTSTRAP_ADMIN_PASSWORD 2>/dev/null || true
  local temporary
  for temporary in "${TEMP_FILES[@]:-}"; do
    [[ -n "$temporary" ]] && rm -f -- "$temporary"
  done
  return 0
}

on_error() {
  local exit_code=$?
  local line_number=$1
  trap - ERR
  printf 'GAGAL [%s] pada baris %s (exit %s).\n' "$CURRENT_STEP" "$line_number" "$exit_code" >&2
  printf 'Periksa log: %s\n' "$LOG_FILE" >&2
  cleanup
  exit "$exit_code"
}

trap 'on_error $LINENO' ERR
trap cleanup EXIT

require_value() {
  local option=$1
  local value=${2:-}
  [[ -n "$value" ]] || {
    printf 'Opsi %s memerlukan nilai.\n' "$option" >&2
    exit 2
  }
}

parse_arguments() {
  while (($# > 0)); do
    case "$1" in
      --domain)
        require_value "$1" "${2:-}"
        APP_DOMAIN=$2
        shift 2
        ;;
      --acme-email)
        require_value "$1" "${2:-}"
        ACME_EMAIL=$2
        shift 2
        ;;
      --admin-user)
        require_value "$1" "${2:-}"
        ADMIN_USERNAME=$2
        shift 2
        ;;
      --admin-password-file)
        require_value "$1" "${2:-}"
        ADMIN_PASSWORD_FILE=$2
        shift 2
        ;;
      --generate-admin-password)
        GENERATE_ADMIN_PASSWORD=true
        shift
        ;;
      --deploy-user)
        require_value "$1" "${2:-}"
        DEPLOY_USER=$2
        DEPLOY_USER_EXPLICIT=true
        shift 2
        ;;
      --ssh-port)
        require_value "$1" "${2:-}"
        SSH_PORT_OVERRIDE=$2
        shift 2
        ;;
      --timezone)
        require_value "$1" "${2:-}"
        TIMEZONE=$2
        shift 2
        ;;
      --non-interactive)
        NON_INTERACTIVE=true
        shift
        ;;
      --allow-low-resources)
        ALLOW_LOW_RESOURCES=true
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        printf 'Opsi tidak dikenal: %s\n\n' "$1" >&2
        usage >&2
        exit 2
        ;;
    esac
  done

  if [[ -n "$ADMIN_PASSWORD_FILE" && "$GENERATE_ADMIN_PASSWORD" == true ]]; then
    printf '%s\n' '--admin-password-file dan --generate-admin-password tidak dapat dipakai bersamaan.' >&2
    exit 2
  fi
}

setup_runtime() {
  if ((EUID != 0)); then
    printf 'Jalankan installer sebagai root: sudo bash install.sh\n' >&2
    exit 1
  fi
  install -d -o root -g root -m 0700 "$LOG_DIR" "$STATE_DIR"
  touch "$LOG_FILE"
  chmod 0600 "$LOG_FILE"
  exec 9>"/var/lock/isp-billing-installer.lock"
  if ! flock -n 9; then
    fatal "installer lain sedang berjalan"
  fi
  printf '\n=== Installer %s | %s ===\n' "$INSTALLER_VERSION" "$(date -Is)" >>"$LOG_FILE"
}

sanitize_process_environment() {
  unset POSTGRES_DB POSTGRES_USER POSTGRES_PASSWORD POSTGRES_APP_USER POSTGRES_APP_PASSWORD
  unset DATABASE_URL DATABASE_RUNTIME_USER BOOTSTRAP_ADMIN_USERNAME BOOTSTRAP_ADMIN_PASSWORD APP_ENCRYPTION_KEY
  unset COMPOSE_FILE COMPOSE_PROFILES COMPOSE_ENV_FILES DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH
  export DOCKER_HOST=unix:///var/run/docker.sock
  export -n APP_DOMAIN ACME_EMAIL ADMIN_USERNAME ADMIN_PASSWORD 2>/dev/null || true
}

run_logged() {
  "$@" >>"$LOG_FILE" 2>&1
}

git_safe() {
  git -c "safe.directory=$APP_DIR" -C "$APP_DIR" "$@"
}

compose() {
  docker compose \
    --project-directory "$APP_DIR" \
    --file "$APP_DIR/compose.yaml" \
    --env-file "$ENV_FILE" \
    --project-name "$COMPOSE_PROJECT_NAME" \
    "$@"
}

validate_local_docker() {
  [[ "$DOCKER_HOST" == "unix:///var/run/docker.sock" ]] || fatal "Docker harus memakai socket lokal"
  [[ -S /var/run/docker.sock ]] || fatal "socket Docker lokal /var/run/docker.sock tidak tersedia"
  docker info >>"$LOG_FILE" 2>&1 || fatal "daemon Docker lokal tidak merespons"
}

validate_repository() {
  [[ "$APP_DIR" == /opt/* ]] || fatal "clone repository ke /opt/isp-billing sebelum menjalankan installer"
  [[ "$APP_DIR" =~ ^/opt/[A-Za-z0-9._/-]+$ && "$APP_DIR" != *..* ]] || fatal "path repository hanya boleh memakai karakter aman di bawah /opt"
  [[ -d "$APP_DIR/.git" ]] || fatal "$APP_DIR bukan Git repository"
  local required
  for required in compose.yaml Caddyfile Dockerfile go.mod web/package-lock.json; do
    [[ -f "$APP_DIR/$required" ]] || fatal "file wajib tidak ditemukan: $required"
  done
}

validate_tracked_release_files() {
  local entry path file relative
  while IFS= read -r -d '' entry; do
    [[ "${entry:0:2}" == "!!" ]] || continue
    path=${entry:3}
    case "$path" in
      .env | .vscode/ | coverage.out | web/node_modules/ | web/.next/) ;;
      *) fatal "file ignored tidak diizinkan pada release production: $path" ;;
    esac
  done < <(git_safe status --porcelain=v1 -z --ignored=matching --untracked-files=normal)

  while IFS= read -r -d '' file; do
    relative=${file#"$APP_DIR"/}
    git_safe ls-files --error-unmatch -- "$relative" >/dev/null 2>&1 || fatal "migration harus berasal dari Git: $relative"
  done < <(find "$APP_DIR/migrations" -maxdepth 1 -type f -name '*.up.sql' -print0)
}

read_env_value() {
  local key=$1
  local matches=()
  mapfile -t matches < <(grep -E "^${key}=" "$ENV_FILE" || true)
  ((${#matches[@]} == 1)) || fatal "variable $key pada .env harus muncul tepat satu kali"
  printf '%s' "${matches[0]#*=}"
}

read_state_value() {
  local key=$1
  local matches=()
  mapfile -t matches < <(grep -E "^${key}=" "$STATE_FILE" || true)
  ((${#matches[@]} == 1)) || fatal "key $key pada installer-state harus muncul tepat satu kali"
  printf '%s' "${matches[0]#*=}"
}

load_installer_state() {
  [[ -f "$STATE_FILE" ]] || return 0
  [[ ! -L "$STATE_FILE" ]] || fatal "installer-state tidak boleh berupa symlink"
  [[ "$(stat -c '%U:%a' "$STATE_FILE")" == "root:600" ]] || fatal "installer-state harus owner root mode 600"

  local stored_status stored_app_dir stored_domain stored_deploy_user identity_key_count
  stored_status=$(read_state_value STATUS)
  stored_app_dir=$(read_state_value APP_DIR)
  stored_domain=$(read_state_value APP_DOMAIN)
  stored_deploy_user=$(read_state_value DEPLOY_USER)
  [[ "$stored_status" == "installing" || "$stored_status" == "complete" ]] || fatal "status installer-state tidak valid"
  [[ "$stored_app_dir" == "$APP_DIR" ]] || fatal "repository berbeda dari installer-state: $stored_app_dir"
  validate_domain "$stored_domain"
  [[ "$stored_deploy_user" =~ ^[a-z_][a-z0-9_-]{0,31}$ && "$stored_deploy_user" != "root" ]] || fatal "deploy user pada installer-state tidak valid"
  if [[ "$DEPLOY_USER_EXPLICIT" == true && "$DEPLOY_USER" != "$stored_deploy_user" ]]; then
    fatal "deploy user existing adalah $stored_deploy_user; perubahan user tidak didukung oleh rerun"
  fi
  if [[ -n "$APP_DOMAIN" && "${APP_DOMAIN,,}" != "$stored_domain" ]]; then
    fatal "domain berbeda dari installer-state ($stored_domain)"
  fi
  DEPLOY_USER=$stored_deploy_user
  APP_DOMAIN=$stored_domain
  INSTALL_STATE_LOADED=true
  INSTALL_STATE_STATUS=$stored_status
  [[ "$stored_status" == "complete" ]] && EXISTING_INSTALL=true

  identity_key_count=$(grep -Ec '^(DATABASE_INITIALIZED|DATABASE_VOLUME_NAME|DATABASE_VOLUME_ID)=' "$STATE_FILE" || true)
  [[ "$identity_key_count" == "0" || "$identity_key_count" == "3" ]] || fatal "metadata volume pada installer-state tidak lengkap atau duplikat"
  if [[ "$identity_key_count" == "3" ]]; then
    DATABASE_INITIALIZED=$(read_state_value DATABASE_INITIALIZED)
    DATABASE_VOLUME_NAME=$(read_state_value DATABASE_VOLUME_NAME)
    DATABASE_VOLUME_ID=$(read_state_value DATABASE_VOLUME_ID)
    [[ "$DATABASE_INITIALIZED" == "true" || "$DATABASE_INITIALIZED" == "false" ]] || fatal "DATABASE_INITIALIZED pada installer-state tidak valid"
    if [[ "$DATABASE_INITIALIZED" == "true" ]]; then
      [[ "$DATABASE_VOLUME_NAME" =~ ^[a-z0-9][a-z0-9_.-]{0,254}$ ]] || fatal "nama volume database pada installer-state tidak valid"
      [[ "$DATABASE_VOLUME_ID" =~ ^[a-f0-9]{64}$ ]] || fatal "ID volume database pada installer-state tidak valid"
    else
      if [[ -n "$DATABASE_VOLUME_NAME" || -n "$DATABASE_VOLUME_ID" ]]; then
        [[ "$DATABASE_VOLUME_NAME" =~ ^[a-z0-9][a-z0-9_.-]{0,254}$ ]] || fatal "nama volume database pending pada installer-state tidak valid"
        [[ "$DATABASE_VOLUME_ID" =~ ^[a-f0-9]{64}$ ]] || fatal "ID volume database pending pada installer-state tidak valid"
      fi
    fi
  fi
  if [[ "$stored_status" == "complete" && "$DATABASE_INITIALIZED" != "true" ]]; then
    fatal "installer-state complete tidak memiliki identitas volume; hentikan dan lakukan recovery/adopsi legacy secara manual"
  fi
}

load_existing_environment() {
  [[ -f "$ENV_FILE" ]] || return 0
  [[ "$INSTALL_STATE_LOADED" == true ]] || fatal ".env ada tanpa installer-state; hentikan dan lakukan recovery/adopsi legacy secara manual"
  EXISTING_ENV=true
  local mode env_owner env_domain env_email owner_password app_password owner_user app_user database actual_keys expected_keys legacy_keys encryption_key
  mode=$(stat -c '%a' "$ENV_FILE")
  [[ "$mode" == "600" ]] || fatal ".env harus memiliki mode 600, saat ini $mode"
  env_owner=$(stat -c '%U' "$ENV_FILE")
  [[ "$env_owner" != "root" && "$env_owner" != "UNKNOWN" ]] || fatal ".env harus dimiliki deploy user, bukan $env_owner"
  [[ "$DEPLOY_USER" == "$env_owner" ]] || fatal ".env dimiliki $env_owner tetapi installer-state mencatat deploy user $DEPLOY_USER"
  if ! awk 'NF == 0 || $0 !~ /^[A-Z][A-Z0-9_]*=[^[:space:]]+$/ {exit 1}' "$ENV_FILE"; then
    fatal ".env hanya boleh berisi KEY=value tanpa spasi, komentar, atau baris kosong"
  fi
  actual_keys=$(cut -d= -f1 "$ENV_FILE" | sort)
  legacy_keys=$'ACME_EMAIL\nAPP_DOMAIN\nPOSTGRES_APP_PASSWORD\nPOSTGRES_APP_USER\nPOSTGRES_DB\nPOSTGRES_PASSWORD\nPOSTGRES_USER'
  expected_keys=$'ACME_EMAIL\nAPP_DOMAIN\nAPP_ENCRYPTION_KEY\nPOSTGRES_APP_PASSWORD\nPOSTGRES_APP_USER\nPOSTGRES_DB\nPOSTGRES_PASSWORD\nPOSTGRES_USER'
  if [[ "$actual_keys" == "$legacy_keys" ]]; then
    # Instalasi lama tanpa kunci enkripsi: tambahkan sekali secara atomik.
    local upgrade_temp encryption_key_new
    encryption_key_new=$(openssl rand -hex 32)
    upgrade_temp=$(mktemp "$APP_DIR/.env.tmp.XXXXXX")
    TEMP_FILES+=("$upgrade_temp")
    cat "$ENV_FILE" >"$upgrade_temp"
    printf 'APP_ENCRYPTION_KEY=%s\n' "$encryption_key_new" >>"$upgrade_temp"
    chown "$DEPLOY_USER:$DEPLOY_USER" "$upgrade_temp"
    chmod 0600 "$upgrade_temp"
    mv -f -- "$upgrade_temp" "$ENV_FILE"
    encryption_key_new=""
    actual_keys=$(cut -d= -f1 "$ENV_FILE" | sort)
    ui "APP_ENCRYPTION_KEY ditambahkan ke .env (upgrade dari instalasi lama)."
  fi
  [[ "$actual_keys" == "$expected_keys" ]] || fatal ".env memiliki key tambahan, hilang, atau duplikat"
  env_domain=$(read_env_value APP_DOMAIN)
  env_email=$(read_env_value ACME_EMAIL)
  database=$(read_env_value POSTGRES_DB)
  owner_user=$(read_env_value POSTGRES_USER)
  owner_password=$(read_env_value POSTGRES_PASSWORD)
  app_user=$(read_env_value POSTGRES_APP_USER)
  app_password=$(read_env_value POSTGRES_APP_PASSWORD)
  encryption_key=$(read_env_value APP_ENCRYPTION_KEY)

  [[ "$database" == "isp_billing" ]] || fatal "POSTGRES_DB pada .env tidak dikenali"
  [[ "$owner_user" == "isp_billing_owner" ]] || fatal "POSTGRES_USER pada .env tidak dikenali"
  [[ "$app_user" == "isp_billing_app" ]] || fatal "POSTGRES_APP_USER pada .env tidak dikenali"
  [[ "$owner_password" =~ ^[a-f0-9]{64}$ ]] || fatal "POSTGRES_PASSWORD pada .env tidak valid"
  [[ "$app_password" =~ ^[a-f0-9]{64}$ ]] || fatal "POSTGRES_APP_PASSWORD pada .env tidak valid"
  [[ "$encryption_key" =~ ^[a-f0-9]{64}$ ]] || fatal "APP_ENCRYPTION_KEY pada .env tidak valid"
  [[ "$owner_password" != "$app_password" ]] || fatal "password database owner dan runtime harus berbeda"

  if [[ -n "$APP_DOMAIN" && "$APP_DOMAIN" != "$env_domain" ]]; then
    fatal "domain berbeda dari instalasi existing ($env_domain)"
  fi
  if [[ -n "$ACME_EMAIL" && "$ACME_EMAIL" != "$env_email" ]]; then
    fatal "email ACME berbeda dari instalasi existing ($env_email)"
  fi
  APP_DOMAIN=$env_domain
  ACME_EMAIL=$env_email
}

prompt_value() {
  local variable_name=$1
  local label=$2
  local default_value=${3:-}
  local value=""
  if [[ -n "$default_value" ]]; then
    read -r -p "$label [$default_value]: " value </dev/tty
    value=${value:-$default_value}
  else
    read -r -p "$label: " value </dev/tty
  fi
  printf -v "$variable_name" '%s' "$value"
}

collect_inputs() {
  if [[ "$NON_INTERACTIVE" == false && ! -t 0 ]]; then
    fatal "mode wizard memerlukan terminal; gunakan --non-interactive"
  fi

  if [[ "$EXISTING_ENV" == false ]]; then
    if [[ "$NON_INTERACTIVE" == false ]]; then
      [[ -n "$APP_DOMAIN" ]] || prompt_value APP_DOMAIN "Domain aplikasi (contoh billing.example.com)"
      [[ -n "$ACME_EMAIL" ]] || prompt_value ACME_EMAIL "Email notifikasi HTTPS"
    else
      [[ -n "$APP_DOMAIN" ]] || fatal "--domain wajib pada instalasi non-interaktif pertama"
      [[ -n "$ACME_EMAIL" ]] || fatal "--acme-email wajib pada instalasi non-interaktif pertama"
    fi
  fi

  APP_DOMAIN=${APP_DOMAIN,,}
  validate_domain "$APP_DOMAIN"
  [[ "$ACME_EMAIL" =~ ^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$ ]] || fatal "email ACME tidak valid"
  [[ "$DEPLOY_USER" =~ ^[a-z_][a-z0-9_-]{0,31}$ && "$DEPLOY_USER" != "root" ]] || fatal "nama deploy user tidak valid"
  if [[ -n "$SSH_PORT_OVERRIDE" ]]; then
    if [[ ! "$SSH_PORT_OVERRIDE" =~ ^[0-9]+$ ]] || ((SSH_PORT_OVERRIDE < 1 || SSH_PORT_OVERRIDE > 65535)); then
      fatal "SSH port harus angka 1-65535"
    fi
  fi
  [[ "$TIMEZONE" =~ ^[A-Za-z0-9._+-]+(/[A-Za-z0-9._+-]+)+$ && "$TIMEZONE" != *..* ]] || fatal "format timezone tidak valid"
  [[ -f "/usr/share/zoneinfo/$TIMEZONE" ]] || fatal "timezone tidak valid: $TIMEZONE"
  if [[ -n "$ADMIN_USERNAME" ]]; then
    validate_admin_username "$ADMIN_USERNAME"
  fi
}

validate_domain() {
  local domain=$1 labels=() label
  ((${#domain} <= 253)) || fatal "domain melebihi 253 karakter"
  [[ "$domain" == *.* && "$domain" != *:* && "$domain" != */* ]] || fatal "gunakan FQDN tanpa skema, slash, atau port"
  IFS='.' read -r -a labels <<<"$domain"
  ((${#labels[@]} >= 2)) || fatal "domain harus memiliki minimal dua label"
  for label in "${labels[@]}"; do
    ((${#label} <= 63)) || fatal "label domain melebihi 63 karakter"
    [[ "$label" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]] || fatal "label domain tidak valid: $label"
  done
}

validate_admin_username() {
  local username=$1
  [[ "$username" =~ ^[A-Za-z0-9][A-Za-z0-9._@-]{2,99}$ ]] || fatal "username Super Admin harus 3-100 karakter dan hanya memakai huruf, angka, . _ @ -"
}

confirm_low_resources() {
  local message=$1
  if [[ "$ALLOW_LOW_RESOURCES" == true ]]; then
    warn "$message; dilanjutkan karena --allow-low-resources"
    return
  fi
  if [[ "$NON_INTERACTIVE" == true ]]; then
    fatal "$message; gunakan --allow-low-resources bila sudah memahami risikonya"
  fi
  warn "$message"
  local answer
  read -r -p 'Tetap lanjutkan? [y/N]: ' answer </dev/tty
  [[ "$answer" =~ ^[Yy]$ ]] || fatal "instalasi dibatalkan"
}

preflight() {
  local os_id version_id architecture memory_mb free_disk_mb
  os_id=$(awk -F= '$1 == "ID" {gsub(/"/, "", $2); print $2}' /etc/os-release)
  version_id=$(awk -F= '$1 == "VERSION_ID" {gsub(/"/, "", $2); print $2}' /etc/os-release)
  [[ "$os_id" == "ubuntu" && "$version_id" == "24.04" ]] || fatal "hanya Ubuntu Server 24.04 LTS yang didukung"
  [[ -d /run/systemd/system ]] || fatal "systemd tidak aktif"
  architecture=$(dpkg --print-architecture)
  [[ "$architecture" == "amd64" || "$architecture" == "arm64" ]] || fatal "arsitektur $architecture tidak didukung"
  memory_mb=$(awk '/MemTotal/ {print int($2 / 1024)}' /proc/meminfo)
  free_disk_mb=$(df -Pm "$APP_DIR" | awk 'NR == 2 {print $4}')
  ((memory_mb >= 3800)) || confirm_low_resources "RAM ${memory_mb} MB di bawah rekomendasi 4 GB"
  ((free_disk_mb >= 20000)) || confirm_low_resources "disk kosong ${free_disk_mb} MB di bawah minimum operasional 20 GB"
  getent ahosts archive.ubuntu.com >/dev/null || fatal "DNS/koneksi internet belum berfungsi"
  GIT_COMMIT=$(git_safe rev-parse HEAD)
  if [[ -n "$(git_safe status --porcelain --untracked-files=all)" ]]; then
    fatal "worktree tidak bersih (termasuk file untracked); deploy hanya dari commit/tag yang sudah diuji"
  fi
  validate_tracked_release_files
}

install_base_system() {
  export DEBIAN_FRONTEND=noninteractive
  export NEEDRESTART_MODE=a
  run_logged apt-get update
  if [[ "$EXISTING_INSTALL" == false ]]; then
    run_logged apt-get upgrade -y
  fi
  run_logged apt-get install -y ca-certificates curl git gnupg jq openssl ufw fail2ban dnsutils unattended-upgrades iproute2 sudo
  timedatectl set-timezone "$TIMEZONE"
  systemctl enable --now systemd-timesyncd.service >>"$LOG_FILE" 2>&1 || true
  run_logged dpkg-reconfigure -f noninteractive unattended-upgrades
  curl -fsS --max-time 15 https://download.docker.com/ >/dev/null || fatal "repository Docker tidak dapat diakses"
}

copy_authorized_keys() {
  local caller_user caller_home source_keys target_dir target_keys key
  caller_user=${SUDO_USER:-root}
  caller_home=$(getent passwd "$caller_user" | cut -d: -f6)
  source_keys="$caller_home/.ssh/authorized_keys"
  target_dir="$DEPLOY_HOME/.ssh"
  target_keys="$target_dir/authorized_keys"
  install -d -o "$DEPLOY_USER" -g "$DEPLOY_USER" -m 0700 "$target_dir"
  touch "$target_keys"
  chmod 0600 "$target_keys"
  if [[ -s "$source_keys" && "$source_keys" != "$target_keys" ]]; then
    while IFS= read -r key; do
      [[ -n "$key" ]] || continue
      grep -qxF "$key" "$target_keys" || printf '%s\n' "$key" >>"$target_keys"
    done <"$source_keys"
  fi
  chown -R "$DEPLOY_USER:$DEPLOY_USER" "$target_dir"
  if [[ ! -s "$target_keys" ]]; then
    warn "tidak ada SSH public key yang dapat disalin ke user $DEPLOY_USER"
  fi
}

ensure_deploy_user() {
  local deploy_uid sudoers_temp
  if ! id "$DEPLOY_USER" >/dev/null 2>&1; then
    useradd --create-home --shell /bin/bash "$DEPLOY_USER"
    passwd -l "$DEPLOY_USER" >>"$LOG_FILE" 2>&1
  fi
  deploy_uid=$(id -u "$DEPLOY_USER")
  ((deploy_uid >= 1000)) || fatal "deploy user harus merupakan user manusia dengan UID minimal 1000"
  DEPLOY_HOME=$(getent passwd "$DEPLOY_USER" | cut -d: -f6)
  [[ -d "$DEPLOY_HOME" ]] || fatal "home deploy user tidak ditemukan: $DEPLOY_HOME"
  usermod -aG sudo "$DEPLOY_USER"
  copy_authorized_keys
  sudoers_temp=$(mktemp)
  TEMP_FILES+=("$sudoers_temp")
  printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$DEPLOY_USER" >"$sudoers_temp"
  chmod 0440 "$sudoers_temp"
  visudo -cf "$sudoers_temp" >>"$LOG_FILE" 2>&1
  install -o root -g root -m 0440 "$sudoers_temp" "/etc/sudoers.d/90-isp-billing-$DEPLOY_USER"
  chown -R "$DEPLOY_USER:$DEPLOY_USER" "$APP_DIR"
}

install_docker() {
  local package codename architecture
  if ! dpkg-query -W -f='${Status}' docker-ce 2>/dev/null | grep -q 'install ok installed'; then
    for package in docker.io docker-compose docker-compose-v2 docker-doc podman-docker containerd runc; do
      if dpkg-query -W -f='${Status}' "$package" 2>/dev/null | grep -q 'install ok installed'; then
        run_logged apt-get remove -y "$package"
      fi
    done
  fi
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  codename=$(awk -F= '$1 == "VERSION_CODENAME" {gsub(/"/, "", $2); print $2}' /etc/os-release)
  architecture=$(dpkg --print-architecture)
  cat > /etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: $codename
Components: stable
Architectures: $architecture
Signed-By: /etc/apt/keyrings/docker.asc
EOF
  run_logged apt-get update
  run_logged apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  systemctl enable --now docker >>"$LOG_FILE" 2>&1
  usermod -aG docker "$DEPLOY_USER"
  validate_local_docker
  run_logged docker compose version
}

endpoint_port() {
  local endpoint=$1
  endpoint=${endpoint##*]:}
  endpoint=${endpoint##*:}
  [[ "$endpoint" =~ ^[0-9]+$ ]] && printf '%s' "$endpoint"
}

port_is_listening() {
  local wanted_port=$1 endpoint candidate
  while IFS= read -r endpoint; do
    candidate=$(endpoint_port "$endpoint" || true)
    [[ "$candidate" == "$wanted_port" ]] && return 0
  done < <(ss -H -ltn | awk '{print $4}')
  return 1
}

detect_ssh_ports() {
  local ports=() port endpoint
  if [[ -n "$SSH_PORT_OVERRIDE" ]]; then
    port_is_listening "$SSH_PORT_OVERRIDE" || fatal "--ssh-port $SSH_PORT_OVERRIDE tidak sedang listen"
    ports+=("$SSH_PORT_OVERRIDE")
  fi
  if [[ -n "${SSH_CONNECTION:-}" ]]; then
    port=$(awk '{print $4}' <<<"$SSH_CONNECTION")
    if [[ "$port" =~ ^[0-9]+$ ]] && port_is_listening "$port"; then
      ports+=("$port")
    fi
  fi
  while IFS= read -r endpoint; do
    port=$(endpoint_port "$endpoint" || true)
    [[ -n "$port" ]] && ports+=("$port")
  done < <(ss -H -tnp state established 2>/dev/null | awk '$0 ~ /"sshd"/ {print $4}')
  if command -v sshd >/dev/null 2>&1; then
    while IFS= read -r port; do
      if [[ "$port" =~ ^[0-9]+$ ]] && port_is_listening "$port"; then
        ports+=("$port")
      fi
    done < <(sshd -T 2>/dev/null | awk '$1 == "port" {print $2}')
  fi
  ((${#ports[@]} > 0)) || fatal "port SSH aktif tidak dapat dibuktikan; gunakan --ssh-port PORT setelah memastikan listener aktif"
  printf '%s\n' "${ports[@]}" | sort -nu
}

configure_firewall() {
  local ssh_ports=() port joined_ports jail_temp
  mapfile -t ssh_ports < <(detect_ssh_ports)
  {
    ufw default deny incoming
    ufw default allow outgoing
    for port in "${ssh_ports[@]}"; do
      ufw allow "$port/tcp" comment 'SSH aktif'
    done
    ufw allow 80/tcp comment 'HTTP dan ACME'
    ufw allow 443/tcp comment 'HTTPS'
    ufw allow 443/udp comment 'HTTP3'
    ufw --force enable
  } >>"$LOG_FILE"
  joined_ports=$(IFS=,; printf '%s' "${ssh_ports[*]}")
  jail_temp=$(mktemp)
  TEMP_FILES+=("$jail_temp")
  cat >"$jail_temp" <<EOF
[sshd]
enabled = true
port = $joined_ports
maxretry = 5
findtime = 10m
bantime = 1h
EOF
  install -o root -g root -m 0644 "$jail_temp" /etc/fail2ban/jail.d/sshd.local
  systemctl enable --now fail2ban >>"$LOG_FILE" 2>&1
  systemctl restart fail2ban
  ufw status verbose >>"$LOG_FILE"
}

own_gateway_running() {
  local container_id
  container_id=$(compose ps -q gateway 2>/dev/null || true)
  [[ -n "$container_id" && "$(docker inspect -f '{{.State.Running}}' "$container_id" 2>/dev/null)" == "true" ]]
}

check_host_ports() {
  local occupied=false
  if ss -H -ltn | awk '{print $4}' | grep -Eq '(^|:)(80|443)$'; then
    occupied=true
  fi
  if ss -H -lun | awk '{print $4}' | grep -Eq '(^|:)443$'; then
    occupied=true
  fi
  if [[ "$occupied" == true ]] && ! own_gateway_running; then
    ss -lntup >>"$LOG_FILE" 2>&1 || true
    fatal "port 80/443 sedang digunakan service lain"
  fi
}

check_dns() {
  local a_records=() aaaa_records=() host_ipv6=() record matched
  PUBLIC_IPV4=$(curl -4fsS --max-time 15 https://api.ipify.org || curl -4fsS --max-time 15 https://icanhazip.com)
  PUBLIC_IPV4=${PUBLIC_IPV4//$'\n'/}
  [[ "$PUBLIC_IPV4" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || fatal "IPv4 publik VPS tidak dapat ditentukan"
  mapfile -t a_records < <(dig +short A "$APP_DOMAIN" @1.1.1.1 | sed '/^$/d' | sort -u)
  ((${#a_records[@]} > 0)) || fatal "record A $APP_DOMAIN belum tersedia; arahkan ke $PUBLIC_IPV4 lalu jalankan ulang"
  for record in "${a_records[@]}"; do
    [[ "$record" == "$PUBLIC_IPV4" ]] || fatal "record A $APP_DOMAIN mengarah ke $record, seharusnya $PUBLIC_IPV4 (Cloudflare gunakan DNS only)"
  done

  mapfile -t aaaa_records < <(dig +short AAAA "$APP_DOMAIN" @1.1.1.1 | sed '/^$/d' | sort -u)
  if ((${#aaaa_records[@]} > 0)); then
    mapfile -t host_ipv6 < <(ip -6 -o addr show scope global | awk '{sub(/\/.*/, "", $4); print $4}')
    for record in "${aaaa_records[@]}"; do
      matched=false
      local address
      for address in "${host_ipv6[@]:-}"; do
        [[ "$record" == "$address" ]] && matched=true
      done
      [[ "$matched" == true ]] || fatal "record AAAA $record tidak tersedia pada VPS; hapus AAAA atau konfigurasikan IPv6"
    done
  fi
}

compose_project_name() {
  basename "$APP_DIR" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]+//g; s/^[^a-z0-9]+//'
}

postgres_volume_exists() {
  postgres_volume_name >/dev/null
}

postgres_volume_name() {
  local expected_volume all_volumes_output labeled_output metadata volume expected_present=false
  local metadata_fields=() labeled_volumes=()
  expected_volume="${COMPOSE_PROJECT_NAME}_postgres_data"
  all_volumes_output=$(docker volume ls --format '{{.Name}}') || fatal "daftar volume Docker lokal tidak dapat dibaca"
  labeled_output=$(docker volume ls -q \
    --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME" \
    --filter 'label=com.docker.compose.volume=postgres_data') || fatal "daftar volume PostgreSQL berlabel Compose tidak dapat dibaca"
  while IFS= read -r volume; do
    [[ -n "$volume" ]] || continue
    [[ "$volume" == "$expected_volume" ]] && expected_present=true
  done <<<"$all_volumes_output"
  if [[ -n "$labeled_output" ]]; then
    mapfile -t labeled_volumes <<<"$labeled_output"
  fi
  if [[ "$expected_present" == false ]]; then
    ((${#labeled_volumes[@]} == 0)) || fatal "volume PostgreSQL berlabel Compose memakai nama yang tidak dikenali: ${labeled_volumes[*]}"
    return 1
  fi
  metadata=$(docker volume inspect --format '{{println (index .Labels "com.docker.compose.project")}}{{println (index .Labels "com.docker.compose.volume")}}{{println .Driver}}{{println .Scope}}{{json .Options}}' "$expected_volume") || fatal "metadata volume $expected_volume tidak dapat dibaca"
  mapfile -t metadata_fields <<<"$metadata"
  ((${#metadata_fields[@]} == 5)) || fatal "metadata volume $expected_volume tidak lengkap"
  [[ "${metadata_fields[0]}" == "$COMPOSE_PROJECT_NAME" && "${metadata_fields[1]}" == "postgres_data" ]] || fatal "volume $expected_volume ada tetapi label Compose tidak cocok; hentikan untuk recovery"
  ((${#labeled_volumes[@]} == 1)) && [[ "${labeled_volumes[0]}" == "$expected_volume" ]] || fatal "identitas volume PostgreSQL Compose ambigu"
  [[ "${metadata_fields[2]}" == "local" && "${metadata_fields[3]}" == "local" ]] || fatal "volume $expected_volume bukan volume Docker local"
  [[ "${metadata_fields[4]}" == "null" || "${metadata_fields[4]}" == "{}" ]] || fatal "volume $expected_volume memakai opsi driver yang tidak diizinkan"
  printf '%s' "$expected_volume"
}

postgres_volume_is_empty() {
  local volume_name=$1 mountpoint first_entry
  mountpoint=$(docker volume inspect --format '{{.Mountpoint}}' "$volume_name" 2>/dev/null) || fatal "mountpoint volume $volume_name tidak dapat dibaca"
  [[ -n "$mountpoint" && -d "$mountpoint" && ! -L "$mountpoint" ]] || fatal "mountpoint volume $volume_name tidak aman untuk diperiksa"
  if ! first_entry=$(find "$mountpoint" -mindepth 1 -maxdepth 1 -print -quit 2>>"$LOG_FILE"); then
    fatal "isi volume $volume_name tidak dapat diperiksa"
  fi
  [[ -z "$first_entry" ]]
}

postgres_volume_mountpoint() {
  local volume_name=$1 mountpoint
  mountpoint=$(docker volume inspect --format '{{.Mountpoint}}' "$volume_name") || fatal "mountpoint volume $volume_name tidak dapat dibaca"
  [[ -n "$mountpoint" && -d "$mountpoint" && ! -L "$mountpoint" ]] || fatal "mountpoint volume $volume_name tidak aman"
  printf '%s' "$mountpoint"
}

read_database_volume_id_from_volume() {
  local volume_name=$1 mountpoint marker lines=()
  mountpoint=$(postgres_volume_mountpoint "$volume_name")
  marker="$mountpoint/.isp-billing-volume-id"
  [[ -e "$marker" ]] || return 1
  [[ -f "$marker" && ! -L "$marker" ]] || fatal "marker identitas pada volume $volume_name bukan regular file"
  mapfile -t lines <"$marker" || fatal "marker identitas pada volume $volume_name tidak dapat dibaca"
  ((${#lines[@]} == 1)) || fatal "marker identitas pada volume $volume_name harus tepat satu baris"
  [[ "${lines[0]}" =~ ^[a-f0-9]{64}$ ]] || fatal "marker identitas pada volume $volume_name tidak valid"
  printf '%s' "${lines[0]}"
}

write_database_volume_id_to_volume() {
  local volume_name=$1 volume_id=$2 mountpoint marker pending extra_entry
  [[ "$volume_id" =~ ^[a-f0-9]{64}$ ]] || fatal "ID marker volume internal tidak valid"
  mountpoint=$(postgres_volume_mountpoint "$volume_name")
  marker="$mountpoint/.isp-billing-volume-id"
  pending="$mountpoint/.isp-billing-volume-id.pending"
  [[ ! -e "$marker" ]] || fatal "marker identitas volume sudah ada sebelum commit"
  if ! extra_entry=$(find "$mountpoint" -mindepth 1 -maxdepth 1 \
    ! -name '.isp-billing-volume-id.pending' -print -quit 2>>"$LOG_FILE"); then
    fatal "isi volume $volume_name tidak dapat diperiksa sebelum marker ditulis"
  fi
  [[ -z "$extra_entry" ]] || fatal "volume $volume_name tidak kosong sebelum marker ditulis"
  [[ ! -e "$pending" || (-f "$pending" && ! -L "$pending") ]] || fatal "file marker pending pada volume $volume_name tidak aman"
  umask 077
  printf '%s\n' "$volume_id" >"$pending"
  chmod 0644 "$pending"
  mv -T -- "$pending" "$marker"
  [[ "$(read_database_volume_id_from_volume "$volume_name")" == "$volume_id" ]] || fatal "verifikasi marker volume $volume_name gagal"
}

postgres_volume_has_cluster() {
  local volume_name=$1 mountpoint pg_version version
  mountpoint=$(postgres_volume_mountpoint "$volume_name")
  pg_version="$mountpoint/pgdata/PG_VERSION"
  [[ -e "$pg_version" ]] || return 1
  [[ -f "$pg_version" && ! -L "$pg_version" ]] || fatal "PG_VERSION pada volume $volume_name tidak aman"
  IFS= read -r version <"$pg_version" || fatal "PG_VERSION pada volume $volume_name tidak dapat dibaca"
  [[ "$version" == "17" ]] || fatal "volume $volume_name berisi PostgreSQL versi $version, expected 17"
}

postgres_volume_has_only_marker() {
  local volume_name=$1 mountpoint extra_entry
  mountpoint=$(postgres_volume_mountpoint "$volume_name")
  if ! extra_entry=$(find "$mountpoint" -mindepth 1 -maxdepth 1 \
    ! -name '.isp-billing-volume-id' ! -name '.isp-billing-volume-id.pending' \
    -print -quit 2>>"$LOG_FILE"); then
    fatal "isi volume $volume_name tidak dapat diperiksa"
  fi
  [[ -z "$extra_entry" ]]
}

postgres_container_count() {
  local containers
  containers=$(docker ps -aq \
    --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME" \
    --filter 'label=com.docker.compose.service=postgres') || fatal "daftar container PostgreSQL Compose tidak dapat dibaca"
  awk 'NF {count++} END {print count + 0}' <<<"$containers"
}

require_empty_uninitialized_volume() {
  local phase=$1 volume_name attached_containers compose_container_count
  [[ "$DATABASE_INITIALIZED" == false && "$EXISTING_INSTALL" == false ]] || fatal "volume database tidak boleh dianggap kosong setelah initialization ($phase)"
  postgres_volume_exists || fatal "volume PostgreSQL kosong dari instalasi terputus tidak ditemukan lagi ($phase)"
  volume_name="${COMPOSE_PROJECT_NAME}_postgres_data"
  compose_container_count=$(postgres_container_count)
  ((compose_container_count == 0)) || fatal "container PostgreSQL Compose muncul saat memverifikasi volume kosong ($phase)"
  attached_containers=$(docker ps -aq --filter "volume=$volume_name") || fatal "attachment container pada volume $volume_name tidak dapat diperiksa"
  [[ -z "$attached_containers" ]] || fatal "volume $volume_name sudah dipasang oleh container; hentikan untuk recovery ($phase)"
  postgres_volume_is_empty "$volume_name" || fatal "volume $volume_name tidak lagi kosong; hentikan untuk recovery ($phase)"
}

existing_postgres_container() {
  local output containers=()
  output=$(docker ps -aq \
    --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME" \
    --filter 'label=com.docker.compose.service=postgres') || fatal "daftar container PostgreSQL Compose tidak dapat dibaca"
  if [[ -n "$output" ]]; then
    mapfile -t containers <<<"$output"
  fi
  ((${#containers[@]} == 1)) || return 1
  printf '%s' "${containers[0]}"
}

wait_for_postgres_container() {
  local container_id=$1 elapsed=0 status
  while ((elapsed < 120)); do
    status=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container_id" 2>/dev/null || true)
    [[ "$status" == "healthy" ]] && return 0
    [[ "$status" == "unhealthy" || "$status" == "exited" || "$status" == "dead" ]] && return 1
    sleep 3
    elapsed=$((elapsed + 3))
  done
  return 1
}

validate_postgres_container_mount() {
  local container_id=$1 volume_name=$2 mounted_volume attached_output attached=()
  mounted_volume=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}' "$container_id") || fatal "mount container PostgreSQL tidak dapat dibaca"
  [[ "$mounted_volume" == "$volume_name" ]] || fatal "container PostgreSQL tidak memasang volume $volume_name"
  attached_output=$(docker ps -aq --filter "volume=$volume_name") || fatal "attachment volume $volume_name tidak dapat dibaca"
  if [[ -n "$attached_output" ]]; then
    mapfile -t attached <<<"$attached_output"
  fi
  ((${#attached[@]} == 1)) && [[ "${attached[0]}" == "$container_id" ]] || fatal "volume $volume_name dipasang oleh container yang tidak dikenali"
}

ready_existing_postgres_container() {
  local volume_name=${1:-"${COMPOSE_PROJECT_NAME}_postgres_data"} container_id
  container_id=$(existing_postgres_container) || return 1
  validate_postgres_container_mount "$container_id" "$volume_name"
  if [[ "$(docker inspect -f '{{.State.Running}}' "$container_id")" != "true" ]]; then
    run_logged docker start "$container_id"
  fi
  wait_for_postgres_container "$container_id" || return 1
  printf '%s' "$container_id"
}

postgres_container_volume_name() {
  docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}' "$1" || fatal "mount container PostgreSQL tidak dapat dibaca"
}

read_database_volume_id() {
  docker exec --user postgres "$1" sh -ec '
    marker=/var/lib/postgresql/data/.isp-billing-volume-id
    test -f "$marker" && test ! -L "$marker"
    cat "$marker"
  '
}

verify_database_identity() {
  local volume_name container_id mounted_volume actual_id
  [[ "$DATABASE_INITIALIZED" == true ]] || fatal "verifikasi identitas database dipanggil sebelum initialization"
  volume_name=$(postgres_volume_name) || fatal "volume PostgreSQL yang tercatat tidak ditemukan"
  [[ "$volume_name" == "$DATABASE_VOLUME_NAME" ]] || fatal "volume PostgreSQL berubah: expected $DATABASE_VOLUME_NAME, ditemukan $volume_name"
  actual_id=$(read_database_volume_id_from_volume "$volume_name") || fatal "marker identitas volume PostgreSQL hilang"
  [[ "$actual_id" == "$DATABASE_VOLUME_ID" ]] || fatal "identitas volume PostgreSQL berubah; hentikan untuk recovery data"
  container_id=$(ready_existing_postgres_container "$volume_name") || fatal "container PostgreSQL untuk volume tercatat tidak healthy"
  mounted_volume=$(postgres_container_volume_name "$container_id")
  [[ "$mounted_volume" == "$DATABASE_VOLUME_NAME" ]] || fatal "container PostgreSQL tidak memasang volume yang tercatat"
  actual_id=$(read_database_volume_id "$container_id") || fatal "marker identitas volume PostgreSQL hilang atau tidak dapat dibaca"
  [[ "$actual_id" =~ ^[a-f0-9]{64}$ ]] || fatal "marker identitas volume PostgreSQL tidak valid"
  [[ "$actual_id" == "$DATABASE_VOLUME_ID" ]] || fatal "identitas volume PostgreSQL berubah; hentikan untuk recovery data"
}

verify_database_volume_offline() {
  local volume_name actual_id compose_count attached_output container_id
  [[ -n "$DATABASE_VOLUME_NAME" && "$DATABASE_VOLUME_ID" =~ ^[a-f0-9]{64}$ ]] || fatal "expected identity database belum tersedia"
  volume_name=$(postgres_volume_name) || fatal "volume PostgreSQL yang tercatat tidak ditemukan"
  [[ "$volume_name" == "$DATABASE_VOLUME_NAME" ]] || fatal "nama volume PostgreSQL berubah"
  actual_id=$(read_database_volume_id_from_volume "$volume_name") || fatal "marker identitas volume PostgreSQL hilang"
  [[ "$actual_id" == "$DATABASE_VOLUME_ID" ]] || fatal "marker identitas volume PostgreSQL berbeda; hentikan untuk recovery data"
  if ! postgres_volume_has_cluster "$volume_name" && ! postgres_volume_has_only_marker "$volume_name"; then
    fatal "volume PostgreSQL berisi initialization parsial yang tidak dapat dipulihkan otomatis"
  fi
  compose_count=$(postgres_container_count)
  ((compose_count <= 1)) || fatal "lebih dari satu container PostgreSQL Compose ditemukan"
  attached_output=$(docker ps -aq --filter "volume=$volume_name") || fatal "attachment volume $volume_name tidak dapat dibaca"
  if ((compose_count == 0)); then
    [[ -z "$attached_output" ]] || fatal "volume $volume_name dipasang oleh container yang tidak dikenali"
    return
  fi
  container_id=$(existing_postgres_container) || fatal "container PostgreSQL Compose tidak dapat diidentifikasi"
  validate_postgres_container_mount "$container_id" "$volume_name"
}

create_postgres_volume() {
  local volume_name compose_version created
  volume_name="${COMPOSE_PROJECT_NAME}_postgres_data"
  compose_version=$(docker compose version --short) || fatal "versi Docker Compose tidak dapat dibaca"
  created=$(docker volume create --driver local \
    --label "com.docker.compose.project=$COMPOSE_PROJECT_NAME" \
    --label "com.docker.compose.version=$compose_version" \
    --label 'com.docker.compose.volume=postgres_data' \
    "$volume_name") || fatal "volume PostgreSQL tidak dapat dibuat"
  [[ "$created" == "$volume_name" ]] || fatal "Docker membuat nama volume PostgreSQL yang tidak dikenali: $created"
  postgres_volume_exists || fatal "volume PostgreSQL tidak ditemukan setelah dibuat"
}

prepare_database_identity() {
  local volume_name actual_id="" compose_count attached_output
  volume_name="${COMPOSE_PROJECT_NAME}_postgres_data"

  if [[ "$DATABASE_INITIALIZED" == true ]]; then
    verify_database_volume_offline
    return
  fi

  if [[ -z "$DATABASE_VOLUME_ID" ]]; then
    if ! postgres_volume_exists; then
      create_postgres_volume
    fi
    require_empty_uninitialized_volume "sebelum memilih identitas volume"
    DATABASE_VOLUME_NAME=$volume_name
    DATABASE_VOLUME_ID=$(openssl rand -hex 32)
    write_state installing
  else
    [[ "$DATABASE_VOLUME_NAME" == "$volume_name" ]] || fatal "nama volume pending berbeda dari project Compose"
    postgres_volume_exists || fatal "volume dengan identitas pending tidak ditemukan"
  fi

  if actual_id=$(read_database_volume_id_from_volume "$volume_name"); then
    [[ "$actual_id" == "$DATABASE_VOLUME_ID" ]] || fatal "marker volume berbeda dari expected ID pending"
  else
    compose_count=$(postgres_container_count)
    ((compose_count == 0)) || fatal "container PostgreSQL muncul sebelum marker volume committed"
    attached_output=$(docker ps -aq --filter "volume=$volume_name") || fatal "attachment volume $volume_name tidak dapat diperiksa"
    [[ -z "$attached_output" ]] || fatal "volume $volume_name dipasang sebelum marker committed"
    postgres_volume_has_only_marker "$volume_name" || fatal "volume $volume_name tidak kosong saat memulihkan marker pending"
    write_database_volume_id_to_volume "$volume_name" "$DATABASE_VOLUME_ID"
  fi

  DATABASE_INITIALIZED=true
  write_state installing
  verify_database_volume_offline
}

backup_existing_database_early() {
  local container_id timestamp backup_file temporary owner_group
  container_id=$(ready_existing_postgres_container) || fatal "volume PostgreSQL ada tetapi container existing tidak healthy; pulihkan container sebelum rerun"

  install -d -o "$DEPLOY_USER" -g "$DEPLOY_USER" -m 0700 "$BACKUP_DIR"
  timestamp=$(date -u +%Y%m%dT%H%M%SZ)
  backup_file="$BACKUP_DIR/isp_billing_preinstall_${timestamp}.dump"
  temporary=$(mktemp "$BACKUP_DIR/.isp_billing_preinstall_${timestamp}.XXXXXX.dump")
  TEMP_FILES+=("$temporary")
  if ! docker exec "$container_id" sh -ec '
    export PGPASSWORD="$POSTGRES_PASSWORD"
    exec pg_dump \
      --username "$POSTGRES_USER" \
      --dbname "$POSTGRES_DB" \
      --format custom \
      --no-owner \
      --no-privileges
  ' >"$temporary"; then
    fatal "backup database existing gagal; paket dan container belum diubah"
  fi
  [[ -s "$temporary" ]] || fatal "backup database existing kosong"
  mv -- "$temporary" "$backup_file"
  sha256sum "$backup_file" >"$backup_file.sha256.tmp"
  mv -- "$backup_file.sha256.tmp" "$backup_file.sha256"
  owner_group="$DEPLOY_USER:$DEPLOY_USER"
  chown "$owner_group" "$backup_file" "$backup_file.sha256"
  chmod 0600 "$backup_file" "$backup_file.sha256"
  EARLY_BACKUP_DONE=true
  ui "Backup pre-install tersimpan: $backup_file"
}

protect_existing_database() {
  local docker_available=false volume_exists=false volume_name="" container_count=0
  if [[ ("$EXISTING_INSTALL" == true || "$DATABASE_INITIALIZED" == true || -n "$DATABASE_VOLUME_ID") && "$EXISTING_ENV" == false ]]; then
    fatal "state mencatat database existing tetapi .env hilang; pulihkan .env sebelum rerun"
  fi

  if command -v docker >/dev/null 2>&1; then
    docker_available=true
    systemctl start docker >>"$LOG_FILE" 2>&1 || fatal "Docker existing tidak dapat dijalankan untuk pemeriksaan backup"
    validate_local_docker
    docker compose version >>"$LOG_FILE" 2>&1 || fatal "Docker Compose plugin existing tidak tersedia"
    if postgres_volume_exists; then
      volume_exists=true
      volume_name="${COMPOSE_PROJECT_NAME}_postgres_data"
    fi
    container_count=$(postgres_container_count)
    ((container_count <= 1)) || fatal "lebih dari satu container PostgreSQL ditemukan untuk project Compose"
    if ((container_count == 1)) && [[ "$volume_exists" == false ]]; then
      fatal "container PostgreSQL ada tetapi volume Compose yang valid tidak ditemukan"
    fi
  fi

  if [[ "$EXISTING_INSTALL" == true && "$docker_available" == false ]]; then
    fatal "instalasi complete tetapi Docker tidak ditemukan"
  fi
  if [[ ("$DATABASE_INITIALIZED" == true || -n "$DATABASE_VOLUME_ID") && "$docker_available" == false ]]; then
    fatal "state mencatat identitas database tetapi Docker tidak ditemukan"
  fi
  if [[ "$EXISTING_ENV" == true && "$docker_available" == false ]]; then
    fatal ".env existing tetapi Docker tidak tersedia; pulihkan Docker agar database dapat diperiksa dan dibackup"
  fi
  if [[ ("$EXISTING_INSTALL" == true || "$DATABASE_INITIALIZED" == true || -n "$DATABASE_VOLUME_ID") && "$volume_exists" == false ]]; then
    fatal "state mencatat database existing tetapi volume PostgreSQL tidak ditemukan; hentikan untuk recovery data"
  fi
  [[ "$volume_exists" == true ]] || {
    ((container_count == 0)) || fatal "container PostgreSQL ada tetapi volume Compose yang valid tidak ditemukan"
    return
  }

  if [[ -z "$DATABASE_VOLUME_ID" ]]; then
    ((container_count == 0)) || fatal "container PostgreSQL ditemukan tanpa expected identity di installer-state; hentikan untuk recovery"
    require_empty_uninitialized_volume "pemeriksaan awal"
    EMPTY_UNINITIALIZED_VOLUME=true
    ui "Volume PostgreSQL kosong dari instalasi terputus akan digunakan kembali."
    return
  fi

  prepare_database_identity
  verify_database_volume_offline
  if postgres_volume_has_cluster "$volume_name"; then
    [[ "$EXISTING_ENV" == true ]] || fatal "cluster PostgreSQL existing tidak memiliki .env untuk backup"
    ((container_count == 1)) || fatal "cluster PostgreSQL existing tidak memiliki container yang dapat diverifikasi untuk backup"
    backup_existing_database_early
  elif ! postgres_volume_has_only_marker "$volume_name"; then
    fatal "volume PostgreSQL memiliki isi parsial tanpa cluster yang valid"
  fi
}

create_or_validate_environment() {
  if [[ "$EXISTING_ENV" == true ]]; then
    chown "$DEPLOY_USER:$DEPLOY_USER" "$ENV_FILE"
    chmod 0600 "$ENV_FILE"
    return
  fi
  if postgres_volume_exists; then
    [[ "$EMPTY_UNINITIALIZED_VOLUME" == true ]] || fatal "volume PostgreSQL sudah ada tetapi .env hilang; pulihkan .env dari backup, jangan generate password baru"
    require_empty_uninitialized_volume "sebelum membuat .env"
  elif [[ "$EMPTY_UNINITIALIZED_VOLUME" == true ]]; then
    fatal "volume PostgreSQL kosong dari instalasi terputus hilang sebelum .env dibuat"
  fi
  local owner_password app_password encryption_key temporary
  owner_password=$(openssl rand -hex 32)
  app_password=$(openssl rand -hex 32)
  encryption_key=$(openssl rand -hex 32)
  [[ "$owner_password" != "$app_password" ]] || fatal "generator menghasilkan password database yang sama"
  temporary=$(mktemp "$APP_DIR/.env.tmp.XXXXXX")
  TEMP_FILES+=("$temporary")
  cat >"$temporary" <<EOF
APP_DOMAIN=$APP_DOMAIN
ACME_EMAIL=$ACME_EMAIL
POSTGRES_DB=isp_billing
POSTGRES_USER=isp_billing_owner
POSTGRES_PASSWORD=$owner_password
POSTGRES_APP_USER=isp_billing_app
POSTGRES_APP_PASSWORD=$app_password
APP_ENCRYPTION_KEY=$encryption_key
EOF
  chown "$DEPLOY_USER:$DEPLOY_USER" "$temporary"
  chmod 0600 "$temporary"
  mv -f -- "$temporary" "$ENV_FILE"
  owner_password=""
  app_password=""
  encryption_key=""
  EXISTING_ENV=true
}

install_backup_automation() {
  local backup_temp service_temp timer_temp
  install -d -o "$DEPLOY_USER" -g "$DEPLOY_USER" -m 0700 "$BACKUP_DIR"
  backup_temp=$(mktemp)
  service_temp=$(mktemp)
  timer_temp=$(mktemp)
  TEMP_FILES+=("$backup_temp" "$service_temp" "$timer_temp")
  cat >"$backup_temp" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

unset DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH COMPOSE_FILE COMPOSE_PROFILES COMPOSE_ENV_FILES
export DOCKER_HOST=unix:///var/run/docker.sock

APP_DIR="__APP_DIR__"
BACKUP_DIR="/var/backups/isp-billing"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP_FILE="$BACKUP_DIR/isp_billing_${TIMESTAMP}.dump"
TEMP_FILE="$(mktemp "$BACKUP_DIR/.isp_billing_${TIMESTAMP}.XXXXXX.dump")"

cleanup() {
  rm -f -- "$TEMP_FILE" "$BACKUP_FILE.sha256.tmp"
}
trap cleanup EXIT

cd "$APP_DIR"
docker compose \
  --project-directory "$APP_DIR" \
  --file "$APP_DIR/compose.yaml" \
  --env-file "$APP_DIR/.env" \
  --project-name "__COMPOSE_PROJECT_NAME__" \
  exec -T postgres sh -ec '
  export PGPASSWORD="$POSTGRES_PASSWORD"
  exec pg_dump \
    --username "$POSTGRES_USER" \
    --dbname "$POSTGRES_DB" \
    --format custom \
    --no-owner \
    --no-privileges
' >"$TEMP_FILE"

test -s "$TEMP_FILE"
mv -- "$TEMP_FILE" "$BACKUP_FILE"
sha256sum "$BACKUP_FILE" >"$BACKUP_FILE.sha256.tmp"
mv -- "$BACKUP_FILE.sha256.tmp" "$BACKUP_FILE.sha256"
find "$BACKUP_DIR" -type f -name '*.dump' -mtime +14 -delete
find "$BACKUP_DIR" -type f -name '*.dump.sha256' -mtime +14 -delete
trap - EXIT
printf 'Backup selesai: %s\n' "$BACKUP_FILE"
EOF
  sed -i "s|__APP_DIR__|$APP_DIR|g" "$backup_temp"
  sed -i "s|__COMPOSE_PROJECT_NAME__|$COMPOSE_PROJECT_NAME|g" "$backup_temp"
  install -o root -g root -m 0755 "$backup_temp" /usr/local/sbin/isp-billing-backup

  cat >"$service_temp" <<EOF
[Unit]
Description=Backup PostgreSQL ISP Billing
Requires=docker.service
After=docker.service

[Service]
Type=oneshot
User=$DEPLOY_USER
Group=$DEPLOY_USER
SupplementaryGroups=docker
WorkingDirectory=$APP_DIR
ExecStart=/usr/local/sbin/isp-billing-backup
PrivateTmp=true
NoNewPrivileges=true
EOF
  install -o root -g root -m 0644 "$service_temp" /etc/systemd/system/isp-billing-backup.service

  cat >"$timer_temp" <<EOF
[Unit]
Description=Jadwal backup harian ISP Billing

[Timer]
OnCalendar=*-*-* 02:30:00 $TIMEZONE
Persistent=true
RandomizedDelaySec=10m

[Install]
WantedBy=timers.target
EOF
  install -o root -g root -m 0644 "$timer_temp" /etc/systemd/system/isp-billing-backup.timer
  systemctl daemon-reload
  systemctl enable --now isp-billing-backup.timer >>"$LOG_FILE" 2>&1
}

postgres_is_healthy() {
  local container_id
  container_id=$(compose ps -q postgres 2>/dev/null || true)
  [[ -n "$container_id" ]] || return 1
  [[ "$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container_id" 2>/dev/null)" == "healthy" ]]
}

backup_before_update() {
  if [[ "$EARLY_BACKUP_DONE" == true ]]; then
    ui "Backup pre-update sudah dibuat sebelum perubahan host."
    return
  fi
  if postgres_is_healthy; then
    run_logged systemctl start isp-billing-backup.service
    ui "Backup pre-update selesai."
  else
    ui "Database existing belum berjalan; backup pre-update tidak diperlukan."
  fi
}

validate_compose() {
  local actual expected
  compose config --quiet >>"$LOG_FILE" 2>&1
  actual=$(compose config --format json | jq -r '
    .services | to_entries[] | .key as $service | (.value.ports // [])[]? |
    "\($service):\(.published):\(.target):\(.protocol)"
  ' | sort)
  expected=$'gateway:443:443:tcp\ngateway:443:443:udp\ngateway:80:80:tcp'
  [[ "$actual" == "$expected" ]] || {
    printf 'Port Compose aktual:\n%s\n' "$actual" >>"$LOG_FILE"
    fatal "hanya gateway 80/443 yang boleh dipublikasikan"
  }
}

deploy_stack() {
  if ! compose pull postgres gateway >>"$LOG_FILE" 2>&1; then
    fatal "gagal menarik image PostgreSQL/Caddy"
  fi
  if ! compose build --pull >>"$LOG_FILE" 2>&1; then
    fatal "build API/Next.js gagal"
  fi
  prepare_database_identity
  verify_database_volume_offline
  if ! compose up -d postgres >>"$LOG_FILE" 2>&1; then
    compose logs --tail=100 postgres >>"$LOG_FILE" 2>&1 || true
    fatal "gagal menjalankan PostgreSQL"
  fi
  wait_for_service postgres 180
  verify_database_identity
  if ! compose up -d --remove-orphans >>"$LOG_FILE" 2>&1; then
    { compose ps -a; compose logs --tail=100; } >>"$LOG_FILE" 2>&1 || true
    fatal "gagal menjalankan stack"
  fi
}

service_health() {
  local service=$1 container_id
  container_id=$(compose ps -q "$service" 2>/dev/null || true)
  [[ -n "$container_id" ]] || {
    printf 'missing'
    return
  }
  docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container_id" 2>/dev/null || printf 'unknown'
}

wait_for_service() {
  local service=$1 timeout_seconds=$2 elapsed=0 status
  while ((elapsed < timeout_seconds)); do
    status=$(service_health "$service")
    [[ "$status" == "healthy" ]] && return 0
    [[ "$status" == "unhealthy" || "$status" == "exited" ]] && break
    sleep 5
    elapsed=$((elapsed + 5))
  done
  { compose ps -a; compose logs --tail=100 "$service"; } >>"$LOG_FILE" 2>&1 || true
  fatal "service $service tidak healthy (status: $status)"
}

verify_internal_stack() {
  local migrate_id migrate_status migrate_exit
  wait_for_service postgres 180
  migrate_id=$(compose ps -a -q migrate 2>/dev/null || true)
  [[ -n "$migrate_id" ]] || fatal "container migration tidak ditemukan"
  migrate_status=$(docker inspect -f '{{.State.Status}}' "$migrate_id")
  migrate_exit=$(docker inspect -f '{{.State.ExitCode}}' "$migrate_id")
  [[ "$migrate_status" == "exited" && "$migrate_exit" == "0" ]] || {
    compose logs --no-log-prefix migrate >>"$LOG_FILE" 2>&1 || true
    fatal "migration gagal (status $migrate_status, exit $migrate_exit)"
  }
  wait_for_service api 180
  wait_for_service web 240
  run_logged compose exec -T api wget -qO- http://127.0.0.1:8080/health
  run_logged compose exec -T api wget -qO- http://127.0.0.1:8080/ready
  run_logged compose exec -T web node -e "fetch('http://127.0.0.1:3000/login').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
}

bootstrap_check() {
  if compose run --rm --no-deps --entrypoint /app/bootstrap-admin api --check >>"$LOG_FILE" 2>&1; then
    return 0
  else
    return $?
  fi
}

validate_password_file() {
  local mode owner lines
  [[ -f "$ADMIN_PASSWORD_FILE" && ! -L "$ADMIN_PASSWORD_FILE" ]] || fatal "password file harus regular file dan bukan symlink"
  mode=$(stat -c '%a' "$ADMIN_PASSWORD_FILE")
  owner=$(stat -c '%u' "$ADMIN_PASSWORD_FILE")
  [[ "$mode" == "600" && "$owner" == "0" ]] || fatal "password file harus owner root dan mode 600"
  mapfile -t lines <"$ADMIN_PASSWORD_FILE"
  ((${#lines[@]} == 1)) || fatal "password file harus berisi tepat satu baris"
  ADMIN_PASSWORD=${lines[0]}
}

collect_admin_credentials() {
  if [[ -z "$ADMIN_USERNAME" ]]; then
    if [[ "$NON_INTERACTIVE" == true ]]; then
      fatal "--admin-user wajib karena Super Admin belum ada"
    fi
    prompt_value ADMIN_USERNAME "Username Super Admin" "admin"
  fi
  validate_admin_username "$ADMIN_USERNAME"

  if [[ -n "$ADMIN_PASSWORD_FILE" ]]; then
    validate_password_file
  elif [[ "$GENERATE_ADMIN_PASSWORD" == true ]]; then
    ADMIN_PASSWORD=$(openssl rand -base64 36 | tr -d '\n')
    ADMIN_PASSWORD_GENERATED=true
  elif [[ "$NON_INTERACTIVE" == true ]]; then
    fatal "gunakan --admin-password-file atau --generate-admin-password"
  else
    local confirmation
    while true; do
      read -r -s -p 'Password Super Admin (minimal 20 karakter, Enter untuk generate): ' ADMIN_PASSWORD </dev/tty
      printf '\n'
      if [[ -z "$ADMIN_PASSWORD" ]]; then
        ADMIN_PASSWORD=$(openssl rand -base64 36 | tr -d '\n')
        ADMIN_PASSWORD_GENERATED=true
        break
      fi
      read -r -s -p 'Ulangi password Super Admin: ' confirmation </dev/tty
      printf '\n'
      [[ "$ADMIN_PASSWORD" == "$confirmation" ]] && break
      warn "konfirmasi password tidak cocok"
    done
  fi
  ((${#ADMIN_PASSWORD} >= 20)) || fatal "password Super Admin minimal 20 karakter"
}

current_admin_username() {
  # shellcheck disable=SC2016
  compose exec -T postgres sh -ec '
    export PGPASSWORD="$POSTGRES_PASSWORD"
    exec psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --tuples-only --no-align \
      --command "SELECT username FROM users WHERE role_code = '\''super_admin'\'' ORDER BY created_at LIMIT 1"
  ' | tr -d '\r\n'
}

read_admin_credential_value() {
  local key=$1 matches=()
  mapfile -t matches < <(grep -E "^${key}=" "$GENERATED_ADMIN_FILE" || true)
  ((${#matches[@]} == 1)) || fatal "key $key pada credential admin pending harus muncul tepat satu kali"
  printf '%s' "${matches[0]#*=}"
}

load_pending_admin_credentials() {
  [[ -f "$GENERATED_ADMIN_FILE" ]] || return 0
  [[ ! -L "$GENERATED_ADMIN_FILE" ]] || fatal "file credential admin tidak boleh berupa symlink"
  [[ "$(stat -c '%U:%a' "$GENERATED_ADMIN_FILE")" == "root:600" ]] || fatal "file credential admin harus owner root mode 600"
  if ! grep -q '^STATUS=' "$GENERATED_ADMIN_FILE"; then
    return 0
  fi

  local status stored_url stored_username stored_password
  status=$(read_admin_credential_value STATUS)
  [[ "$status" == "PENDING" || "$status" == "COMPLETE" ]] || fatal "status credential admin tidak valid"
  [[ "$status" == "PENDING" ]] || return 0
  stored_url=$(read_admin_credential_value URL)
  stored_username=$(read_admin_credential_value USERNAME)
  stored_password=$(read_admin_credential_value PASSWORD)
  [[ "$stored_url" == "https://$APP_DOMAIN" ]] || fatal "credential admin pending dibuat untuk $stored_url"
  validate_admin_username "$stored_username"
  ((${#stored_password} >= 20)) || fatal "password admin pending tidak valid"
  if [[ -n "$ADMIN_USERNAME" && "$ADMIN_USERNAME" != "$stored_username" ]]; then
    fatal "credential pending memakai username $stored_username, bukan $ADMIN_USERNAME"
  fi
  ADMIN_USERNAME=$stored_username
  ADMIN_PASSWORD=$stored_password
  ADMIN_PASSWORD_GENERATED=true
  ADMIN_PASSWORD_RECOVERED=true
}

persist_generated_admin_password() {
  local status=$1 temporary
  [[ "$status" == "PENDING" || "$status" == "COMPLETE" ]] || fatal "status credential admin tidak valid"
  temporary=$(mktemp /root/.isp-billing-initial-admin.XXXXXX)
  TEMP_FILES+=("$temporary")
  chmod 0600 "$temporary"
  {
    printf 'STATUS=%s\n' "$status"
    printf 'URL=%s\n' "https://$APP_DOMAIN"
    printf 'USERNAME=%s\n' "$ADMIN_USERNAME"
    printf 'PASSWORD=%s\n' "$ADMIN_PASSWORD"
    printf 'HAPUS_FILE_SETELAH_DISIMPAN_DI_PASSWORD_MANAGER=true\n'
  } >"$temporary"
  mv -f -- "$temporary" "$GENERATED_ADMIN_FILE"
  chown root:root "$GENERATED_ADMIN_FILE"
  chmod 0600 "$GENERATED_ADMIN_FILE"
}

run_bootstrap_with_credentials() {
  local mode=$1 exit_code arguments=()
  [[ "$mode" == "create" || "$mode" == "verify" ]] || fatal "mode bootstrap internal tidak valid"
  [[ "$mode" == "verify" ]] && arguments=(--verify)
  export BOOTSTRAP_ADMIN_USERNAME="$ADMIN_USERNAME"
  export BOOTSTRAP_ADMIN_PASSWORD="$ADMIN_PASSWORD"
  if compose run --rm --no-deps \
    -e BOOTSTRAP_ADMIN_USERNAME -e BOOTSTRAP_ADMIN_PASSWORD \
    --entrypoint /app/bootstrap-admin api "${arguments[@]}" >>"$LOG_FILE" 2>&1; then
    exit_code=0
  else
    exit_code=$?
  fi
  unset BOOTSTRAP_ADMIN_USERNAME BOOTSTRAP_ADMIN_PASSWORD
  return "$exit_code"
}

bootstrap_admin() {
  local check_exit create_exit existing_username
  load_pending_admin_credentials
  if bootstrap_check; then
    check_exit=0
  else
    check_exit=$?
  fi
  case "$check_exit" in
    0)
      existing_username=$(current_admin_username)
      if [[ "$ADMIN_PASSWORD_RECOVERED" == true ]]; then
        [[ "$ADMIN_USERNAME" == "$existing_username" ]] || fatal "Super Admin existing adalah $existing_username, credential pending untuk $ADMIN_USERNAME"
        if run_bootstrap_with_credentials verify; then
          ADMIN_CREATED=true
          persist_generated_admin_password COMPLETE
          ui "Credential Super Admin dari instalasi terputus berhasil dipulihkan."
        else
          create_exit=$?
          [[ "$create_exit" == "5" ]] || fatal "credential admin pending tidak dapat diverifikasi (exit $create_exit)"
          rm -f -- "$GENERATED_ADMIN_FILE"
          fatal "credential admin pending tidak cocok dengan akun existing; gunakan prosedur recovery admin"
        fi
      else
        ADMIN_USERNAME=$existing_username
        ui "Super Admin existing dipertahankan: $ADMIN_USERNAME"
      fi
      ;;
    3)
      if [[ "$ADMIN_PASSWORD_RECOVERED" == false ]]; then
        collect_admin_credentials
      fi
      [[ "$ADMIN_PASSWORD_GENERATED" == true ]] && persist_generated_admin_password PENDING
      if run_bootstrap_with_credentials create; then
        create_exit=0
      else
        create_exit=$?
      fi
      case "$create_exit" in
        0)
          ADMIN_CREATED=true
          if [[ "$ADMIN_PASSWORD_GENERATED" == true ]]; then
            persist_generated_admin_password COMPLETE
          else
            rm -f -- "$GENERATED_ADMIN_FILE"
          fi
          ;;
        4)
          if run_bootstrap_with_credentials verify; then
            ADMIN_CREATED=true
            [[ "$ADMIN_PASSWORD_GENERATED" == true ]] && persist_generated_admin_password COMPLETE
          else
            rm -f -- "$GENERATED_ADMIN_FILE"
            fatal "Super Admin dibuat bersamaan dengan credential berbeda; gunakan akun existing"
          fi
          ;;
        *)
          fatal "bootstrap Super Admin gagal (exit $create_exit)"
          ;;
      esac
      ;;
    *)
      fatal "status Super Admin tidak dapat diperiksa (exit $check_exit)"
      ;;
  esac
}

verify_public_https() {
  local elapsed=0 status_code location
  compose restart gateway >>"$LOG_FILE" 2>&1
  while ((elapsed < 360)); do
    if curl -fsS --max-time 15 "https://$APP_DOMAIN/health" >/dev/null 2>&1 \
      && curl -fsS --max-time 15 "https://$APP_DOMAIN/ready" >/dev/null 2>&1 \
      && [[ "$(curl -sS -o /dev/null -w '%{http_code}' --max-time 15 "https://$APP_DOMAIN/login" 2>/dev/null)" == "200" ]]; then
      status_code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 15 "http://$APP_DOMAIN" 2>/dev/null || true)
      location=$(curl -sSI --max-time 15 "http://$APP_DOMAIN" 2>/dev/null | awk 'tolower($1) == "location:" {gsub(/\r/, "", $2); print $2; exit}')
      if [[ "$status_code" =~ ^30[12378]$ && "$location" == https://* ]]; then
        return 0
      fi
    fi
    sleep 6
    elapsed=$((elapsed + 6))
    printf '.'
  done
  printf '\n'
  compose logs --tail=150 gateway api web >>"$LOG_FILE" 2>&1 || true
  fatal "HTTPS belum aktif setelah 6 menit; periksa DNS, firewall provider, serta log Caddy"
}

write_state() {
  local status=$1 temporary
  [[ "$status" == "installing" || "$status" == "complete" ]] || fatal "status state internal tidak valid"
  temporary=$(mktemp "$STATE_DIR/installer-state.tmp.XXXXXX")
  TEMP_FILES+=("$temporary")
  cat >"$temporary" <<EOF
STATUS=$status
INSTALLER_VERSION=$INSTALLER_VERSION
APP_DIR=$APP_DIR
APP_DOMAIN=$APP_DOMAIN
DEPLOY_USER=$DEPLOY_USER
GIT_COMMIT=$GIT_COMMIT
DATABASE_INITIALIZED=$DATABASE_INITIALIZED
DATABASE_VOLUME_NAME=$DATABASE_VOLUME_NAME
DATABASE_VOLUME_ID=$DATABASE_VOLUME_ID
COMPLETED_AT=$(date -Is)
EOF
  chown root:root "$temporary"
  chmod 0600 "$temporary"
  mv -T -- "$temporary" "$STATE_FILE"
}

print_summary() {
  ui ""
  ui "============================================================"
  ui "ISP Billing sudah berjalan"
  ui "URL          : https://$APP_DOMAIN"
  ui "Super Admin  : $ADMIN_USERNAME"
  ui "Commit       : $GIT_COMMIT"
  ui "Environment  : $ENV_FILE (owner $DEPLOY_USER, mode 600)"
  ui "Backup       : $BACKUP_DIR"
  ui "Log installer: $LOG_FILE"
  if [[ "$ADMIN_PASSWORD_GENERATED" == true && "$ADMIN_CREATED" == true ]]; then
    ui "Password baru : $ADMIN_PASSWORD"
    ui "Salinan root  : $GENERATED_ADMIN_FILE"
    ui "Simpan ke password manager lalu hapus file tersebut."
  fi
  if [[ -f /var/run/reboot-required ]]; then
    warn "Ubuntu meminta reboot. Jalankan sudo reboot setelah login berhasil diuji."
  fi
  ui ""
  ui "Status: cd $APP_DIR && docker compose ps"
  ui "Log   : cd $APP_DIR && docker compose logs --tail=100"
  ui "Pastikan firewall/security group provider hanya membuka SSH aktif, 80, dan 443."
  ui "============================================================"
}

main() {
  parse_arguments "$@"
  sanitize_process_environment
  setup_runtime
  validate_repository
  load_installer_state
  COMPOSE_PROJECT_NAME=$(compose_project_name)
  [[ "$COMPOSE_PROJECT_NAME" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || fatal "nama project Compose hasil normalisasi tidak valid"
  export COMPOSE_PROJECT_NAME
  load_existing_environment
  collect_inputs

  progress "Preflight VPS dan repository"
  preflight
  protect_existing_database
  if [[ "$INSTALL_STATE_STATUS" != "complete" ]]; then
    write_state installing
    INSTALL_STATE_STATUS=installing
  fi

  progress "Update Ubuntu dan paket dasar"
  install_base_system

  progress "Siapkan user deployment"
  ensure_deploy_user

  progress "Instal dan verifikasi Docker Engine"
  install_docker

  progress "Konfigurasi UFW dan Fail2ban"
  configure_firewall

  progress "Periksa konflik port host"
  check_host_ports

  progress "Validasi DNS publik"
  check_dns
  ui "DNS $APP_DOMAIN sudah mengarah ke $PUBLIC_IPV4."

  progress "Buat atau validasi secret production"
  create_or_validate_environment

  progress "Pasang backup harian"
  install_backup_automation

  progress "Backup database sebelum update"
  backup_before_update

  progress "Validasi keamanan Docker Compose"
  validate_compose

  progress "Pull dan build image aplikasi"
  deploy_stack

  progress "Tunggu migration dan service internal"
  verify_internal_stack

  progress "Bootstrap Super Admin"
  bootstrap_admin

  progress "Buat backup awal"
  run_logged systemctl start isp-billing-backup.service

  progress "Aktifkan dan verifikasi HTTPS publik"
  verify_public_https

  write_state complete
  print_summary
}

main "$@"
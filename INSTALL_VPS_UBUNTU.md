# Instalasi ISP Billing dari VPS Kosong

Panduan ini memasang ISP Billing pada **Ubuntu Server 24.04 LTS** sampai dapat
diakses melalui:

```text
https://billing.example.com
```

Pengguna tidak perlu menulis nomor port pada URL. Secara teknis, internet tetap
memerlukan port standar `80` dan `443`. Hanya Caddy yang membuka kedua port
tersebut. PostgreSQL (`5432`), Go API (`8080`), dan Next.js (`3000`) tetap berada
di jaringan private Docker dan tidak dipublikasikan ke VPS atau internet.

## Dua Versi Instalasi

Tersedia dua cara memasang. Pilih salah satu sesuai kondisi server:

| Versi | Kapan dipakai | TLS/HTTPS | File utama |
|---|---|---|---|
| **Opsi 1 — VPS full (sekali paste)** | VPS Ubuntu 24.04 **kosong**, tanpa panel atau web server lain. Domain diarahkan langsung ke IP VPS. | Caddy menerbitkan sertifikat sendiri (Let's Encrypt) di port `80/443`. | `install.sh`, `compose.yaml`, `Caddyfile` |
| **Opsi 2 — aaPanel (Nginx di depan)** | Server sudah memakai **aaPanel/Nginx** yang memegang port `80/443`, atau satu server dipakai untuk banyak site. | Nginx aaPanel terminasi SSL, lalu `proxy_pass` ke `127.0.0.1:8090`. | `compose.aapanel.yaml`, `Caddyfile.aapanel`, `deploy-safe.sh` |

- **Opsi 1** dijelaskan pada seluruh bagian di bawah: instalasi otomatis via
  `install.sh`, ditambah rincian/fallback manual bernomor 1-20.
- **Opsi 2** berada pada bagian [Opsi 2 — Instalasi via aaPanel](#opsi-2--instalasi-via-aapanel)
  di akhir dokumen.

> Jangan mencampur kedua mode pada server yang sama. Opsi 1 memerlukan port
> `80/443` bebas untuk Caddy; Opsi 2 justru memakai `80/443` untuk Nginx aaPanel
> dan hanya membuka Caddy di `127.0.0.1:8090`.

## Opsi 1 — Instalasi Otomatis VPS Full (Direkomendasikan)

Auto-installer ditujukan untuk Ubuntu Server 24.04 LTS kosong. Sebelum mulai:

1. Arahkan record `A` domain langsung ke IPv4 publik VPS.
2. Hapus record `AAAA` bila IPv6 VPS belum aktif.
3. Jika memakai Cloudflare, gunakan **DNS only** selama instalasi pertama.
4. Buka port SSH yang sedang dipakai serta TCP `80/443` pada firewall/security
   group provider.
5. Gunakan VPS minimal 4 GB RAM dan 20 GB disk kosong.

Login menggunakan akses awal `root`, lalu jalankan satu command chain berikut:

```bash
apt-get update && apt-get install -y git && \
git clone https://github.com/ORGANISASI/ISP-BILLING.git /opt/isp-billing && \
cd /opt/isp-billing && bash install.sh
```

Ganti URL repository dengan URL aktual. Untuk repository private, siapkan
autentikasi Git sebelum clone; installer tidak meminta atau menyimpan token Git.

Wizard menampilkan 16 tahap progress. Masukkan domain, email notifikasi HTTPS,
dan username Super Admin. Pada prompt password Super Admin:

- masukkan password unik minimal 20 karakter lalu konfirmasi; atau
- tekan Enter agar password kuat digenerate otomatis.

Password PostgreSQL owner dan runtime selalu digenerate acak, masing-masing 64
karakter hexadecimal. Password database tidak ditampilkan dan hanya disimpan di
`/opt/isp-billing/.env`, owner `deploy`, mode `600`.

### Mode Non-Interaktif

Gunakan password Super Admin generated:

```bash
cd /opt/isp-billing
sudo bash install.sh \
  --non-interactive \
  --domain billing.example.com \
  --acme-email admin@example.com \
  --admin-user admin \
  --generate-admin-password
```

Atau siapkan password tanpa menaruhnya di history/process list:

```bash
sudo install -o root -g root -m 600 /dev/null /root/isp-admin-password
sudo bash -c 'read -r -s -p "Password Super Admin: " password; printf "\n"; printf "%s\n" "$password" > /root/isp-admin-password; unset password'

cd /opt/isp-billing
sudo bash install.sh \
  --non-interactive \
  --domain billing.example.com \
  --acme-email admin@example.com \
  --admin-user admin \
  --admin-password-file /root/isp-admin-password
sudo rm -f /root/isp-admin-password
```

Password literal sengaja tidak tersedia sebagai flag. Lihat semua opsi:

```bash
sudo bash /opt/isp-billing/install.sh --help
```

### Hasil dan Lokasi Penting

Setelah berhasil, installer memastikan halaman login, `/health`, dan `/ready`
dapat diakses melalui HTTPS. Lokasi operasional:

| Data | Lokasi |
|---|---|
| Environment dan password database | `/opt/isp-billing/.env` |
| Log installer | `/var/log/isp-billing/installer.log` |
| Backup PostgreSQL | `/var/backups/isp-billing` |
| State installer dan marker identitas volume | `/var/lib/isp-billing/installer-state` |
| Password admin generated | `/root/isp-billing-initial-admin.txt` |

File password admin generated harus dipindahkan ke password manager lalu
dihapus. Password admin yang dimasukkan manual tidak disimpan oleh installer.

### Rerun dan Recovery

Jalankan command yang sama untuk melanjutkan instalasi terputus atau setelah
checkout release baru:

```bash
cd /opt/isp-billing
sudo bash install.sh
```

Pada rerun, installer:

- mempertahankan `.env`, password database, volume PostgreSQL, dan data;
- tidak membuat ulang atau mereset password Super Admin;
- membuat backup sebelum build/migration bila database existing sehat;
- memverifikasi marker volume dan checksum semua migration yang sudah applied;
- mengunci Docker ke socket lokal serta memakai file/project Compose eksplisit;
- memperbarui script serta timer backup secara idempotent;
- tidak menjalankan `docker compose down -v` atau volume prune.

Jika volume PostgreSQL ada tetapi `.env` hilang, installer berhenti. Pulihkan
`.env` dari backup rahasia sebelum melanjutkan. Membuat password baru tidak akan
membuka database lama.

Installer hanya menerima delapan key production yang dibuatnya di `.env`
(termasuk `APP_ENCRYPTION_KEY`).
Jangan menambahkan `COMPOSE_FILE`, `DOCKER_HOST`, atau konfigurasi aplikasi lain
ke file tersebut. Jika marker volume hilang, ID berbeda, atau volume yang
tercatat tidak tersedia, installer berhenti agar database kosong tidak dianggap
sebagai database production.

Installer membuat user `deploy`, menyalin SSH key pemanggil bila tersedia, dan
membuka port SSH aktif di UFW. Installer **tidak** mengganti port SSH,
menonaktifkan login lama, mengubah firewall provider, mengubah DNS melalui API,
melakukan reboot otomatis, atau mengelola backup offsite. Hardening SSH manual
pada bagian berikut dilakukan setelah login user `deploy` berhasil diuji.

Bagian bernomor di bawah tetap tersedia sebagai prosedur manual/fallback dan
referensi audit.

Panduan ini menggunakan:

- Ubuntu Server 24.04 LTS 64-bit
- public Git repository
- Docker Engine dan Docker Compose plugin resmi
- Caddy sebagai reverse proxy dan pengelola HTTPS otomatis
- port SSH khusus
- DNS yang diarahkan setelah core stack selesai dipasang

> Jangan jalankan seluruh halaman secara membabi buta. Baca setiap bagian,
> ganti placeholder, lalu periksa output sebelum melanjutkan. Pertahankan sesi
> SSH lama ketika mengubah konfigurasi SSH.

## 1. Kebutuhan Awal

Rekomendasi awal VPS:

- 2 vCPU
- RAM 4 GB
- SSD 40 GB
- IPv4 publik statis
- Ubuntu Server 24.04 LTS

Untuk lebih dari 100.000 pelanggan, kapasitas final harus ditentukan melalui
load test dan pemantauan produksi.

Siapkan nilai berikut:

| Nama | Contoh | Keterangan |
|---|---|---|
| `VPS_IP` | `203.0.113.10` | IPv4 publik VPS |
| `REPO_URL` | `https://github.com/perusahaan/isp-billing.git` | URL public repository |
| `APP_DOMAIN` | `billing.example.com` | Subdomain tanpa `http://`, slash, atau port |
| `ACME_EMAIL` | `admin@example.com` | Email notifikasi sertifikat HTTPS |
| `SSH_PORT` | `2244` | Port khusus antara 1024-65535 |
| SSH public key | `ssh-ed25519 AAAA...` | Public key komputer administrator |

Jangan membuat record `AAAA` jika VPS tidak memiliki IPv6 yang benar-benar
aktif. Record IPv6 yang salah sering menyebabkan HTTPS terlihat gagal pada
sebagian perangkat.

## 2. Login Awal dan Update Ubuntu

Login pertama kali menggunakan akses yang diberikan penyedia VPS:

```bash
ssh root@VPS_IP
```

Perbarui sistem dan pasang utilitas dasar:

```bash
apt update
apt full-upgrade -y
apt install -y ca-certificates curl git gnupg jq openssl ufw fail2ban \
  dnsutils unattended-upgrades
timedatectl set-timezone Asia/Jakarta
systemctl enable --now systemd-timesyncd
```

Jika update kernel meminta reboot, lakukan sekarang lalu login kembali:

```bash
reboot
```

Aktifkan security update otomatis:

```bash
dpkg-reconfigure -plow unattended-upgrades
```

Periksa waktu server:

```bash
timedatectl status
```

Status sinkronisasi waktu harus aktif. Waktu yang salah dapat menggagalkan TLS,
session login, dan pencatatan transaksi.

## 3. Buat User Deployment dan Pasang SSH Key

Tetapkan nama user:

```bash
export DEPLOY_USER=deploy
adduser "$DEPLOY_USER"
usermod -aG sudo "$DEPLOY_USER"
```

Masukkan password lokal yang kuat ketika diminta. Password tersebut dipakai
untuk `sudo`, bukan untuk login SSH setelah hardening selesai.

Buat direktori SSH:

```bash
install -d -m 700 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "/home/$DEPLOY_USER/.ssh"
touch "/home/$DEPLOY_USER/.ssh/authorized_keys"
chown "$DEPLOY_USER:$DEPLOY_USER" "/home/$DEPLOY_USER/.ssh/authorized_keys"
chmod 600 "/home/$DEPLOY_USER/.ssh/authorized_keys"
```

Tambahkan public key administrator. Ganti teks contoh dengan public key asli:

```bash
printf '%s\n' 'ssh-ed25519 GANTI_DENGAN_PUBLIC_KEY administrator' \
  > "/home/$DEPLOY_USER/.ssh/authorized_keys"
chown "$DEPLOY_USER:$DEPLOY_USER" "/home/$DEPLOY_USER/.ssh/authorized_keys"
chmod 600 "/home/$DEPLOY_USER/.ssh/authorized_keys"
```

**Jangan menutup sesi root yang sedang aktif.** Buka terminal kedua dan uji:

```bash
ssh deploy@VPS_IP
sudo whoami
```

Output terakhir harus `root`. Jangan melanjutkan jika login key atau `sudo`
belum berhasil.

## 4. Ubah SSH ke Port Khusus dengan Aman

Bagian ini dijalankan melalui user `deploy`. Ganti `2244` dengan port pilihan:

```bash
export SSH_PORT=2244
```

Validasi rentang port:

```bash
if ! [[ "$SSH_PORT" =~ ^[0-9]+$ ]] || [ "$SSH_PORT" -lt 1024 ] || [ "$SSH_PORT" -gt 65535 ]; then
  echo "SSH_PORT harus berupa angka 1024-65535"
  exit 1
fi
```

Buka port baru di firewall **sebelum** memindahkan SSH. Port 22 dipertahankan
sementara sampai koneksi baru terbukti berhasil. Buka juga `SSH_PORT/tcp` pada
firewall/security group provider VPS sebelum menjalankan langkah berikutnya;
jangan menghapus rule port 22 di provider pada tahap ini.

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow 22/tcp comment 'SSH sementara'
sudo ufw allow "$SSH_PORT/tcp" comment 'SSH production'
sudo ufw allow 80/tcp comment 'HTTP untuk redirect dan ACME'
sudo ufw allow 443/tcp comment 'HTTPS'
sudo ufw allow 443/udp comment 'HTTP3 opsional'
sudo ufw --force enable
sudo ufw status numbered
```

Buat hardening SSH dalam snippet `00-...` agar dibaca sebelum snippet cloud-init
atau provider. Pada OpenSSH, banyak directive scalar memakai nilai efektif
pertama yang ditemukan, sehingga nama `99-...` dapat kalah dari konfigurasi
provider yang lebih awal:

```bash
sudo tee /etc/ssh/sshd_config.d/00-isp-billing.conf >/dev/null <<EOF
Port ${SSH_PORT}
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
AuthenticationMethods publickey
X11Forwarding no
AllowUsers deploy
EOF
sudo /usr/sbin/sshd -t
```

Jika nama user bukan `deploy`, ganti nilai `AllowUsers` sebelum menjalankan
`sshd -t`. Uji konfigurasi **efektif**, bukan hanya sintaks:

```bash
EFFECTIVE_SSH="$(sudo /usr/sbin/sshd -T -C user=deploy,host="$(hostname)",addr=127.0.0.1)"
printf '%s\n' "$EFFECTIVE_SSH" | grep -E \
  '^(port|permitrootlogin|passwordauthentication|kbdinteractiveauthentication|pubkeyauthentication|authenticationmethods|allowusers) '

test "$(printf '%s\n' "$EFFECTIVE_SSH" | awk '$1 == "port" {count++} END {print count+0}')" -eq 1
printf '%s\n' "$EFFECTIVE_SSH" | grep -qx "port ${SSH_PORT}"
printf '%s\n' "$EFFECTIVE_SSH" | grep -qx 'permitrootlogin no'
printf '%s\n' "$EFFECTIVE_SSH" | grep -qx 'passwordauthentication no'
printf '%s\n' "$EFFECTIVE_SSH" | grep -qx 'kbdinteractiveauthentication no'
printf '%s\n' "$EFFECTIVE_SSH" | grep -qx 'pubkeyauthentication yes'
printf '%s\n' "$EFFECTIVE_SSH" | grep -qx 'authenticationmethods publickey'
unset EFFECTIVE_SSH
```

Semua command `test`/`grep -qx` harus exit `0`. Periksa dengan `echo $?` jika
ragu. Jika ada yang gagal, jangan restart SSH. Cari directive yang konflik:

```bash
sudo grep -RniE '^[[:space:]]*(Port|PermitRootLogin|PasswordAuthentication|KbdInteractiveAuthentication|AuthenticationMethods|AllowUsers)[[:space:]]' \
  /etc/ssh/sshd_config /etc/ssh/sshd_config.d
```

Ubuntu 24.04 umumnya memakai socket activation. Generator systemd membaca
konfigurasi OpenSSH ketika `daemon-reload`, jadi pertahankan model bawaan dan
restart unit yang memang aktif:

```bash
sudo systemctl daemon-reload
if sudo systemctl is-active --quiet ssh.socket; then
  sudo systemctl restart ssh.socket
  sudo systemctl try-restart ssh.service || true
else
  sudo systemctl restart ssh.service
fi

sudo ss -lntp | grep -E ":${SSH_PORT}[[:space:]]"
if sudo ss -H -lnt | awk '{print $4}' | grep -qE '(^|:)22$'; then
  echo 'Port 22 masih mendengarkan. Jangan lanjut sampai konflik konfigurasi ditemukan.'
  exit 1
fi
```

Jika listener port baru tidak muncul, jangan logout. Periksa:

```bash
sudo systemctl status ssh.socket ssh.service --no-pager
sudo journalctl -u ssh.socket -u ssh.service -n 100 --no-pager
sudo /usr/sbin/sshd -T | grep '^port '
```

Buka terminal ketiga dan uji port baru:

```bash
ssh -p SSH_PORT deploy@VPS_IP
sudo whoami
```

Ganti `SSH_PORT` pada perintah tersebut dengan angka aktual. Hanya setelah login
baru berhasil, hapus akses port 22:

```bash
sudo ufw delete allow 22/tcp
sudo ufw status verbose
```

Hapus port 22 juga dari firewall/security group milik provider VPS. Izinkan
hanya:

- `SSH_PORT/tcp`, sebaiknya dibatasi ke IP administrator jika memungkinkan
- `80/tcp` dari internet
- `443/tcp` dari internet
- `443/udp` dari internet, opsional untuk HTTP/3

Konfigurasikan Fail2ban sesuai port SSH:

```bash
sudo tee /etc/fail2ban/jail.d/sshd.local >/dev/null <<EOF
[sshd]
enabled = true
port = ${SSH_PORT}
maxretry = 5
findtime = 10m
bantime = 1h
EOF
sudo systemctl enable --now fail2ban
sudo systemctl restart fail2ban
sudo fail2ban-client status sshd
```

> Mengganti port bukan pengganti SSH key. Proteksi utamanya tetap public key,
> `PermitRootLogin no`, `PasswordAuthentication no`, firewall, dan Fail2ban.

## 5. Instal Docker Engine Resmi

Hapus paket yang dapat konflik. Pesan bahwa paket tidak ditemukan dapat
diabaikan:

```bash
for package in docker.io docker-compose docker-compose-v2 docker-doc podman-docker containerd runc; do
  sudo apt-get remove -y "$package" 2>/dev/null || true
done
```

Tambahkan repository APT resmi Docker:

```bash
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
  -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

sudo tee /etc/apt/sources.list.d/docker.sources >/dev/null <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: $(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}")
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF

sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io \
  docker-buildx-plugin docker-compose-plugin
sudo systemctl enable --now docker
```

Verifikasi instalasi:

```bash
sudo docker version
sudo docker compose version
sudo docker run --rm hello-world
```

Tambahkan user deployment ke group Docker:

```bash
sudo usermod -aG docker deploy
```

Group `docker` setara dengan akses root pada host. Hanya tambahkan administrator
yang dipercaya. Logout lalu login kembali menggunakan port SSH khusus:

```bash
exit
ssh -p SSH_PORT deploy@VPS_IP
```

Verifikasi tanpa `sudo`:

```bash
docker version
docker compose version
groups
```

## 6. Clone Repository

Tetapkan nilai deployment pada sesi user `deploy`. Ganti seluruh contoh:

```bash
export REPO_URL='https://github.com/perusahaan/isp-billing.git'
export APP_DOMAIN='billing.example.com'
export ACME_EMAIL='admin@example.com'
```

Validasi format domain:

```bash
if ! [[ "$APP_DOMAIN" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])$ ]] \
  || [[ "$APP_DOMAIN" == *:* ]] || [[ "$APP_DOMAIN" == *'/'* ]]; then
  echo "APP_DOMAIN tidak valid. Gunakan nama seperti billing.example.com"
  exit 1
fi
```

Clone proyek ke `/opt/isp-billing`:

```bash
sudo install -d -m 0750 -o deploy -g deploy /opt/isp-billing
git clone "$REPO_URL" /opt/isp-billing
cd /opt/isp-billing
git status --short
git log -1 --oneline
```

Untuk produksi, sebaiknya checkout release tag yang sudah diuji, bukan commit
acak dari branch pengembangan:

```bash
git tag --sort=-version:refname | head
# Contoh jika tag tersedia:
# git checkout v1.0.0
```

## 7. Buat Environment Production

Gunakan dua login PostgreSQL yang berbeda:

- `isp_billing_owner`: pemilik database, hanya dipakai init, migration, dan backup
- `isp_billing_app`: runtime API dengan hak terbatas dan tanpa hak `DELETE`

Keduanya memakai password hexadecimal acak 64 karakter sehingga kuat dan aman
digunakan pada URL koneksi internal.

Tambahan `APP_ENCRYPTION_KEY` (64 karakter hexadecimal) dipakai API untuk
mengenkripsi kredensial router/OLT. Compose menjadikannya wajib, jadi `.env`
harus memuatnya.

```bash
cd /opt/isp-billing
umask 077
POSTGRES_PASSWORD="$(openssl rand -hex 32)"
POSTGRES_APP_PASSWORD="$(openssl rand -hex 32)"
APP_ENCRYPTION_KEY="$(openssl rand -hex 32)"

cat > .env <<EOF
APP_DOMAIN=${APP_DOMAIN}
ACME_EMAIL=${ACME_EMAIL}
POSTGRES_DB=isp_billing
POSTGRES_USER=isp_billing_owner
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_APP_USER=isp_billing_app
POSTGRES_APP_PASSWORD=${POSTGRES_APP_PASSWORD}
APP_ENCRYPTION_KEY=${APP_ENCRYPTION_KEY}
EOF

unset POSTGRES_PASSWORD POSTGRES_APP_PASSWORD APP_ENCRYPTION_KEY
chmod 600 .env
```

Pastikan file dimiliki user deployment dan tidak dapat dibaca user lain:

```bash
stat -c '%U:%G %a %n' .env
```

Output harus menyerupai:

```text
deploy:deploy 600 .env
```

Jangan menjalankan `cat .env`, `docker compose config`, atau perintah lain yang
mencetak secret ke terminal/log. Validasi tanpa menampilkan nilainya:

```bash
required=(APP_DOMAIN ACME_EMAIL POSTGRES_DB POSTGRES_USER POSTGRES_PASSWORD POSTGRES_APP_USER POSTGRES_APP_PASSWORD APP_ENCRYPTION_KEY)
declare -A config=()
for variable in "${required[@]}"; do
  count="$(grep -c "^${variable}=" .env || true)"
  if [ "$count" -ne 1 ]; then
    echo "Variable $variable harus muncul tepat satu kali"
    exit 1
  fi
  config["$variable"]="$(sed -n "s/^${variable}=//p" .env)"
  if [ -z "${config[$variable]}" ]; then
    echo "Variable $variable kosong"
    exit 1
  fi
done
if [ "${config[POSTGRES_USER]}" = "${config[POSTGRES_APP_USER]}" ]; then
  echo "POSTGRES_USER dan POSTGRES_APP_USER harus berbeda"
  exit 1
fi
if [ "${config[POSTGRES_PASSWORD]}" = "${config[POSTGRES_APP_PASSWORD]}" ]; then
  echo "Password owner dan runtime harus berbeda"
  exit 1
fi
unset config
echo "Environment lengkap"
```

Jangan menyimpan password akun Super Admin di `.env`. Password tersebut hanya
dimasukkan ketika bootstrap satu kali.

## 8. Validasi dan Build Aplikasi

Validasi Compose tanpa mencetak konfigurasinya:

```bash
cd /opt/isp-billing
docker compose config --quiet
docker compose config --services
```

Periksa port yang dipublikasikan tanpa mencetak environment/secrets:

```bash
docker compose config --format json | jq -r '
  .services
  | to_entries[]
  | .key as $service
  | (.value.ports // [])[]?
  | "\($service): \(.published)->\(.target)/\(.protocol)"
'
```

Output yang diperbolehkan hanya milik `gateway`:

```text
gateway: 80->80/tcp
gateway: 443->443/tcp
gateway: 443->443/udp
```

Jika `postgres`, `api`, atau `web` muncul, hentikan instalasi dan periksa
`compose.yaml`.

Build image:

```bash
docker compose build --pull
```

Build pertama dapat memerlukan beberapa menit. Jika gagal, lihat bagian
troubleshooting sebelum mengulang.

## 9. Jalankan Core Stack Sebelum DNS Aktif

Jalankan semua service:

```bash
docker compose up -d
docker compose ps -a
```

Migration harus berakhir dengan exit code `0`:

```bash
docker compose ps -a migrate
docker compose logs --no-log-prefix migrate
```

Setiap file migration yang sudah applied bersifat append-only. Runner menyimpan
checksum SHA-256 di `schema_migrations` dan menolak release jika file lama
berubah atau file applied tidak lagi tersedia. Perubahan schema berikutnya harus
selalu dibuat sebagai file `*.up.sql` baru.

Periksa API dari dalam container:

```bash
docker compose exec -T api wget -qO- http://127.0.0.1:8080/health
docker compose exec -T api wget -qO- http://127.0.0.1:8080/ready
```

Output yang benar:

```json
{"status":"ok"}
```

Periksa Next.js dari dalam container:

```bash
docker compose exec -T web node -e \
  "fetch('http://127.0.0.1:3000/login').then(r=>{console.log(r.status);process.exit(r.ok?0:1)}).catch(e=>{console.error(e);process.exit(1)})"
```

Output harus `200`.

Periksa status keseluruhan:

```bash
docker compose ps
```

`postgres`, `api`, dan `web` harus `healthy`. Gateway dapat menulis error ACME
selama DNS belum menunjuk ke VPS. Hal tersebut wajar pada tahap ini.

Jangan menggunakan IP atau menambahkan `:8080` untuk login produksi. Cookie
session production memakai atribut `Secure` dan memerlukan HTTPS dari domain.

## 10. Buat Super Admin Satu Kali

Periksa dahulu apakah bootstrap sudah pernah selesai:

```bash
cd /opt/isp-billing
docker compose run --rm --no-deps \
  --entrypoint /app/bootstrap-admin api --check
BOOTSTRAP_STATUS=$?
```

Exit `0` berarti Super Admin sudah ada dan langkah pembuatan harus dilewati.
Exit `3` berarti akun belum ada dan command berikut boleh dijalankan. Exit lain
menandakan error database yang harus diperiksa terlebih dahulu.

Password dibaca secara tersembunyi, tidak ditulis literal pada command, dan
tidak masuk shell history:

```bash
cd /opt/isp-billing
read -r -p 'Username Super Admin: ' BOOTSTRAP_ADMIN_USERNAME
read -r -s -p 'Password Super Admin (minimal 20 karakter): ' BOOTSTRAP_ADMIN_PASSWORD
printf '\n'
export BOOTSTRAP_ADMIN_USERNAME BOOTSTRAP_ADMIN_PASSWORD

docker compose run --rm --no-deps \
  -e BOOTSTRAP_ADMIN_USERNAME \
  -e BOOTSTRAP_ADMIN_PASSWORD \
  --entrypoint /app/bootstrap-admin api

unset BOOTSTRAP_ADMIN_USERNAME BOOTSTRAP_ADMIN_PASSWORD
```

Gunakan password unik minimal 20 karakter untuk akun produksi. Simpan di
password manager. Rerun dengan username Super Admin yang sama tidak membuat akun
duplikat dan tidak mereset password. Jika Super Admin lain sudah ada atau
username dimiliki role lain, command gagal aman.

## 11. Pointing DNS Setelah Core Stack Sehat

Pada DNS provider, buat record:

| Type | Name | Value | TTL |
|---|---|---|---|
| `A` | `billing` | `VPS_IP` | Auto atau 300 |

Contoh tersebut menghasilkan `billing.example.com`. Gunakan nama sesuai
`APP_DOMAIN` dalam `.env`.

Aturan DNS:

1. Hapus record `A` lama untuk subdomain yang sama.
2. Buat `AAAA` hanya jika IPv6 VPS aktif dan firewall IPv6 sudah benar.
3. Jika menggunakan Cloudflare, gunakan **DNS only** saat validasi pertama.
4. Jangan membuat redirect URL pada panel DNS; gunakan record `A` biasa.

Periksa IP publik VPS:

```bash
curl -4 https://icanhazip.com
```

Periksa propagasi menggunakan resolver publik:

```bash
cd /opt/isp-billing
APP_DOMAIN="$(sed -n 's/^APP_DOMAIN=//p' .env)"
dig +short A "$APP_DOMAIN" @1.1.1.1
dig +short A "$APP_DOMAIN" @8.8.8.8
dig +short AAAA "$APP_DOMAIN" @1.1.1.1
```

Record `A` harus mengembalikan IPv4 VPS. Output `AAAA` harus kosong jika IPv6
tidak digunakan.

## 12. Aktifkan HTTPS Otomatis

Setelah DNS publik benar, pastikan firewall provider dan UFW mengizinkan TCP
80/443. Restart gateway untuk memulai percobaan ACME segera:

```bash
cd /opt/isp-billing
docker compose restart gateway
docker compose logs -f --tail=100 gateway
```

Tunggu sampai log menunjukkan sertifikat berhasil diterbitkan. Tekan `Ctrl+C`
untuk keluar dari tampilan log; container tetap berjalan.

Caddy menyimpan sertifikat dan private key di named volume `caddy_data`.
Sertifikat akan diperbarui otomatis dan tidak hilang ketika container dibuat
ulang.

Verifikasi redirect HTTP:

```bash
curl -I "http://$APP_DOMAIN"
```

Respons harus berupa redirect ke `https://...`.

Verifikasi health melalui HTTPS:

```bash
curl -fsS "https://$APP_DOMAIN/health"
curl -fsS "https://$APP_DOMAIN/ready"
```

Output:

```json
{"status":"ok"}
```

Buka browser:

```text
https://billing.example.com
```

Tidak perlu dan tidak boleh menggunakan `:8080`, `:3000`, atau `:5432`.

## 13. Pemeriksaan Keamanan Port

Periksa listener host:

```bash
sudo ss -lntup
docker compose ps
```

Yang diharapkan:

- SSH pada port khusus
- `80/tcp`
- `443/tcp`
- `443/udp` jika HTTP/3 digunakan

Yang tidak boleh tersedia dari internet:

- PostgreSQL `5432`
- Go API `8080`
- Next.js `3000`

Docker dapat melewati sebagian rule UFW ketika sebuah container menggunakan
`ports:`. Karena itu, keamanan tidak hanya mengandalkan UFW: `compose.yaml`
sengaja tidak memublikasikan port database, API, dan web sama sekali.

Lakukan port scan dari komputer lain atau layanan pemindai eksternal. Jangan
hanya menguji dari dalam VPS.

## 14. Uji Login dan Reboot

1. Login menggunakan akun Super Admin.
2. Pastikan dashboard tampil.
3. Logout dan pastikan session berakhir.
4. Login kembali.
5. Tambah satu pelanggan uji.
6. Pastikan URL tetap menggunakan domain yang sama untuk halaman dan API.

Lakukan reboot test:

```bash
sudo reboot
```

Setelah VPS kembali online:

```bash
ssh -p SSH_PORT deploy@VPS_IP
cd /opt/isp-billing
APP_DOMAIN="$(sed -n 's/^APP_DOMAIN=//p' .env)"
docker compose ps
curl -fsS "https://$APP_DOMAIN/health"
curl -fsS "https://$APP_DOMAIN/ready"
```

Service dengan `restart: unless-stopped` harus kembali otomatis.

## 15. Backup PostgreSQL Harian

Backup pada disk VPS bukan disaster recovery. Tetap salin hasil backup secara
terenkripsi ke storage atau server lain.

Buat direktori dan script backup:

```bash
sudo install -d -o deploy -g deploy -m 700 /var/backups/isp-billing

sudo tee /usr/local/sbin/isp-billing-backup >/dev/null <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

APP_DIR=/opt/isp-billing
BACKUP_DIR=/var/backups/isp-billing
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP_FILE="$BACKUP_DIR/isp_billing_${TIMESTAMP}.dump"
TEMP_FILE="$(mktemp "$BACKUP_DIR/.isp_billing_${TIMESTAMP}.XXXXXX.dump")"

cleanup() {
  rm -f -- "$TEMP_FILE" "$BACKUP_FILE.sha256.tmp"
}
trap cleanup EXIT

cd "$APP_DIR"
umask 077
mkdir -p "$BACKUP_DIR"

docker compose exec -T postgres sh -ec '
  export PGPASSWORD="$POSTGRES_PASSWORD"
  exec pg_dump \
    --username "$POSTGRES_USER" \
    --dbname "$POSTGRES_DB" \
    --format custom \
    --no-owner \
    --no-privileges
' > "$TEMP_FILE"

test -s "$TEMP_FILE"
mv -- "$TEMP_FILE" "$BACKUP_FILE"
sha256sum "$BACKUP_FILE" > "$BACKUP_FILE.sha256.tmp"
mv -- "$BACKUP_FILE.sha256.tmp" "$BACKUP_FILE.sha256"
find "$BACKUP_DIR" -type f -name '*.dump' -mtime +14 -delete
find "$BACKUP_DIR" -type f -name '*.dump.sha256' -mtime +14 -delete

trap - EXIT
echo "Backup selesai: $BACKUP_FILE"
EOF

sudo chmod 700 /usr/local/sbin/isp-billing-backup
```

Uji manual:

```bash
sudo -u deploy /usr/local/sbin/isp-billing-backup
sudo ls -lh /var/backups/isp-billing
```

Buat systemd service dan timer harian:

```bash
sudo tee /etc/systemd/system/isp-billing-backup.service >/dev/null <<'EOF'
[Unit]
Description=Backup PostgreSQL ISP Billing
Requires=docker.service
After=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/isp-billing-backup
User=deploy
Group=deploy
SupplementaryGroups=docker
WorkingDirectory=/opt/isp-billing
PrivateTmp=true
NoNewPrivileges=true
EOF

sudo tee /etc/systemd/system/isp-billing-backup.timer >/dev/null <<'EOF'
[Unit]
Description=Jadwal backup harian ISP Billing

[Timer]
OnCalendar=*-*-* 02:30:00 Asia/Jakarta
Persistent=true
RandomizedDelaySec=10m

[Install]
WantedBy=timers.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now isp-billing-backup.timer
systemctl list-timers isp-billing-backup.timer
```

Periksa log backup:

```bash
sudo journalctl -u isp-billing-backup.service -n 100 --no-pager
```

## 16. Restore Drill ke Database Uji

Restore harus diuji berkala. Jangan pertama kali mencoba restore ketika insiden
sudah terjadi.

Pilih backup terbaru:

```bash
LATEST_BACKUP="$(sudo find /var/backups/isp-billing -maxdepth 1 -name '*.dump' -type f -printf '%T@ %p\n' | sort -nr | head -1 | cut -d' ' -f2-)"
test -n "$LATEST_BACKUP"
echo "$LATEST_BACKUP"
sudo sha256sum -c "$LATEST_BACKUP.sha256"
```

Buat database uji dan restore:

```bash
cd /opt/isp-billing
docker compose exec -T postgres sh -ec '
  export PGPASSWORD="$POSTGRES_PASSWORD"
  exec createdb -U "$POSTGRES_USER" isp_billing_restore_test
'

sudo cat "$LATEST_BACKUP" | docker compose exec -T postgres sh -ec '
  export PGPASSWORD="$POSTGRES_PASSWORD"
  exec pg_restore \
    -U "$POSTGRES_USER" \
    -d isp_billing_restore_test \
    --exit-on-error \
    --single-transaction \
    --no-owner \
    --no-privileges
'

docker compose exec -T postgres sh -ec '
  export PGPASSWORD="$POSTGRES_PASSWORD"
  exec psql \
    -U "$POSTGRES_USER" \
    -d isp_billing_restore_test \
    -c "SELECT version, applied_at FROM schema_migrations ORDER BY version;"
'
```

Setelah hasil diverifikasi, hapus database uji:

```bash
docker compose exec -T postgres sh -ec '
  export PGPASSWORD="$POSTGRES_PASSWORD"
  exec dropdb -U "$POSTGRES_USER" isp_billing_restore_test
'
```

## 17. Update Aplikasi

Jangan update langsung tanpa backup dan catatan versi.

```bash
cd /opt/isp-billing
sudo systemctl start isp-billing-backup.service
sudo systemctl status isp-billing-backup.service --no-pager

git status --short
git fetch --tags --prune
git tag --sort=-version:refname | head
```

Worktree harus bersih. Checkout release yang sudah diuji, lalu gunakan installer
untuk backup awal, pemeriksaan checksum/volume, build, dan verifikasi HTTPS:

```bash
git checkout VERSI_RELEASE
sudo bash install.sh
```

Periksa hasil:

```bash
APP_DOMAIN="$(sed -n 's/^APP_DOMAIN=//p' .env)"
docker compose ps -a
docker compose logs --no-log-prefix migrate
docker compose logs --tail=100 api web gateway
curl -fsS "https://$APP_DOMAIN/health"
curl -fsS "https://$APP_DOMAIN/ready"
```

Setelah stabil, image yang tidak terpakai dapat dibersihkan:

```bash
docker image prune -f
```

Perbarui paket Ubuntu dan Docker Engine pada jadwal maintenance terpisah:

```bash
sudo apt update
apt list --upgradable
sudo apt upgrade
if [ -f /var/run/reboot-required ]; then
  cat /var/run/reboot-required
  sudo reboot
fi
```

Setelah upgrade atau reboot, ulangi pemeriksaan `docker compose ps` serta
endpoint `/health` dan `/ready` seperti di atas.

**Jangan pernah menjalankan:**

```text
docker compose down -v
docker volume prune
```

Opsi `-v` dapat menghapus database dan storage sertifikat. Rollback aplikasi
juga harus mempertimbangkan kompatibilitas migration database; checkout kode
lama tidak selalu cukup.

## 18. Troubleshooting

Pada sesi SSH baru, muat nama domain tanpa memuat secret database:

```bash
cd /opt/isp-billing
APP_DOMAIN="$(sed -n 's/^APP_DOMAIN=//p' .env)"
```

### DNS belum mengarah ke VPS

```bash
dig +short A "$APP_DOMAIN" @1.1.1.1
dig +short AAAA "$APP_DOMAIN" @1.1.1.1
curl -4 https://icanhazip.com
```

Pastikan record `A` sama dengan IP VPS. Hapus `AAAA` jika IPv6 tidak aktif.

### Caddy gagal memperoleh sertifikat

```bash
docker compose logs --tail=200 gateway
sudo ufw status verbose
sudo ss -lntup | grep -E ':80|:443'
```

Penyebab umum:

- DNS belum propagasi atau menunjuk IP lama
- port 80/443 tertutup pada firewall provider
- record `AAAA` salah
- domain masih diproxy sebelum origin siap
- terlalu sering mencoba sertifikat hingga terkena rate limit ACME

Jangan menghapus volume `caddy_data` untuk mencoba ulang. Setelah DNS benar:

```bash
docker compose restart gateway
docker compose logs -f --tail=100 gateway
```

### Migration gagal

```bash
docker compose ps -a migrate
docker compose logs --no-log-prefix migrate
docker compose logs --tail=100 postgres
```

Jangan mengedit tabel manual sebelum penyebab migration dipahami. Pastikan
backup tersedia sebelum tindakan koreksi. Jika log menunjukkan checksum mismatch
atau file applied hilang, pulihkan file migration asli dari release yang benar;
jangan mengubah checksum di `schema_migrations`. Buat migration baru untuk
perubahan schema berikutnya.

### API tidak sehat

```bash
docker compose ps api postgres
docker compose logs --tail=200 api postgres
docker compose exec -T api wget -qO- http://127.0.0.1:8080/health
docker compose exec -T api wget -qO- http://127.0.0.1:8080/ready
```

### Next.js tidak sehat

```bash
docker compose ps web
docker compose logs --tail=200 web
docker compose exec -T web node -e \
  "fetch('http://127.0.0.1:3000/login').then(r=>console.log(r.status)).catch(console.error)"
```

### Port host konflik

```bash
sudo ss -lntup | grep -E ':80|:443'
sudo systemctl status nginx apache2 caddy --no-pager 2>/dev/null || true
```

Hentikan web server host yang tidak digunakan. Dalam arsitektur ini Caddy
berjalan di container, bukan sebagai service host.

### Disk hampir penuh

```bash
df -h
docker system df
sudo du -sh /var/lib/docker /var/backups/isp-billing 2>/dev/null
```

Log container sudah dibatasi rotasi. Hapus image yang tidak dipakai hanya
setelah memastikan deployment stabil:

```bash
docker image prune -f
```

Jangan menghapus named volume.

### `.env` tidak dapat dibaca atau Compose meminta variable

```bash
cd /opt/isp-billing
stat -c '%U:%G %a %n' .env
docker compose config --quiet
```

File harus dimiliki `deploy`, mode `600`, dan command dijalankan dari direktori
proyek.

### Koneksi MikroTik

Jangan membuka RouterOS API ke internet umum. Gunakan salah satu:

- WireGuard/site-to-site VPN
- jaringan management private
- allowlist IP publik VPS pada firewall MikroTik

Uji koneksi dari VPS ke alamat management setelah VPN/allowlist aktif. Port
MikroTik bukan bagian dari port publik aplikasi billing.

## 19. Checklist Go-Live

- [ ] Login SSH key pada port khusus berhasil
- [ ] Root login dan password SSH sudah dinonaktifkan
- [ ] Firewall provider dan UFW hanya membuka SSH khusus, 80, dan 443
- [ ] Docker berasal dari repository resmi
- [ ] `.env` mode `600` dan tidak masuk Git
- [ ] `docker compose config --quiet` berhasil
- [ ] Hanya gateway memublikasikan port Docker
- [ ] Migration selesai dengan exit code `0`
- [ ] PostgreSQL, API, dan web berstatus healthy
- [ ] Record `A` menunjuk IPv4 VPS
- [ ] Record `AAAA` kosong atau menunjuk IPv6 yang valid
- [ ] HTTPS valid dan HTTP redirect ke HTTPS
- [ ] Aplikasi dibuka tanpa nomor port
- [ ] Login, dashboard, logout, dan tambah pelanggan berhasil
- [ ] Port 5432, 8080, dan 3000 tertutup dari internet
- [ ] Reboot test berhasil
- [ ] Backup harian aktif
- [ ] Backup sudah diuji restore ke database uji
- [ ] Salinan backup terenkripsi disimpan di lokasi lain

## 20. Ringkasan Arsitektur Port

```text
Internet
   |
   | TCP 80/443, UDP 443 opsional
   v
Caddy gateway
   |-- /api/*  -> Go API:8080   (private Docker network)
   |-- /*      -> Next.js:3000  (private Docker network)
                         |
Go API ------------------+-----> PostgreSQL:5432 (private backend network)

Administrator -> SSH_PORT/tcp -> Ubuntu VPS
```

Hasil akhirnya adalah satu URL publik:

```text
https://APP_DOMAIN
```

Tidak ada port database, API, atau frontend yang dapat diakses langsung dari
internet.

## Opsi 2 — Instalasi via aaPanel

Pakai opsi ini bila server **sudah** memakai aaPanel dan Nginx-nya memegang port
`80/443` (misalnya berbagi satu server untuk beberapa domain). Perbedaan utama
dengan Opsi 1:

- HTTPS/SSL diterbitkan dan diterminasi oleh **Nginx aaPanel**, bukan Caddy.
- Caddy hanya bind ke `127.0.0.1:8090` (`auto_https off`) sebagai satu origin.
- Compose yang dipakai adalah `compose.aapanel.yaml` (bukan `compose.yaml`).
- `install.sh` **tidak dipakai** karena akan berebut port `80/443` dengan Nginx.

```text
Browser → https://APP_DOMAIN
   ▼
Nginx aaPanel :80/:443  (SSL Let's Encrypt via aaPanel)
   │  proxy_pass http://127.0.0.1:8090
   ▼
Caddy gateway (container app-gateway-1, bind 127.0.0.1:8090:80, auto_https off)
   ├── /api/* /health /ready /internal/* → app-api-1 :8080
   └── sisanya                            → app-web-1 :3000
```

### 1. Prasyarat aaPanel

- Ubuntu Server + aaPanel dengan **Nginx** aktif.
- Docker Engine + Docker Compose plugin resmi terpasang (lihat bagian
  [5. Instal Docker Engine Resmi](#5-instal-docker-engine-resmi)).
- Record `A` domain sudah mengarah ke IP server dan sudah bisa diakses lewat
  Nginx aaPanel.

### 2. Siapkan folder aplikasi dan source

Struktur yang dipakai script deploy adalah `/opt/isp-billing/app`:

```bash
sudo install -d -m 0750 /opt/isp-billing/app
cd /opt/isp-billing/app
# Salin/clone source project ke folder ini (tanpa .env).
git clone "$REPO_URL" .
git log -1 --oneline
```

### 3. Buat `.env` (delapan key)

`.env` diletakkan di `/opt/isp-billing/app/.env`, mode `600`. Jangan pernah
menaruh nilai secret literal pada perintah paste; gunakan `openssl` agar nilai
dibuat langsung di server:

```bash
cd /opt/isp-billing/app
umask 077
APP_DOMAIN='billing.example.com'
ACME_EMAIL='admin@example.com'
POSTGRES_PASSWORD="$(openssl rand -hex 32)"
POSTGRES_APP_PASSWORD="$(openssl rand -hex 32)"
APP_ENCRYPTION_KEY="$(openssl rand -hex 32)"

cat > .env <<EOF
APP_DOMAIN=${APP_DOMAIN}
ACME_EMAIL=${ACME_EMAIL}
POSTGRES_DB=isp_billing
POSTGRES_USER=isp_billing_owner
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_APP_USER=isp_billing_app
POSTGRES_APP_PASSWORD=${POSTGRES_APP_PASSWORD}
APP_ENCRYPTION_KEY=${APP_ENCRYPTION_KEY}
EOF

unset POSTGRES_PASSWORD POSTGRES_APP_PASSWORD APP_ENCRYPTION_KEY
chmod 600 .env
```

`APP_DOMAIN` dan `ACME_EMAIL` tetap diisi karena Compose mewajibkannya, walau
pada mode aaPanel penerbitan sertifikat dilakukan Nginx, bukan Caddy.

### 4. Deploy pertama

Semua perintah memakai `-f compose.aapanel.yaml`:

```bash
cd /opt/isp-billing/app
docker compose -f compose.aapanel.yaml build --pull
docker compose -f compose.aapanel.yaml up -d --wait --wait-timeout 180
docker compose -f compose.aapanel.yaml run --rm migrate
docker compose -f compose.aapanel.yaml ps
```

Uji origin lokal (belum lewat domain):

```bash
curl -fsS http://127.0.0.1:8090/health
curl -fsS http://127.0.0.1:8090/ready
```

Keduanya harus mengembalikan `{"status":"ok"}`.

### 5. Bootstrap Super Admin

```bash
cd /opt/isp-billing/app
docker compose -f compose.aapanel.yaml run --rm --no-deps \
  --entrypoint /app/bootstrap-admin api --check
```

Exit `0` = Super Admin sudah ada (lewati). Exit `3` = belum ada, buat sekarang:

```bash
cd /opt/isp-billing/app
read -r -p 'Username Super Admin: ' BOOTSTRAP_ADMIN_USERNAME
read -r -s -p 'Password Super Admin (minimal 20 karakter): ' BOOTSTRAP_ADMIN_PASSWORD
printf '\n'
export BOOTSTRAP_ADMIN_USERNAME BOOTSTRAP_ADMIN_PASSWORD

docker compose -f compose.aapanel.yaml run --rm --no-deps \
  -e BOOTSTRAP_ADMIN_USERNAME \
  -e BOOTSTRAP_ADMIN_PASSWORD \
  --entrypoint /app/bootstrap-admin api

unset BOOTSTRAP_ADMIN_USERNAME BOOTSTRAP_ADMIN_PASSWORD
```

### 6. Reverse proxy di aaPanel

Di panel aaPanel: **Website → tambah Site** untuk `APP_DOMAIN`, aktifkan **SSL**
(Let's Encrypt) dan **Force HTTPS**. Lalu buka **Reverse Proxy** pada site itu
dan arahkan target ke:

```text
http://127.0.0.1:8090
```

Jika mengedit konfigurasi Nginx secara manual, blok `location` inti:

```nginx
location / {
    proxy_pass http://127.0.0.1:8090;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 300s;
}
```

Uji dari luar:

```bash
curl -I "https://$APP_DOMAIN"
curl -fsS "https://$APP_DOMAIN/health"
```

Header pertama harus `200`/`301` dari Nginx, dan `/health` mengembalikan
`{"status":"ok"}`.

### 7. Update berikutnya (deploy aman)

Untuk semua update source berikutnya, gunakan `deploy-safe.sh` yang sudah
memakai `compose.aapanel.yaml` secara otomatis (snapshot → build `--no-cache` →
`up --wait` → migrate → verifikasi → auto-rollback bila gagal):

```bash
cd /opt/isp-billing/app
# taruh src.tar.gz baru di /opt/isp-billing bila ada, lalu:
bash deploy-safe.sh
```

Rollback manual ke snapshot tertentu bila diperlukan:

```bash
bash /opt/isp-billing/rollback.sh /opt/isp-billing/snap-YYYYMMDD-HHMMSS
```

### Catatan keamanan Opsi 2

- `.env` tidak boleh masuk tarball source. `deploy-safe.sh` mengunci `.env`
  (`chattr +i` bila didukung) dan mengecualikannya dari extract.
- Jangan menyetel HSTS di dua tempat. Header HSTS diatur oleh Nginx aaPanel;
  `Caddyfile.aapanel` sengaja menghapusnya (`-Strict-Transport-Security`).
- Port `8090` hanya bind ke `127.0.0.1`, jadi tidak terekspos ke internet.
  Pastikan firewall provider tetap hanya membuka SSH, `80`, dan `443`.

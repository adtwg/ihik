// Package ztecli menyediakan klien SSH/Telnet yang aman untuk ZTE C320/C300.
//
// Prinsip keamanan (wajib dipatuhi pemanggil):
//   - Session lock per OLT: hanya 1 sesi CLI aktif per OLT (lihat Manager).
//   - Rate limit per OLT: jeda minimum antar perintah + kuota per menit.
//   - Command whitelist: hanya perintah aman yang boleh dieksekusi.
//   - Kredensial tidak pernah di-log.
package ztecli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

var (
	ErrLocked       = errors.New("olt sedang dipakai sesi CLI lain, coba beberapa saat lagi")
	ErrRateLimited  = errors.New("batas perintah OLT tercapai, coba lagi sebentar")
	ErrForbiddenCmd = errors.New("perintah ditolak oleh kebijakan keamanan")
	ErrAuthFailed   = errors.New("autentikasi CLI gagal")
	ErrUnreachable  = errors.New("olt cli unreachable")
)

// ---------- Command safety ----------

// dangerousSubstrings: frasa berbahaya yang TIDAK PERNAH dijalankan.
// Pencocokan dilakukan per-token/frasa (bukan substring mentah)
// agar kata aman seperti "information" tidak salah terdeteksi sebagai "format".
var dangerousSubstrings = []string{
	"reload", "reboot", "erase", "format", "restore", "factory",
	"no snmp", "no snmp server", "delete startup", "copy tftp",
	"write erase", "rollback", "firmware", "upgrade", "download",
	"save", "startup config", "admin name", "admin password",
	"line vty", "acl", "firewall", "shutdown", "switch vlan mode",
}

// allowedPrefixes: satu-satunya perintah yang boleh dieksekusi.
var allowedPrefixes = []string{
	"show ",               // semua show read-only
	"interface gpon-olt_", // masuk mode PON
	"interface gpon-onu_", // masuk mode ONU mgmt
	"onu ",                // add/reset/state/del pada mode PON
	"no onu ",             // hapus ONU pada mode interface gpon-olt
	"name ",               // edit nama ONU (mode interface gpon-onu)
	"description ",        // edit deskripsi ONU
	"tcont ",              // konfigurasi tcont ONU
	"no tcont ",           // hapus tcont ONU
	"gemport ",            // konfigurasi gemport ONU
	"no gemport ",         // hapus gemport ONU
	"service-port ",       // konfigurasi service-port ONU
	"no service-port ",    // hapus service-port ONU
	"switchport mode ",    // mode VLAN per vport ONU (tagged/untagged/hybrid)
	"switch vlan ",        // assign tag/untag VLAN pada mode ONU
	"pon-onu-mng ",        // fallback mode manajemen ONU dari interface gpon-olt
	"wan-ip ",             // konfigurasi WAN profile ONU pada mode pon-onu-mng
	"no wan-ip ",          // hapus WAN profile ONU
	"exit",                // keluar mode
	"end",                 // kembali ke exec
	"configure",           // masuk config
	"enable",              // exec privilege
}

func normalizeSafetyPhrase(input string) string {
	in := strings.ToLower(strings.TrimSpace(input))
	if in == "" {
		return ""
	}
	var out strings.Builder
	out.Grow(len(in))
	lastSpace := true
	for _, r := range in {
		isAZ := r >= 'a' && r <= 'z'
		is09 := r >= '0' && r <= '9'
		if isAZ || is09 {
			out.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			out.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(out.String())
}

// ValidateCommand memastikan perintah aman sebelum dikirim.
func ValidateCommand(command string) error {
	cmd := strings.ToLower(strings.TrimSpace(command))
	if cmd == "" || len(cmd) > 200 {
		return ErrForbiddenCmd
	}
	normalized := normalizeSafetyPhrase(cmd)
	wrapped := " " + normalized + " "
	for _, bad := range dangerousSubstrings {
		badNorm := normalizeSafetyPhrase(bad)
		if badNorm == "" {
			continue
		}
		if strings.Contains(wrapped, " "+badNorm+" ") {
			return fmt.Errorf("%w: mengandung kata berbahaya %q", ErrForbiddenCmd, badNorm)
		}
	}
	allowed := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("%w: perintah di luar whitelist", ErrForbiddenCmd)
	}
	// Blokir karakter injeksi CLI
	if strings.ContainsAny(command, "\n\r;") && !strings.EqualFold(strings.TrimSpace(command), "exit") {
		return fmt.Errorf("%w: multi-perintah tidak diizinkan", ErrForbiddenCmd)
	}
	return nil
}

// ---------- Rate limiting per OLT ----------

type oltThrottle struct {
	mu           sync.Mutex
	lastCommand  time.Time
	windowStart  time.Time
	windowCount  int
	minGap       time.Duration
	maxPerMinute int
}

func newThrottle() *oltThrottle {
	return &oltThrottle{
		minGap:       envDurationMS("ZTECLI_MIN_GAP_MS", 120),
		maxPerMinute: envInt("ZTECLI_MAX_PER_MINUTE", 240),
	}
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func envDurationMS(name string, fallbackMS int) time.Duration {
	ms := envInt(name, fallbackMS)
	if ms < 50 {
		ms = 50
	}
	return time.Duration(ms) * time.Millisecond
}

func (t *oltThrottle) wait(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	// window per menit
	if now.Sub(t.windowStart) > time.Minute {
		t.windowStart = now
		t.windowCount = 0
	}
	if t.windowCount >= t.maxPerMinute {
		return ErrRateLimited
	}
	// gap antar perintah
	if elapsed := now.Sub(t.lastCommand); elapsed < t.minGap {
		delay := t.minGap - elapsed
		t.lastCommand = now.Add(delay)
		t.windowCount++
		go func() { _ = delay }()
		time.Sleep(delay)
		return nil
	}
	t.lastCommand = now
	t.windowCount++
	return nil
}

// ---------- Session lock manager per OLT ----------

// Manager menjaga maksimal satu sesi CLI aktif per OLT (key = tenantID+oltID).
type Manager struct {
	mu        sync.Mutex
	blocks    map[string]*sync.Mutex
	throttles map[string]*oltThrottle
}

func NewManager() *Manager {
	return &Manager{blocks: map[string]*sync.Mutex{}, throttles: map[string]*oltThrottle{}}
}

// WithTimeout mengembalikan ErrLocked setelah durasi maksimum.
// Dipakai pemanggil agar request cepat gagal saat OLT sibuk, bukan menggantung.
func (m *Manager) WithTimeout(ctx context.Context, tenantID, oltID string, timeout time.Duration) (func(), *oltThrottle, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return m.Acquire(ctxTimeout, tenantID, oltID)
}

func (m *Manager) key(tenantID, oltID string) string { return tenantID + ":" + oltID }

// Acquire mengambil lock sesi; blok jika OLT sedang dipakai (dengan timeout ctx).
func (m *Manager) Acquire(ctx context.Context, tenantID, oltID string) (func(), *oltThrottle, error) {
	k := m.key(tenantID, oltID)
	m.mu.Lock()
	block := m.blocks[k]
	if block == nil {
		block = &sync.Mutex{}
		m.blocks[k] = block
	}
	throttle := m.throttles[k]
	if throttle == nil {
		throttle = newThrottle()
		m.throttles[k] = throttle
	}
	m.mu.Unlock()

	acquired := make(chan struct{})
	go func() {
		block.Lock()
		close(acquired)
	}()
	start := time.Now()
	select {
	case <-acquired:
		release := func() { block.Unlock() }
		return release, throttle, nil
	case <-ctx.Done():
		log.Printf("ztecli.Manager Acquire timeout [olt=%s] wait=%v", k, time.Since(start))
		return nil, nil, ErrLocked
	}
}

// ---------- Credentials ----------

type CLICredentials struct {
	Protocol       string // ssh | telnet
	Host           string
	Port           uint16
	Username       string
	Password       string
	EnablePassword string
}

// ---------- Session ----------

type Session struct {
	mode     string // "ssh" | "telnet"
	ssh      *ssh.Client
	telnet   net.Conn
	promptRe string
	throttle *oltThrottle
}

const (
	dialTimeout    = 8 * time.Second
	commandTimeout = 15 * time.Second
	readIdle       = 1500 * time.Millisecond
)

// Connect membuka sesi CLI sesuai protokol. Untuk telnet, koneksi dibungkus
// plaintext (ZTE lama); gunakan hanya dalam jaringan manajemen tertutup.
func Connect(ctx context.Context, creds CLICredentials, throttle *oltThrottle) (*Session, error) {
	switch strings.ToLower(creds.Protocol) {
	case "ssh":
		config := &ssh.ClientConfig{
			User: creds.Username,
			Auth: []ssh.AuthMethod{ssh.Password(creds.Password)},
			HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
				return nil
			},
			Timeout: dialTimeout,
			Config: ssh.Config{
				// OLT ZTE lawas (C320 V2.1.0) hanya mendukung cipher CBC.
				Ciphers:      []string{"aes128-cbc", "3des-cbc", "aes128-ctr", "aes192-ctr", "aes256-ctr", "aes128-gcm@openssh.com", "aes256-gcm@openssh.com", "chacha20-poly1305@openssh.com"},
				KeyExchanges: []string{"diffie-hellman-group-exchange-sha1", "diffie-hellman-group1-sha1", "diffie-hellman-group14-sha1", "curve25519-sha256", "ecdh-sha2-nistp256"},
				MACs:         []string{"hmac-sha1", "hmac-sha1-96", "hmac-sha2-256"},
			},
		}
		client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", creds.Host, creds.Port), config)
		if err != nil {
			if strings.Contains(err.Error(), "unable to authenticate") {
				return nil, ErrAuthFailed
			}
			return nil, fmt.Errorf("%w: %s", ErrUnreachable, err.Error())
		}
		return &Session{mode: "ssh", ssh: client, throttle: throttle}, nil
	case "telnet":
		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", creds.Host, creds.Port))
		if err != nil {
			if strings.Contains(err.Error(), "refused") || strings.Contains(err.Error(), "timeout") {
				return nil, fmt.Errorf("%w: %s", ErrUnreachable, err.Error())
			}
			return nil, err
		}
		sess := &Session{mode: "telnet", telnet: conn, throttle: throttle}
		// Login sequence sederhana ZTE: tunggu "Login:"/"Username:" lalu "Password:"
		if _, err := sess.readUntilAny([]string{"ogin:", "sername:"}, commandTimeout); err != nil {
			conn.Close()
			return nil, fmt.Errorf("%w: prompt login tidak muncul", ErrUnreachable)
		}
		sess.writeLine(creds.Username)
		if _, err := sess.readUntilAny([]string{"assword:"}, commandTimeout); err != nil {
			conn.Close()
			return nil, ErrAuthFailed
		}
		sess.writeLine(creds.Password)
		out, err := sess.readUntilAny([]string{">", "#", "assword:"}, commandTimeout)
		if err != nil || strings.Contains(out, "assword:") || out == "" {
			conn.Close()
			return nil, ErrAuthFailed
		}
		return sess, nil
	default:
		return nil, fmt.Errorf("protokol CLI tidak dikenal: %s", creds.Protocol)
	}
}

func findCLICommandErrorLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		low := strings.ToLower(trimmed)
		if strings.Contains(low, "%error") ||
			strings.Contains(low, "invalid input detected") ||
			strings.Contains(low, "invalid parameter") ||
			strings.Contains(low, "invalid command") ||
			strings.Contains(low, "unrecognized command") ||
			strings.Contains(low, "incomplete command") ||
			strings.Contains(low, "ambiguous command") {
			return trimmed
		}
	}
	return ""
}

// Execute menjalankan satu perintah yang sudah divalidasi.
func (s *Session) Execute(ctx context.Context, command string) (string, error) {
	if err := ValidateCommand(command); err != nil {
		return "", err
	}
	if err := s.throttle.wait(ctx); err != nil {
		return "", err
	}
	cctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	switch s.mode {
	case "ssh":
		session, err := s.ssh.NewSession()
		if err != nil {
			return "", fmt.Errorf("buka channel ssh: %w", err)
		}
		output, runErr := session.CombinedOutput(command)
		_ = session.Close()
		text := string(output)
		if (runErr != nil && strings.TrimSpace(text) == "") || strings.TrimSpace(text) == "" {
			// Banyak OLT ZTE tidak mendukung exec non-interaktif; fallback ke shell+PTY.
			fallback, fallbackErr := s.executeSSHViaShell(cctx, command)
			if strings.TrimSpace(fallback) != "" {
				text = fallback
			} else if fallbackErr != nil {
				if runErr != nil {
					return "", fmt.Errorf("eksekusi %q gagal (exec=%v, shell=%v)", command, runErr, fallbackErr)
				}
				return "", fallbackErr
			}
		}
		if runErr != nil && strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("eksekusi %q: %w", command, runErr)
		}
		if bad := findCLICommandErrorLine(text); bad != "" {
			return text, fmt.Errorf("eksekusi %q ditolak OLT: %s", command, bad)
		}
		return text, nil
	default:
		select {
		case <-cctx.Done():
			return "", fmt.Errorf("timeout perintah %q", command)
		default:
		}
		// Buang sisa output perintah sebelumnya (sesi telnet pooled) —
		// tanpa ini output ONU lain bisa terbaca oleh perintah berikutnya.
		s.drainStale()
		s.writeLine(command)
		outAll := ""
		for i := 0; i < 64; i++ {
			chunk, readErr := s.readUntilAny([]string{"---more---", "--more--", "more", "#", ">"}, commandTimeout)
			if chunk != "" {
				if outAll == "" {
					outAll = chunk
				} else {
					outAll += "\n" + chunk
				}
			}
			low := strings.ToLower(chunk)
			if strings.Contains(low, "--more--") || strings.Contains(low, "---more---") {
				_, _ = s.telnet.Write([]byte(" "))
				time.Sleep(40 * time.Millisecond)
				continue
			}
			if bad := findCLICommandErrorLine(outAll); bad != "" {
				return outAll, fmt.Errorf("eksekusi %q ditolak OLT: %s", command, bad)
			}
			return outAll, readErr
		}
		if bad := findCLICommandErrorLine(outAll); bad != "" {
			return outAll, fmt.Errorf("eksekusi %q ditolak OLT: %s", command, bad)
		}
		return outAll, nil
	}
}

// ExecuteSequence menjalankan beberapa command dalam satu konteks sesi.
// Penting untuk command bertingkat mode (configure -> interface -> aksi),
// terutama saat protokol SSH karena satu Execute() tidak mempertahankan mode.
func (s *Session) ExecuteSequence(ctx context.Context, commands []string) (string, error) {
	seq := make([]string, 0, len(commands))
	for _, raw := range commands {
		cmd := strings.TrimSpace(raw)
		if cmd == "" {
			continue
		}
		if err := ValidateCommand(cmd); err != nil {
			return "", err
		}
		seq = append(seq, cmd)
	}
	if len(seq) == 0 {
		return "", ErrForbiddenCmd
	}

	if s.mode != "ssh" {
		parts := make([]string, 0, len(seq))
		for _, cmd := range seq {
			out, err := s.Execute(ctx, cmd)
			if strings.TrimSpace(out) != "" {
				parts = append(parts, out)
			}
			if err != nil {
				return strings.Join(parts, "\n"), err
			}
		}
		return strings.Join(parts, "\n"), nil
	}

	for range seq {
		if err := s.throttle.wait(ctx); err != nil {
			return "", err
		}
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(len(seq)+1)*commandTimeout)
	defer cancel()

	sess, err := s.ssh.NewSession()
	if err != nil {
		return "", fmt.Errorf("buka shell ssh: %w", err)
	}
	defer sess.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	_ = sess.RequestPty("vt100", 200, 60, modes)

	var out bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &out

	stdin, err := sess.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin shell ssh: %w", err)
	}
	if err := sess.Shell(); err != nil {
		return "", fmt.Errorf("start shell ssh: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	time.Sleep(250 * time.Millisecond)
	_, _ = stdin.Write([]byte("terminal length 0\n"))
	for _, cmd := range seq {
		_, _ = stdin.Write([]byte(cmd + "\n"))
		time.Sleep(80 * time.Millisecond)
	}
	_, _ = stdin.Write([]byte("exit\n"))

	select {
	case <-cctx.Done():
		_ = sess.Close()
		return out.String(), fmt.Errorf("timeout eksekusi multi-command")
	case waitErr := <-done:
		text := out.String()
		if waitErr != nil && strings.TrimSpace(text) == "" {
			return "", waitErr
		}
		if bad := findCLICommandErrorLine(text); bad != "" {
			return text, fmt.Errorf("eksekusi multi-command ditolak OLT: %s", bad)
		}
		return text, nil
	}
}

func (s *Session) executeSSHViaShell(ctx context.Context, command string) (string, error) {
	sess, err := s.ssh.NewSession()
	if err != nil {
		return "", fmt.Errorf("buka shell ssh: %w", err)
	}
	defer sess.Close()

	// PTY dibutuhkan agar CLI network-device mengembalikan prompt/output normal.
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	_ = sess.RequestPty("vt100", 200, 60, modes)

	var out bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &out

	stdin, err := sess.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin shell ssh: %w", err)
	}
	if err := sess.Shell(); err != nil {
		return "", fmt.Errorf("start shell ssh: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	// tunggu prompt awal
	time.Sleep(250 * time.Millisecond)
	_, _ = stdin.Write([]byte("terminal length 0\n"))
	_, _ = stdin.Write([]byte(command + "\n"))
	_, _ = stdin.Write([]byte("exit\n"))

	select {
	case <-ctx.Done():
		_ = sess.Close()
		return out.String(), fmt.Errorf("timeout perintah %q", command)
	case waitErr := <-done:
		text := out.String()
		if waitErr != nil && strings.TrimSpace(text) == "" {
			return "", waitErr
		}
		return text, nil
	}
}

// Close menutup sesi dengan rapi (selalu exit dari config mode bila perlu).
func (s *Session) Close() {
	if s.mode == "ssh" {
		_ = s.ssh.Close()
		return
	}
	_, _ = s.Execute(context.Background(), "end")
	_, _ = s.Execute(context.Background(), "exit")
	_ = s.telnet.Close()
}

// ---------- Telnet helpers ----------

// drainStale membaca-dan-membuang sisa buffer socket telnet (output perintah
// sebelumnya yang timeout di tengah jalan) agar tidak terparse sebagai hasil
// perintah berikutnya.
func (s *Session) drainStale() {
	if s.telnet == nil {
		return
	}
	chunk := make([]byte, 2048)
	for i := 0; i < 16; i++ {
		s.telnet.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
		n, err := s.telnet.Read(chunk)
		if n <= 0 || err != nil {
			return
		}
	}
}

func (s *Session) writeLine(line string) {
	_, _ = s.telnet.Write([]byte(line + "\r\n"))
	time.Sleep(80 * time.Millisecond)
}

// readUntilAny membaca sampai salah satu marker muncul atau timeout.
func (s *Session) readUntilAny(markers []string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	buffer := make([]byte, 0, 4096)
	chunk := make([]byte, 1024)
	for time.Now().Before(deadline) {
		s.telnet.SetReadDeadline(time.Now().Add(readIdle))
		n, err := s.telnet.Read(chunk)
		if n > 0 {
			buffer = append(buffer, chunk[:n]...)
			lowerBuf := strings.ToLower(string(buffer))
			for _, marker := range markers {
				if strings.Contains(lowerBuf, marker) {
					return cleanTelnet(buffer), nil
				}
			}
		}
		if err != nil {
			if os.IsTimeout(err) {
				// idle read: jika sudah ada data dan tidak bertambah, anggap selesai
				if len(buffer) > 0 {
					time.Sleep(200 * time.Millisecond)
					s.telnet.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
					extra := make([]byte, 1024)
					if n2, _ := s.telnet.Read(extra); n2 > 0 {
						buffer = append(buffer, extra[:n2]...)
						continue
					}
					return cleanTelnet(buffer), nil
				}
				continue
			}
			return cleanTelnet(buffer), err
		}
	}
	return cleanTelnet(buffer), nil
}

// cleanTelnet membuang byte negosiasi IAC telnet sederhana.
func cleanTelnet(data []byte) string {
	filtered := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		b := data[i]
		if b == 255 { // IAC
			if i+2 < len(data) {
				i += 2
			}
			continue
		}
		filtered = append(filtered, b)
	}
	return strings.TrimSpace(string(filtered))
}

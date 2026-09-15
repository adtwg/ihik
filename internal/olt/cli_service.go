package olt

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"isp-billing/internal/zte"
	"isp-billing/internal/ztecli"
)

// CLICredentialsFor memuat kredensial CLI terdekripsi untuk sebuah OLT.
func (service *Service) CLICredentialsFor(ctx context.Context, tenantID, id string) (ztecli.CLICredentials, error) {
	if err := service.requireKey(); err != nil {
		return ztecli.CLICredentials{}, err
	}
	raw, err := service.repository.Credentials(ctx, tenantID, id)
	if err != nil {
		return ztecli.CLICredentials{}, err
	}
	getStr := func(key string) string {
		value, _ := raw[key].(string)
		return value
	}
	creds := ztecli.CLICredentials{
		Protocol: getStr("cli_protocol"),
		Host:     getStr("host"),
		Port:     uint16(getInt(raw["cli_port"])),
		Username: getStr("cli_username"),
	}
	if creds.Protocol == "" {
		creds.Protocol = "ssh"
	}
	if creds.Port == 0 {
		if creds.Protocol == "telnet" {
			creds.Port = 23
		} else {
			creds.Port = 22
		}
	}
	for label, target := range map[string]*string{
		"cli_password_ciphertext":        &creds.Password,
		"cli_enable_password_ciphertext": &creds.EnablePassword,
	} {
		ciphertext, ok := raw[label].([]byte)
		if !ok || len(ciphertext) == 0 {
			continue
		}
		plaintext, decErr := decryptSecret(service.encryptionKey, ciphertext)
		if decErr != nil {
			return ztecli.CLICredentials{}, fmt.Errorf("dekripsi kredensial CLI: %w", decErr)
		}
		*target = plaintext
	}
	return creds, nil
}

// withCLISession menjalankan fungsi di dalam sesi CLI yang ter-lock dan
// throttled per OLT. Menjamin tidak ada sesi paralel ke OLT yang sama.
// Lock hanya menunggu selama sisa timeout ctx, jadi request tidak menggantung.
func (service *Service) withCLISession(ctx context.Context, tenantID, id string, fn func(session *ztecli.Session) (string, error)) (string, error) {
	release, throttle, err := service.cliManager.Acquire(ctx, tenantID, id)
	if err != nil {
		return "", err // ErrLocked
	}
	defer release()

	creds, err := service.CLICredentialsFor(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	session, err := ztecli.Connect(ctx, creds, throttle)
	if err != nil {
		return "", err
	}
	defer session.Close()
	return fn(session)
}

// ExecuteCLIShow menjalankan perintah show mentah untuk diagnostik internal.
func (service *Service) ExecuteCLIShow(ctx context.Context, tenantID, id, command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" || !strings.HasPrefix(strings.ToLower(command), "show ") {
		return "", ErrForbiddenCmd
	}
	return service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
		return session.Execute(ctx, command)
	})
}

var (
	serialPattern       = regexp.MustCompile(`(?i)(?:ZTEG[A-F0-9]{8,14}|[A-F0-9]{12,16})`)
	ponInlinePattern    = regexp.MustCompile(`(?i)gpon-(?:olt|onu)_(\d+/\d+/\d+)`)
	onuIDInlinePattern  = regexp.MustCompile(`(?i)(?:onu(?:-?id)?|id)\s*[:=]?\s*(\d{1,3})`)
	onuNumberRowPattern = regexp.MustCompile(`^(\d{1,3})$`)
)

// UnconfiguredONU adalah ONU yang terdeteksi fisik tapi belum provisioned.
type UnconfiguredONU struct {
	SerialNumber   string `json:"serial_number"`
	PONPort        string `json:"pon_port"`
	ONUIDHint      int    `json:"onu_id_hint,omitempty"`
	SuggestedONUID int    `json:"suggested_onu_id,omitempty"`
}

type UnconfiguredONUPortSummary struct {
	PONPort           string `json:"pon_port"`
	OccupiedONUIDs    []int  `json:"occupied_onu_ids,omitempty"`
	EmptyONUIDs       []int  `json:"empty_onu_ids,omitempty"`
	NextONUID         int    `json:"next_onu_id,omitempty"`
	UnconfiguredCount int    `json:"unconfigured_count"`
}

type UnconfiguredONUBulkResult struct {
	Items             []UnconfiguredONU            `json:"items"`
	Ports             []UnconfiguredONUPortSummary `json:"ports,omitempty"`
	TotalUnconfigured int                          `json:"total_unconfigured"`
}

// ListUnconfiguredONUs membaca SN ONU yang belum terdaftar di PON.
// Perintah: show gpon onu uncfg gpon-olt_<port>
func (service *Service) ListUnconfiguredONUs(ctx context.Context, tenantID, id, ponPort string) ([]UnconfiguredONU, error) {
	result, err := service.ListUnconfiguredONUsBulk(ctx, tenantID, id, ponPort)
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

// ListUnconfiguredONUsBulk mendukung scan unconfigured lintas port PON.
// Jika ponPort kosong maka scan semua port yang terdeteksi dari cache ONU DB.
func (service *Service) ListUnconfiguredONUsBulk(ctx context.Context, tenantID, id, ponPort string) (UnconfiguredONUBulkResult, error) {
	sanitizedPON := ""
	if strings.TrimSpace(ponPort) != "" {
		sanitizedPON = sanitizePON(ponPort)
		if sanitizedPON == "" {
			return UnconfiguredONUBulkResult{}, ErrInvalidInput
		}
	}

	onus, err := service.repository.ListONUs(ctx, tenantID, id)
	if err != nil {
		return UnconfiguredONUBulkResult{}, err
	}
	occupancy := buildONUPortOccupancy(onus)

	targetPorts := make([]string, 0)
	if sanitizedPON != "" {
		targetPorts = append(targetPorts, sanitizedPON)
	} else {
		for port := range occupancy {
			targetPorts = append(targetPorts, port)
		}
		sort.Strings(targetPorts)
	}
	if len(targetPorts) == 0 {
		return UnconfiguredONUBulkResult{Items: []UnconfiguredONU{}, Ports: []UnconfiguredONUPortSummary{}}, nil
	}

	portSummaries := make(map[string]*UnconfiguredONUPortSummary, len(targetPorts))
	suggestQueues := make(map[string][]int, len(targetPorts))
	for _, port := range targetPorts {
		used := occupancy[port]
		empty := findEmptyONUIDs(used, 128)
		occupied := sortedONUIDs(used)
		nextID := 0
		if len(empty) > 0 {
			nextID = empty[0]
		}
		portSummaries[port] = &UnconfiguredONUPortSummary{
			PONPort:        port,
			OccupiedONUIDs: occupied,
			EmptyONUIDs:    empty,
			NextONUID:      nextID,
		}
		suggestQueues[port] = append([]int(nil), empty...)
	}

	rawItems := make([]UnconfiguredONU, 0)
	_, err = service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
		for _, port := range targetPorts {
			output, execErr := session.Execute(ctx, "show gpon onu uncfg gpon-olt_"+port)
			if execErr != nil {
				return "", execErr
			}
			rawItems = append(rawItems, parseUncfgONUs(output, port)...)
		}
		return "", nil
	})
	if err != nil {
		return UnconfiguredONUBulkResult{}, err
	}

	items := dedupeUnconfiguredONUs(rawItems)
	for i := range items {
		port := items[i].PONPort
		if port == "" {
			continue
		}
		if summary := portSummaries[port]; summary != nil {
			summary.UnconfiguredCount++
		}
		queue := suggestQueues[port]
		suggested, nextQueue := pickSuggestedONUID(items[i].ONUIDHint, queue)
		items[i].SuggestedONUID = suggested
		suggestQueues[port] = nextQueue
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].PONPort == items[j].PONPort {
			if items[i].SuggestedONUID != items[j].SuggestedONUID {
				return items[i].SuggestedONUID < items[j].SuggestedONUID
			}
			return items[i].SerialNumber < items[j].SerialNumber
		}
		return items[i].PONPort < items[j].PONPort
	})

	ports := make([]UnconfiguredONUPortSummary, 0, len(targetPorts))
	for _, port := range targetPorts {
		if summary := portSummaries[port]; summary != nil {
			ports = append(ports, *summary)
		}
	}

	return UnconfiguredONUBulkResult{
		Items:             items,
		Ports:             ports,
		TotalUnconfigured: len(items),
	}, nil
}

// parseUncfgONUs mem-parse output "show gpon onu uncfg".
// Format ZTE bervariasi; parser ini toleran berbagai bentuk row.
func parseUncfgONUs(output, fallbackPON string) []UnconfiguredONU {
	results := make([]UnconfiguredONU, 0)
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		serial := extractSerialFromLine(line)
		if serial == "" {
			continue
		}
		pon := fallbackPON
		if m := ponInlinePattern.FindStringSubmatch(line); len(m) == 2 {
			if parsed := sanitizePON(m[1]); parsed != "" {
				pon = parsed
			}
		}
		hint := extractONUIDHintFromLine(line)
		results = append(results, UnconfiguredONU{
			SerialNumber: serial,
			PONPort:      pon,
			ONUIDHint:    hint,
		})
	}
	return results
}

func extractSerialFromLine(line string) string {
	line = strings.ToUpper(strings.TrimSpace(line))
	if line == "" {
		return ""
	}
	matched := serialPattern.FindString(line)
	if matched == "" {
		return ""
	}
	matched = strings.Trim(matched, " ,;()[]{}")
	if strings.HasPrefix(matched, "ZTEG") {
		suffix := strings.TrimPrefix(matched, "ZTEG")
		if len(suffix) >= 8 && len(suffix) <= 14 && isHexLike(suffix) {
			return matched
		}
		return ""
	}
	if len(matched) >= 12 && len(matched) <= 16 && isHexLike(matched) {
		return matched
	}
	return ""
}

func extractONUIDHintFromLine(line string) int {
	if m := onuIDInlinePattern.FindStringSubmatch(line); len(m) == 2 {
		if id, err := strconv.Atoi(m[1]); err == nil && id >= 1 && id <= 128 {
			return id
		}
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return 0
	}
	for i := 0; i < len(fields) && i < 3; i++ {
		token := strings.Trim(fields[i], " ,;()[]{}")
		if !onuNumberRowPattern.MatchString(token) {
			continue
		}
		if id, err := strconv.Atoi(token); err == nil && id >= 1 && id <= 128 {
			return id
		}
	}
	return 0
}

func dedupeUnconfiguredONUs(items []UnconfiguredONU) []UnconfiguredONU {
	result := make([]UnconfiguredONU, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		serial := strings.ToUpper(strings.TrimSpace(item.SerialNumber))
		if serial == "" {
			continue
		}
		pon := sanitizePON(item.PONPort)
		key := pon + "|" + serial
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		item.SerialNumber = serial
		item.PONPort = pon
		result = append(result, item)
	}
	return result
}

func buildONUPortOccupancy(onus []zte.ONU) map[string]map[int]struct{} {
	occupancy := make(map[string]map[int]struct{})
	for _, onu := range onus {
		pon, onuID, ok := splitIndex(onu.ONUNumber)
		if !ok {
			pon, onuID, ok = splitIndex(onu.Index)
		}
		if !ok {
			continue
		}
		pon = sanitizePON(pon)
		if pon == "" || onuID < 1 || onuID > 128 {
			continue
		}
		if _, exists := occupancy[pon]; !exists {
			occupancy[pon] = make(map[int]struct{})
		}
		occupancy[pon][onuID] = struct{}{}
	}
	return occupancy
}

func sortedONUIDs(used map[int]struct{}) []int {
	if len(used) == 0 {
		return []int{}
	}
	ids := make([]int, 0, len(used))
	for id := range used {
		if id >= 1 && id <= 128 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

func findEmptyONUIDs(used map[int]struct{}, maxID int) []int {
	if maxID < 1 {
		maxID = 128
	}
	result := make([]int, 0, maxID)
	for id := 1; id <= maxID; id++ {
		if _, ok := used[id]; ok {
			continue
		}
		result = append(result, id)
	}
	return result
}

func pickSuggestedONUID(hint int, queue []int) (int, []int) {
	if len(queue) == 0 {
		return 0, queue
	}
	if hint >= 1 {
		for i, candidate := range queue {
			if candidate == hint {
				next := append([]int{}, queue[:i]...)
				next = append(next, queue[i+1:]...)
				return candidate, next
			}
		}
	}
	return queue[0], queue[1:]
}

func isHexLike(s string) bool {
	for _, ch := range s {
		ok := (ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'F')
		if !ok {
			return false
		}
	}
	return true
}

// sanitizePON memastikan format gpon-olt_ aman (mis. "1/2/1").
func sanitizePON(port string) string {
	port = strings.TrimSpace(port)
	parts := strings.Split(port, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return ""
	}
	for _, part := range parts {
		if part == "" || len(part) > 3 {
			return ""
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return ""
			}
		}
	}
	return port
}

// ProvisionONU mendaftarkan ONU baru via CLI dengan mode-aware sequence.
// Alur dibanding PHP legacy yang stabil:
//  1. masuk mode konfigurasi (fallback configure/configure terminal)
//  2. interface gpon-olt_<pon>
//  3. onu <id> type <tipe> sn <serial>
//  4. (opsional) interface gpon-onu_<pon>:<id> -> description ...
func (service *Service) ProvisionONU(ctx context.Context, tenantID, id, ponPort string, onuID int, onuType, serial, description string) ([]string, error) {
	ponPort = sanitizePON(ponPort)
	if ponPort == "" || onuID < 1 || onuID > 128 {
		return nil, ErrInvalidInput
	}
	if !validONUType(onuType) {
		return nil, fmt.Errorf("%w: tipe ONU tidak valid", ErrInvalidInput)
	}
	serial = strings.ToUpper(strings.TrimSpace(serial))
	if len(serial) < 8 || len(serial) > 16 || !isHexLike(strings.TrimPrefix(serial, "ZTEG")) && !strings.HasPrefix(serial, "ZTEG") {
		return nil, fmt.Errorf("%w: format serial tidak valid", ErrInvalidInput)
	}
	description = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '"' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, description))

	// Jalankan body command dalam satu ExecuteSequence + fallback preamble.
	runWithFallback := func(session *ztecli.Session, body []string) ([]string, error) {
		makeSeq := func(preamble ...string) []string {
			seq := make([]string, 0, len(preamble)+len(body)+1)
			seq = append(seq, preamble...)
			seq = append(seq, body...)
			seq = append(seq, "end")
			return seq
		}
		sequences := [][]string{
			makeSeq(),
			makeSeq("configure terminal"),
			makeSeq("configure"),
			makeSeq("enable", "configure terminal"),
			makeSeq("enable", "configure"),
		}

		attempted := make([]string, 0, 48)
		errMsgs := make([]string, 0, len(sequences))
		for _, seq := range sequences {
			_, err := session.ExecuteSequence(ctx, seq)
			attempted = append(attempted, seq...)
			if err == nil {
				return attempted, nil
			}
			errMsgs = append(errMsgs, err.Error())
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "forbidden") || strings.Contains(low, "rate limit") || strings.Contains(low, "autentikasi") {
				return attempted, err
			}
		}
		return attempted, fmt.Errorf("%s", strings.Join(errMsgs, " || "))
	}

	registerBody := []string{
		"interface gpon-olt_" + ponPort,
		fmt.Sprintf("onu %d type %s sn %s", onuID, onuType, serial),
		"exit",
	}

	var executed []string
	_, err := service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
		execReg, regErr := runWithFallback(session, registerBody)
		executed = append(executed, execReg...)
		if regErr != nil {
			return "", regErr
		}

		if description != "" {
			descCandidates := [][]string{
				{
					fmt.Sprintf("interface gpon-onu_%s:%d", ponPort, onuID),
					fmt.Sprintf("description %s", description),
					"exit",
				},
				{
					fmt.Sprintf("interface gpon-olt_%s", ponPort),
					fmt.Sprintf("onu %d description %s", onuID, description),
					"exit",
				},
			}
			var descErrs []string
			success := false
			for _, body := range descCandidates {
				execDesc, dErr := runWithFallback(session, body)
				executed = append(executed, execDesc...)
				if dErr == nil {
					success = true
					break
				}
				descErrs = append(descErrs, dErr.Error())
			}
			if !success {
				return "", fmt.Errorf("gagal set description: %s", strings.Join(descErrs, " || "))
			}
		}
		return "", nil
	})
	if err != nil {
		return executed, err
	}
	return executed, nil
}

// validONUType membatasi karakter tipe ONU (mis. ZTE-F670L).
func validONUType(t string) bool {
	if t == "" || len(t) > 32 {
		return false
	}
	for _, ch := range t {
		ok := ch == '-' || ch == '_' || ch == '.' ||
			(ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')
		if !ok {
			return false
		}
	}
	return true
}

// ONUOpticalSample hasil parsing nilai optik per-ONU dari CLI.
type ONUOpticalSample struct {
	PONPort   string  `json:"pon_port,omitempty"`
	ONUID     int     `json:"onu_id,omitempty"`
	Rx        float64 `json:"rx_dbm"`
	Tx        float64 `json:"tx_dbm"`
	DistanceM float64 `json:"distance_m"`
	Command   string  `json:"command,omitempty"`
	Raw       string  `json:"raw,omitempty"`
	Status    string  `json:"status,omitempty"`
}

var (
	rxPowerRegex   = regexp.MustCompile(`(?i)rx\s*(?:optical\s*)?power\s*\(?[^\)]*dBm\)?[:\s]+(-?\d+(?:\.\d+)?)`)
	txPowerRegex   = regexp.MustCompile(`(?i)tx\s*(?:optical\s*)?power\s*\(?[^\)]*dBm\)?[:\s]+(-?\d+(?:\.\d+)?)`)
	distanceRegex  = regexp.MustCompile(`(?i)(?:onu\s*)?(?:distance|distancia)\s*[:\s]+(-?\d+(?:\.\d+)?)\s*m?`)
	attenUpPair    = regexp.MustCompile(`(?i)\bup\b.*?rx\s*[:\s]+(-?\d+(?:\.\d+)?)\s*\(?dbm\)?.*?tx\s*[:\s]+(-?\d+(?:\.\d+)?)\s*\(?dbm\)?`)
	attenUpPairAlt = regexp.MustCompile(`(?i)\bup\b.*?tx\s*[:\s]+(-?\d+(?:\.\d+)?)\s*\(?dbm\)?.*?rx\s*[:\s]+(-?\d+(?:\.\d+)?)\s*\(?dbm\)?`)
	attenDownPair  = regexp.MustCompile(`(?i)\bdown\b.*?tx\s*[:\s]+(-?\d+(?:\.\d+)?)\s*\(?dbm\)?.*?rx\s*[:\s]+(-?\d+(?:\.\d+)?)\s*\(?dbm\)?`)
	attenDownAlt   = regexp.MustCompile(`(?i)\bdown\b.*?rx\s*[:\s]+(-?\d+(?:\.\d+)?)\s*\(?dbm\)?.*?tx\s*[:\s]+(-?\d+(?:\.\d+)?)\s*\(?dbm\)?`)
	attenDistRegex = regexp.MustCompile(`(?i)(?:distance|distancia)\s*(?:da\s*onu)?[:\s]+(-?\d+(?:\.\d+)?)\s*m?`)
)

// ProbeONUOpticalCLI mengambil nilai Rx/Tx dBm dan jarak per-ONU lewat CLI.
// Dicoba beberapa varian command karena output antar firmware berbeda.
func (service *Service) ProbeONUOpticalCLI(ctx context.Context, tenantID, id, ponPort string, onuID int) (*ONUOpticalSample, map[string]string, error) {
	ponPort = sanitizePON(ponPort)
	if ponPort == "" || onuID < 1 || onuID > 128 {
		return nil, nil, ErrInvalidInput
	}

	candidates := []string{
		// Fast-path C320: perintah ini paling stabil untuk ambil redaman ONU.
		fmt.Sprintf("show pon power attenuation gpon-onu_%s:%d", ponPort, onuID),
		// Ambil jarak cepat jika command distance tersedia.
		fmt.Sprintf("show gpon onu distance gpon-onu_%s:%d", ponPort, onuID),
		// Variasi firmware/fallback:
		fmt.Sprintf("show gpon onu detail-info gpon-onu_%s:%d", ponPort, onuID),
		fmt.Sprintf("show gpon onu detail-info gpon-olt_%s %d", ponPort, onuID),
		fmt.Sprintf("show gpon onu optical-info gpon-onu_%s:%d", ponPort, onuID),
		fmt.Sprintf("show gpon onu optical-info gpon-olt_%s %d", ponPort, onuID),
		fmt.Sprintf("show int optical-info gpon-onu_%s:%d", ponPort, onuID),
		fmt.Sprintf("show int optical-info gpon-olt_%s %d", ponPort, onuID),
		fmt.Sprintf("show pon power onu-rx gpon-onu_%s:%d", ponPort, onuID),
		fmt.Sprintf("show pon power onu-tx gpon-onu_%s:%d", ponPort, onuID),
		fmt.Sprintf("show pon power onu-rx gpon-olt_%s %d", ponPort, onuID),
		fmt.Sprintf("show pon power onu-tx gpon-olt_%s %d", ponPort, onuID),
		fmt.Sprintf("show interface gpon-onu_%s:%d optical-info", ponPort, onuID),
	}

	raws := make(map[string]string, len(candidates))
	var firstErr error
	var best *ONUOpticalSample
	bestScore := -1
	merged := &ONUOpticalSample{}

	score := func(s *ONUOpticalSample) int {
		total := 0
		if s == nil {
			return total
		}
		if s.Rx != 0 {
			total++
		}
		if s.Tx != 0 {
			total++
		}
		if s.DistanceM != 0 {
			total++
		}
		return total
	}

	output, err := service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
		for _, cmd := range candidates {
			out, execErr := session.Execute(ctx, cmd)
			if execErr != nil {
				if firstErr == nil {
					firstErr = execErr
				}
				continue
			}
			raws[cmd] = out
			if sample := parseONUOpticalCLI(out); sample != nil {
				if cmd == fmt.Sprintf("show pon power attenuation gpon-onu_%s:%d", ponPort, onuID) {
					if sample.Rx == 0 && sample.Tx == 0 {
						continue
					}
					// Prioritaskan redaman dari attenuation C320
					sample.PONPort = ponPort
					sample.ONUID = onuID
					sample.Raw = out
					sample.Command = cmd
					return out, nil
				}
				mergeOpticalSample(merged, sample)
				sc := score(sample)
				if sample != nil && sc >= bestScore {
					sample.PONPort = ponPort
					sample.ONUID = onuID
					sample.Raw = out
					sample.Command = cmd
					best = sample
					bestScore = sc
				}
				// Fast mode: cukup Rx+Tx agar UI cepat terisi; distance bisa fallback dari cache.
				if sc >= 2 {
					return out, nil
				}
				if score(merged) >= 2 {
					return out, nil
				}
			}
		}
		if score(merged) > 0 {
			return merged.Raw, nil
		}
		if best != nil {
			return best.Raw, nil
		}
		return "", fmt.Errorf("tidak ada command optik yang menghasilkan nilai")
	})
	if err != nil {
		if score(merged) > 0 {
			merged.PONPort = ponPort
			merged.ONUID = onuID
			return merged, raws, nil
		}
		if best != nil {
			return best, raws, nil
		}
		if firstErr != nil {
			return nil, raws, firstErr
		}
		return nil, raws, err
	}

	if best != nil {
		mergeOpticalSample(best, merged)
		return best, raws, nil
	}
	sample := parseONUOpticalCLI(output)
	if sample == nil {
		sample = &ONUOpticalSample{}
	}
	mergeOpticalSample(sample, merged)
	sample.PONPort = ponPort
	sample.ONUID = onuID
	sample.Raw = output
	return sample, raws, nil
}

func mergeOpticalSample(dst, src *ONUOpticalSample) {
	if dst == nil || src == nil {
		return
	}
	if dst.Rx == 0 && src.Rx != 0 {
		dst.Rx = src.Rx
	}
	if dst.Tx == 0 && src.Tx != 0 {
		dst.Tx = src.Tx
	}
	if dst.DistanceM == 0 && src.DistanceM != 0 {
		dst.DistanceM = src.DistanceM
	}
	if src.Raw != "" {
		dst.Raw = src.Raw
	}
	if src.Command != "" {
		dst.Command = src.Command
	}
}

// probeAttenuationBatchForPON menembak command attenuation per-ONU dalam SATU
// sesi CLI agar jauh lebih cepat daripada buka sesi baru per ONU.
func (service *Service) probeAttenuationBatchForPON(ctx context.Context, tenantID, id, ponPort string, onuIDs []int) (map[int]*ONUOpticalSample, error) {
	ponPort = sanitizePON(ponPort)
	results := make(map[int]*ONUOpticalSample)
	if ponPort == "" || len(onuIDs) == 0 {
		return results, nil
	}
	_, err := service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
		for _, onuID := range onuIDs {
			if onuID < 1 || onuID > 128 {
				continue
			}
			candidates := []string{
				fmt.Sprintf("show pon power attenuation gpon-onu_%s:%d", ponPort, onuID),
			}
			var merged *ONUOpticalSample
			for _, cmd := range candidates {
				out, execErr := session.Execute(ctx, cmd)
				if execErr != nil {
					continue
				}
				s := parseONUOpticalCLI(out)
				if s == nil {
					continue
				}
				s.Command = cmd
				s.Raw = out
				if merged == nil {
					merged = s
				} else {
					mergeOpticalSample(merged, s)
				}
				if merged.Rx != 0 && merged.Tx != 0 {
					break
				}
			}
			if merged != nil && (merged.Rx != 0 || merged.Tx != 0) {
				merged.PONPort = ponPort
				merged.ONUID = onuID
				results[onuID] = merged
			}
		}
		return "", nil
	})
	if err != nil && len(results) == 0 {
		return results, err
	}
	return results, nil
}

// ProbeAllOpticalForPON mengambil semua nilai optik ONU pada satu PON port
// dalam satu command (jika OLT mendukung format tabel).
func (service *Service) ProbeAllOpticalForPON(ctx context.Context, tenantID, id, ponPort string) (map[int]*ONUOpticalSample, error) {
	ponPort = sanitizePON(ponPort)
	if ponPort == "" {
		return nil, ErrInvalidInput
	}
	candidates := []string{
		// Fast-path C320: tabel attenuation per PON paling sering tersedia.
		fmt.Sprintf("show pon power attenuation gpon-olt_%s", ponPort),
		// Fallback lintas firmware:
		fmt.Sprintf("show gpon onu optical-info gpon-olt_%s", ponPort),
		fmt.Sprintf("show interface gpon-olt_%s optical-info", ponPort),
		fmt.Sprintf("show int optical-info gpon-olt_%s", ponPort),
		fmt.Sprintf("show gpon onu optical-info gpon-onu_%s:1", ponPort),
		fmt.Sprintf("show int optical-info gpon-onu_%s:1", ponPort),
	}
	var lastErr error
	for _, cmd := range candidates {
		out, err := service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
			return session.Execute(ctx, cmd)
		})
		if err != nil {
			lastErr = err
			continue
		}
		parsed := parsePONAttenuationTable(out, ponPort)
		if len(parsed) == 0 {
			parsed = parsePONOpticalTable(out, ponPort)
		}
		if len(parsed) > 0 {
			for _, s := range parsed {
				s.Command = cmd
			}
			return parsed, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return map[int]*ONUOpticalSample{}, nil
}

// parsePONOpticalTable parsing output format:
// OnuId OLT-Rx(dBm) ONU-Rx(dBm) ONU-Tx(dBm) Temp(C) Voltage(V) Current(mA)
//
//	1     -18.45      -19.23       2.35     42.5     3.28       15.2
func parsePONOpticalTable(output, ponPort string) map[int]*ONUOpticalSample {
	results := make(map[int]*ONUOpticalSample)
	dataStarted := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "onuid") || strings.Contains(low, "onu-id") || strings.Contains(low, "olt-rx") || strings.Contains(low, "onu-rx") {
			dataStarted = true
			continue
		}
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "===") {
			continue
		}
		if !dataStarted {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		onuID, ok := parseCLIOnuIDToken(fields[0])
		if !ok || onuID < 1 {
			continue
		}
		nums := make([]float64, 0, 8)
		for _, token := range fields[1:] {
			if v, ok := parseCLINumberToken(token); ok {
				nums = append(nums, v)
			}
		}
		if len(nums) < 2 {
			continue
		}

		// Mayoritas output ZTE optical-info:
		// OnuId OLT-Rx(dBm) ONU-Rx(dBm) ONU-Tx(dBm) ...
		// jadi Rx ONU = angka ke-2, Tx ONU = angka ke-3 setelah OnuId.
		rx := nums[0]
		tx := nums[1]
		if len(nums) >= 3 {
			rx = nums[1]
			tx = nums[2]
		}

		distance := 0.0
		if len(nums) >= 4 {
			for _, cand := range nums[3:] {
				// Jarak ONU umumnya integer >= 50m; hindari suhu/arus.
				if cand >= 50 && cand <= 50000 {
					distance = cand
					break
				}
			}
		}

		results[onuID] = &ONUOpticalSample{
			PONPort:   ponPort,
			ONUID:     onuID,
			Rx:        rx,
			Tx:        tx,
			DistanceM: distance,
			Command:   "show gpon onu optical-info gpon-olt_" + ponPort,
			Raw:       output,
		}
	}
	return results
}

// parsePONAttenuationTable parsing output `show pon power attenuation gpon-olt_<pon>`.
// Format umum (per ONU block):
//
//	gpon-onu_1/1/1:1
//	up   Rx :-24.789(dbm) Tx:2.582(dbm) ...
//	down Tx :6.704(dbm)   Rx:-19.747(dbm) ...
func parsePONAttenuationTable(output, ponPort string) map[int]*ONUOpticalSample {
	results := make(map[int]*ONUOpticalSample)
	currentID := 0
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "%error") || strings.Contains(low, "invalid") {
			return map[int]*ONUOpticalSample{}
		}

		// Tangkap konteks ONU dari token gpon-onu_<pon>:<id>
		if i := strings.Index(low, "gpon-onu_"); i >= 0 {
			sub := line[i:]
			if fields := strings.Fields(sub); len(fields) > 0 {
				if id, ok := parseCLIOnuIDToken(fields[0]); ok {
					currentID = id
					if _, exists := results[currentID]; !exists {
						results[currentID] = &ONUOpticalSample{PONPort: ponPort, ONUID: currentID}
					}
				}
			}
		}
		if currentID < 1 {
			continue
		}

		s := results[currentID]
		if s == nil {
			s = &ONUOpticalSample{PONPort: ponPort, ONUID: currentID}
			results[currentID] = s
		}
		if m := attenUpPair.FindStringSubmatch(low); len(m) == 3 {
			// Untuk ONU: upstream Tx adalah ONU Tx
			if v, err := strconv.ParseFloat(m[2], 64); err == nil && v != 0 {
				s.Tx = v
			}
		} else if m := attenUpPairAlt.FindStringSubmatch(low); len(m) == 3 {
			// Variasi firmware: urutan token bisa Tx lalu Rx.
			if v, err := strconv.ParseFloat(m[1], 64); err == nil && v != 0 {
				s.Tx = v
			}
		}
		if m := attenDownPair.FindStringSubmatch(low); len(m) == 3 {
			// Untuk ONU: downstream Rx adalah ONU Rx
			if v, err := strconv.ParseFloat(m[2], 64); err == nil && v != 0 {
				s.Rx = v
			}
		} else if m := attenDownAlt.FindStringSubmatch(low); len(m) == 3 {
			// Variasi firmware: urutan token bisa Rx lalu Tx.
			if v, err := strconv.ParseFloat(m[1], 64); err == nil && v != 0 {
				s.Rx = v
			}
		}
	}

	// Bersihkan entri kosong
	for id, s := range results {
		if s == nil || (s.Rx == 0 && s.Tx == 0) {
			delete(results, id)
		}
	}
	return results
}

func parseCLINumberToken(token string) (float64, bool) {
	clean := strings.TrimSpace(token)
	clean = strings.Trim(clean, ",;:()[]")
	if clean == "" {
		return 0, false
	}
	lower := strings.ToLower(clean)
	if lower == "n/a" || lower == "na" || lower == "--" || lower == "-" {
		return 0, false
	}
	lower = strings.TrimSuffix(lower, "dbm")
	lower = strings.TrimSuffix(lower, "m")
	lower = strings.ReplaceAll(lower, ",", "")
	v, err := strconv.ParseFloat(lower, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseCLIOnuIDToken(token string) (int, bool) {
	clean := strings.Trim(strings.ToLower(strings.TrimSpace(token)), ":")
	if clean == "" {
		return 0, false
	}
	if id, err := strconv.Atoi(clean); err == nil && id > 0 {
		return id, true
	}
	if i := strings.LastIndex(clean, ":"); i >= 0 && i+1 < len(clean) {
		if id, err := strconv.Atoi(clean[i+1:]); err == nil && id > 0 {
			return id, true
		}
	}
	return 0, false
}

// parseONUOpticalCLI parsing nilai optik dari output command ZTE.
func parseONUOpticalCLI(output string) *ONUOpticalSample {
	// Coba parse tabel terlebih dahulu (single-row hasil PON-wide command).
	if table := parsePONOpticalTable(output, ""); len(table) == 1 {
		for _, s := range table {
			return s
		}
	}
	var rx, tx, dist float64
	inDistanceTable := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" {
			continue
		}
		if strings.Contains(line, "distance(m)") {
			inDistanceTable = true
			continue
		}
		if inDistanceTable {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if v, ok := parseCLINumberToken(fields[len(fields)-1]); ok && v >= 1 && dist == 0 {
					dist = v
				}
			}
		}
		if m := attenUpPair.FindStringSubmatch(line); len(m) == 3 {
			_, _ = strconv.ParseFloat(m[1], 64)
			upTx, _ := strconv.ParseFloat(m[2], 64)
			// Untuk ONU: upstream Tx adalah ONU Tx.
			if upTx != 0 {
				tx = upTx
			}
		} else if m := attenUpPairAlt.FindStringSubmatch(line); len(m) == 3 {
			upTx, _ := strconv.ParseFloat(m[1], 64)
			if upTx != 0 {
				tx = upTx
			}
		}
		if m := attenDownPair.FindStringSubmatch(line); len(m) == 3 {
			_, _ = strconv.ParseFloat(m[1], 64)
			downRx, _ := strconv.ParseFloat(m[2], 64)
			// Untuk ONU: downstream Rx adalah ONU Rx.
			if downRx != 0 {
				rx = downRx
			}
		} else if m := attenDownAlt.FindStringSubmatch(line); len(m) == 3 {
			downRx, _ := strconv.ParseFloat(m[1], 64)
			if downRx != 0 {
				rx = downRx
			}
		}
		if m := rxPowerRegex.FindStringSubmatch(line); m != nil && rx == 0 {
			rx, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := txPowerRegex.FindStringSubmatch(line); m != nil && tx == 0 {
			tx, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := distanceRegex.FindStringSubmatch(line); m != nil && dist == 0 {
			dist, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := attenDistRegex.FindStringSubmatch(line); m != nil && dist == 0 {
			dist, _ = strconv.ParseFloat(m[1], 64)
		}
	}
	if rx == 0 && tx == 0 && dist == 0 {
		return nil
	}
	return &ONUOpticalSample{Rx: rx, Tx: tx, DistanceM: dist}
}

// looksLikeOLTPortPower menandai nilai redaman yang kemungkinan besar adalah
// OLT-side per-port (bukan per-ONU), misalnya pola seragam -3.40 / 6.39
// dengan jarak kosong.
func looksLikeOLTPortPower(rx, tx, distance float64) bool {
	if distance > 0 {
		return false
	}
	return tx >= 5.0 && tx <= 15.0 && rx > -8.0 && rx < 5.0
}

// SyncOpticalFromCLI memperbarui rx_power_dbm/tx_power_dbm/distance_m tiap
// ONU dari CLI. Operasi lama; wajib dipanggil di background.
func (service *Service) SyncOpticalFromCLI(ctx context.Context, tenantID, id string) (int, error) {
	onus, err := service.repository.ListONUs(ctx, tenantID, id)
	if err != nil {
		log.Printf("SyncOpticalFromCLI ListONUs error: %v", err)
		return 0, err
	}
	log.Printf("SyncOpticalFromCLI start: %d ONU", len(onus))
	// Kelompokkan ONU per PON agar bisa pakai command PON-wide.
	ponGroups := make(map[string][]zte.ONU)
	for _, onu := range onus {
		pon, _, ok := splitIndex(onu.ONUNumber)
		if !ok {
			pon, _, ok = splitIndex(onu.Index)
		}
		if !ok {
			continue
		}
		ponGroups[pon] = append(ponGroups[pon], onu)
	}

	// Mode cepat C320: lewati probe PON-wide optical-info karena pada perangkat
	// ini sering menghasilkan 20202 dan hanya menambah latency.
	// Fokus langsung ke batch attenuation per-ONU dalam satu sesi CLI per PON.
	ponSamples := make(map[string]map[int]*ONUOpticalSample)

	// Fast fallback: jika PON-wide tidak lengkap, tembak attenuation per-ONU
	// tetapi tetap dalam SATU sesi CLI per PON agar cepat.
	fastFallbackByPON := make(map[string]map[int]*ONUOpticalSample)
	for pon, group := range ponGroups {
		tbl := ponSamples[pon]
		ids := make([]int, 0, len(group))
		for _, onu := range group {
			_, oid, ok := splitIndex(onu.ONUNumber)
			if !ok {
				_, oid, ok = splitIndex(onu.Index)
			}
			if !ok {
				continue
			}
			s := (*ONUOpticalSample)(nil)
			if tbl != nil {
				s = tbl[oid]
			}
			if s == nil || s.Rx == 0 || s.Tx == 0 {
				ids = append(ids, oid)
			}
		}
		if len(ids) == 0 {
			continue
		}
		quick, qErr := service.probeAttenuationBatchForPON(ctx, tenantID, id, pon, ids)
		if qErr != nil {
			log.Printf("SyncOpticalFromCLI PON %s fast attenuation error: %v", pon, qErr)
		}
		if len(quick) > 0 {
			fastFallbackByPON[pon] = quick
			log.Printf("SyncOpticalFromCLI PON %s fast attenuation samples: %d", pon, len(quick))
		}
	}

	updated := 0
	for pon, group := range ponGroups {
		tbl, hasTable := ponSamples[pon]
		for _, onu := range group {
			var sample *ONUOpticalSample
			var serialPtr *string
			_, onuIDInt, ok := splitIndex(onu.ONUNumber)
			if !ok {
				_, onuIDInt, ok = splitIndex(onu.Index)
			}
			if !ok {
				continue
			}
			if hasTable {
				sample = tbl[onuIDInt]
			}
			if fastTbl, okFast := fastFallbackByPON[pon]; okFast {
				if fast := fastTbl[onuIDInt]; fast != nil {
					if sample == nil {
						sample = &ONUOpticalSample{}
					}
					mergeOpticalSample(sample, fast)
				}
			}
			// Untuk OLT besar (ratusan ONU), fallback per-ONU sangat mahal.
			// Jika Rx/Tx sudah ada dari PON-wide/batch attenuation, jangan
			// lanjut probe kandidat lain.
			needFallback := sample == nil || sample.Rx == 0 || sample.Tx == 0
			if needFallback {
				s, _, err := service.ProbeONUOpticalCLI(ctx, tenantID, id, pon, onuIDInt)
				if err != nil {
					if sample == nil {
						log.Printf("SyncOpticalFromCLI fallback ONU %s error: %v", onu.Index, err)
						continue
					}
				} else if s != nil {
					if sample == nil {
						sample = s
					} else {
						if sample.Rx == 0 {
							sample.Rx = s.Rx
						}
						if sample.Tx == 0 {
							sample.Tx = s.Tx
						}
						if sample.DistanceM == 0 {
							sample.DistanceM = s.DistanceM
						}
					}
				}
			}
			needDetail := sample == nil || sample.Rx == 0 || sample.Tx == 0
			if needDetail {
				_, oid, ok := splitIndex(onu.ONUNumber)
				if !ok {
					_, oid, ok = splitIndex(onu.Index)
				}
				if ok {
					detail, _, dErr := service.ProbeONUConfigCLI(ctx, tenantID, id, pon, oid)
					if dErr == nil && detail != nil {
						if sample == nil {
							sample = &ONUOpticalSample{}
						}
						sample.Status = normalizeONUStatus(detail.Status)
						if sample.Rx == 0 && detail.RxONUSideDBM != 0 {
							sample.Rx = detail.RxONUSideDBM
						}
						if sample.Tx == 0 && detail.TxONUSideDBM != 0 {
							sample.Tx = detail.TxONUSideDBM
						}
						if sample.DistanceM == 0 && detail.DistanceM > 0 {
							sample.DistanceM = detail.DistanceM
						}
						serial := strings.ToUpper(strings.TrimSpace(detail.SerialNumber))
						if serial != "" {
							serialPtr = &serial
						}
					}
				}

				// Persist serial update jika repository mendukungnya.
				if serialPtr != nil {
					type serialUpdater interface {
						UpdateONUSerialByRef(ctx context.Context, tenantID, oltID, pon string, onuID int, serial *string) error
					}
					if updater, ok := any(service.repository).(serialUpdater); ok {
						if sErr := updater.UpdateONUSerialByRef(ctx, tenantID, id, pon, onuIDInt, serialPtr); sErr != nil {
							log.Printf("SyncOpticalFromCLI UpdateONUSerialByRef %s:%d error: %v", pon, onuIDInt, sErr)
						}
					}
				}
			}
			if sample != nil && sample.DistanceM == 0 && onu.DistanceM > 0 {
				sample.DistanceM = onu.DistanceM
			}
			if sample != nil && looksLikeOLTPortPower(sample.Rx, sample.Tx, sample.DistanceM) {
				sample.Rx = 0
				sample.Tx = 0
			}

			// Status dari detail CLI lebih fresh daripada cache SNMP
			liveStatus := normalizeONUStatus(onu.Status)
			if sample != nil && sample.Status != "" {
				liveStatus = sample.Status
			}
			if !statusAllowsOptical(liveStatus) {
				if sample == nil {
					sample = &ONUOpticalSample{DistanceM: onu.DistanceM}
				}
				if sample.DistanceM == 0 && onu.DistanceM > 0 {
					sample.DistanceM = onu.DistanceM
				}
				sample.Rx = 0
				sample.Tx = 0
			}

			// Force update walau redaman 0 agar DB tetap konsisten dengan status.
			if sample == nil {
				sample = &ONUOpticalSample{DistanceM: onu.DistanceM}
			}
			if sample.DistanceM == 0 && onu.DistanceM > 0 {
				sample.DistanceM = onu.DistanceM
			}
			if uErr := service.repository.UpdateONUOptical(ctx, tenantID, id, onu.Index, sample.Rx, sample.Tx, sample.DistanceM); uErr == nil {
				updated++
			} else {
				log.Printf("SyncOpticalFromCLI UpdateONUOptical %s error: %v", onu.Index, uErr)
			}
		}
	}
	log.Printf("SyncOpticalFromCLI finished: updated %d/%d", updated, len(onus))
	return updated, nil
}

// splitIndex memecah index ONU menjadi PON "1/1/x" dan ONU ID.
// Mendukung dua format:
//  1. label: "1/1/5:7"
//  2. indeks SNMP: "285278469.7" (gponOnuIndex.onuId)
func splitIndex(index string) (string, int, bool) {
	parts := strings.Split(index, ":")
	if len(parts) == 2 {
		pon := parts[0]
		onuID, err := strconv.Atoi(parts[1])
		if err != nil || onuID < 1 {
			return "", 0, false
		}
		return pon, onuID, true
	}

	seg := strings.Split(index, ".")
	if len(seg) >= 2 {
		v, err := strconv.ParseInt(seg[0], 10, 64)
		if err != nil || v <= 0xFFFF {
			return "", 0, false
		}
		// Encoding 0x11|shelf|slot|pon: slot di bits 8-15 (bukan 16-23).
		shelf := int((v >> 16) & 0xFF)
		slot := int((v >> 8) & 0xFF)
		port := int(v & 0xFF)
		if port == 0 {
			slot = int((v >> 16) & 0xFF)
			port = int((v >> 8) & 0xFF)
		}
		onuID, err := strconv.Atoi(seg[len(seg)-1])
		if err != nil || onuID < 1 || shelf < 1 || slot < 1 || port < 1 {
			return "", 0, false
		}
		return fmt.Sprintf("%d/%d/%d", shelf, slot, port), onuID, true
	}
	return "", 0, false
}

// BuildONUIndex mengonversi PON "shelf/slot/port" dan onuID ke gponOnuIndex
// yang dipakai oleh tabel SNMP ZTE: (slot-1)*100000 + (port-1)*1000 + onuId.
func BuildONUIndex(pon string, onuID int) (int64, error) {
	pon = sanitizePON(pon)
	if pon == "" {
		return 0, fmt.Errorf("format PON tidak valid")
	}
	parts := strings.Split(pon, "/")
	if len(parts) != 3 {
		return 0, fmt.Errorf("format PON harus shelf/slot/port")
	}
	shelf, err1 := strconv.Atoi(parts[0])
	slot, err2 := strconv.Atoi(parts[1])
	port, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || shelf < 1 || slot < 1 || port < 1 || onuID < 1 {
		return 0, fmt.Errorf("shelf/slot/port/onuID tidak valid")
	}
	// Encoding ZTE standar (slot-1)*100000 + (port-1)*1000 + onuId.
	// Shelf tidak ikut encoding pada banyak firmware C320.
	_ = shelf
	idx := int64((slot-1)*100000 + (port-1)*1000 + onuID)
	return idx, nil
}

// StatusFromUint dipertahankan untuk kompatibilitas; delegasi ke sumber
// kebenaran tunggal zte.StatusFromCode (pemetaan lama di sini keliru:
// 4 sempat dipetakan ke "offline" sehingga ONU online salah jadi offline).
func StatusFromUint(v uint64) string {
	return zte.StatusFromCode(v)
}

type ONUTcontConfig struct {
	ID         int    `json:"id"`
	Name       string `json:"name,omitempty"`
	Config     string `json:"config,omitempty"`
	Profile    string `json:"profile,omitempty"`
	DBAGapMode string `json:"dba_gap_mode,omitempty"`
}

type ONUGemportConfig struct {
	ID                int    `json:"id"`
	Config            string `json:"config,omitempty"`
	UpstreamProfile   string `json:"upstream_profile,omitempty"`
	DownstreamProfile string `json:"downstream_profile,omitempty"`
}

type ONUServicePortConfig struct {
	ID          int    `json:"id"`
	Raw         string `json:"raw,omitempty"`
	VPort       int    `json:"vport,omitempty"`
	Description string `json:"description,omitempty"`
	Mode        string `json:"mode,omitempty"`
	UserVLAN    int    `json:"user_vlan,omitempty"`
	UserSVLAN   int    `json:"user_svlan,omitempty"`
	VLAN        int    `json:"vlan,omitempty"`
	CTagCOS     int    `json:"c_tag_cos,omitempty"`
	SVLAN       int    `json:"svlan,omitempty"`
	STagCOS     int    `json:"s_tag_cos,omitempty"`
	EtherType   string `json:"ether_type,omitempty"`
}

type ONUWANIPConfig struct {
	ID                int    `json:"id"`
	Raw               string `json:"raw,omitempty"`
	Mode              string `json:"mode,omitempty"`
	AuthMode          string `json:"auth_mode,omitempty"`
	VLANProfile       string `json:"vlan_profile,omitempty"`
	IPProfile         string `json:"ip_profile,omitempty"`
	StaticIP          string `json:"static_ip,omitempty"`
	PPPoEUsername     string `json:"pppoe_username,omitempty"`
	PPPoEPassword     string `json:"pppoe_password,omitempty"`
	RespondPing       *bool  `json:"respond_ping,omitempty"`
	RespondTraceroute *bool  `json:"respond_traceroute,omitempty"`
}

type ONUProvisionState struct {
	Status  string   `json:"status,omitempty"` // configured | partial | unconfigured
	Access  string   `json:"access,omitempty"` // pppoe | ipoe | bridge | unknown
	Missing []string `json:"missing,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

type ONUConfigDetail struct {
	PONPort            string                 `json:"pon_port"`
	ONUID              int                    `json:"onu_id"`
	Interface          string                 `json:"interface"`
	Status             string                 `json:"status,omitempty"`
	Name               string                 `json:"name,omitempty"`
	Description        string                 `json:"description,omitempty"`
	SerialNumber       string                 `json:"serial_number,omitempty"`
	ONUType            string                 `json:"onu_type,omitempty"`
	DistanceM          float64                `json:"distance_m,omitempty"`
	RxONUSideDBM       float64                `json:"rx_onu_side_dbm,omitempty"`
	TxONUSideDBM       float64                `json:"tx_onu_side_dbm,omitempty"`
	RxOLTSideDBM       float64                `json:"rx_olt_side_dbm,omitempty"`
	UpstreamBps        float64                `json:"upstream_bps,omitempty"`
	DownstreamBps      float64                `json:"downstream_bps,omitempty"`
	VLANs              []int                  `json:"vlans,omitempty"`
	DBAProfiles        []string               `json:"dba_profiles,omitempty"`
	UpstreamProfiles   []string               `json:"upstream_profiles,omitempty"`
	DownstreamProfiles []string               `json:"downstream_profiles,omitempty"`
	Tconts             []ONUTcontConfig       `json:"tconts,omitempty"`
	Gemports           []ONUGemportConfig     `json:"gemports,omitempty"`
	ServicePorts       []ONUServicePortConfig `json:"service_ports,omitempty"`
	WANIPs             []ONUWANIPConfig       `json:"wan_ips,omitempty"`
	ConfigState        ONUProvisionState      `json:"config_state,omitempty"`
	Warnings           []string               `json:"warnings,omitempty"`
}

var (
	onuNameRegex                = regexp.MustCompile(`(?im)^\s*name\s*:\s*(.+?)\s*$`)
	onuDescRegex                = regexp.MustCompile(`(?im)^\s*description\s*:\s*(.+?)\s*$`)
	onuSerialRegex              = regexp.MustCompile(`(?im)^\s*serial\s+number\s*:\s*(\S+)\s*$`)
	onuTypeRegex                = regexp.MustCompile(`(?im)^\s*onu\s+type\s+reported\s*:\s*(\S+)\s*$`)
	onuDistanceRegex            = regexp.MustCompile(`(?im)^\s*onu\s+distance\s*:\s*([0-9]+)\s*m?\s*$`)
	onuPhaseStateRegex          = regexp.MustCompile(`(?im)^\s*phase\s+state\s*:\s*([a-zA-Z_\-]+)\s*$`)
	runningTcontRegex           = regexp.MustCompile(`(?i)^tcont\s+(\d+)\s+(.+)$`)
	runningGemportRegex         = regexp.MustCompile(`(?i)^gemport\s+(\d+)\s+(.+)$`)
	runningServicePortRegex     = regexp.MustCompile(`(?i)^service-port\s+(\d+)\s+(.+)$`)
	runningWanIPRegex           = regexp.MustCompile(`(?i)\bwan-ip\s+(\d+)\s+(.+)$`)
	profileCaptureRegex         = regexp.MustCompile(`(?i)\bprofile\s+([A-Za-z0-9._\-]+)\b`)
	tcontNameCaptureRegex       = regexp.MustCompile(`(?i)\bname\s+([A-Za-z0-9._\-]+)\b`)
	upstreamProfileRegex        = regexp.MustCompile(`(?i)\bupstream\s+([A-Za-z0-9._\-]+)\b`)
	downstreamProfileRegex      = regexp.MustCompile(`(?i)\bdownstream\s+([A-Za-z0-9._\-]+)\b`)
	userVLANRegex               = regexp.MustCompile(`(?i)\buser-vlan\s+([0-9]+)\b`)
	userSVLANRegex              = regexp.MustCompile(`(?i)\buser-svlan\s+([0-9]+)\b`)
	vportRegex                  = regexp.MustCompile(`(?i)\bvport\s+([0-9]+)\b`)
	svlanRegex                  = regexp.MustCompile(`(?i)(?:\s|^)svlan\s+([0-9]+)\b`)
	vlanRegex                   = regexp.MustCompile(`(?i)(?:\s|^)vlan\s+([0-9]+)\b`)
	cTagCOSRegex                = regexp.MustCompile(`(?i)\b(?:cos|c-cos|c-tag\s+cos)\s+([0-9]+)\b`)
	sTagCOSRegex                = regexp.MustCompile(`(?i)\b(?:scos|s-cos|s-tag\s+cos)\s+([0-9]+)\b`)
	servicePortDescRegex        = regexp.MustCompile(`(?i)^description\s+(.+)$`)
	servicePortModeRegex        = regexp.MustCompile(`(?i)\b(?:mode|tag-mode)\s+([a-z_\-]+)\b`)
	servicePortEtypeRegex       = regexp.MustCompile(`(?i)\b(?:etype|user-etype)\s+([a-z0-9_\-]+)\b`)
	wanModeRegex                = regexp.MustCompile(`(?i)\bmode\s+([A-Za-z0-9_\-]+)\b`)
	wanAuthModeRegex            = regexp.MustCompile(`(?i)\bauth-mode\s+([A-Za-z0-9_\-]+)\b`)
	wanVLANProfileRegex         = regexp.MustCompile(`(?i)\bvlan-profile\s+([A-Za-z0-9._\-]+)\b`)
	wanIPProfileRegex           = regexp.MustCompile(`(?i)\bip-profile\s+([A-Za-z0-9._\-]+)\b`)
	wanStaticIPRegex            = regexp.MustCompile(`(?i)\b(?:static-ip|ip-address)\s+([0-9]{1,3}(?:\.[0-9]{1,3}){3}(?:/[0-9]{1,2})?)\b`)
	wanUsernameRegex            = regexp.MustCompile(`(?i)\busername\s+([^\s]+)\b`)
	wanPasswordRegex            = regexp.MustCompile(`(?i)\bpassword\s+([^\s]+)\b`)
	wanPingResponseRegex        = regexp.MustCompile(`(?i)\bping-response\s+(enable|disable)\b`)
	wanTracerouteResponseRegex  = regexp.MustCompile(`(?i)\btraceroute-response\s+(enable|disable)\b`)
	switchportVportModeRegex    = regexp.MustCompile(`(?i)^switchport\s+mode\s+([a-z_\-]+)(?:\s+vport\s+([0-9]+))?\s*$`)
	switchVlanModeRegex         = regexp.MustCompile(`(?i)^switch\s+vlan\s+([0-9]+)\s+(tag|untag(?:ged)?)(?:\s+vport\s+([0-9]+))?\s*$`)
	dbaGapModeRegex             = regexp.MustCompile(`(?i)\bdba(?:[- ]gap)?(?:[- ]mode)?\s+([A-Za-z][A-Za-z0-9 _-]+)\b`)
	tcontProfileLineRegex       = regexp.MustCompile(`(?im)^\s*profile\s+name\s*:\s*([A-Za-z0-9._\-]+)\s*$`)
	gemportDownProfileLineRegex = regexp.MustCompile(`(?im)^\s*down\s+traffic\s+profile\s+name\s*:\s*([A-Za-z0-9._\-]+)\s*$`)
	gemportUpProfileLineRegex   = regexp.MustCompile(`(?im)^\s*up\s+traffic\s+profile\s+name\s*:\s*([A-Za-z0-9._\-]+)\s*$`)
	ifaceInputRateRegex         = regexp.MustCompile(`(?i)input\s+rate\s*:\s*([0-9][0-9,._]*)\s*Bps`)
	ifaceOutputRateRegex        = regexp.MustCompile(`(?i)output\s+rate\s*:\s*([0-9][0-9,._]*)\s*Bps`)
)

// ProbeONUConfigCLI mengambil detail konfigurasi ONU: VLAN, DBA profile,
// upstream/downstream profile/rate dari command CLI yang terbukti di OLT V2.
func (service *Service) ProbeONUConfigCLI(ctx context.Context, tenantID, id, ponPort string, onuID int) (*ONUConfigDetail, map[string]string, error) {
	ponPort = sanitizePON(ponPort)
	if ponPort == "" || onuID < 1 || onuID > 128 {
		return nil, nil, ErrInvalidInput
	}

	detail := &ONUConfigDetail{
		PONPort:   ponPort,
		ONUID:     onuID,
		Interface: fmt.Sprintf("gpon-onu_%s:%d", ponPort, onuID),
	}
	raws := map[string]string{}
	warnings := []string{}
	firstErr := error(nil)

	_, err := service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
		runFirst := func(key string, candidates []string) (string, string, error) {
			var lastErr error
			for _, cmd := range candidates {
				out, execErr := session.Execute(ctx, cmd)
				if execErr != nil {
					if firstErr == nil {
						firstErr = execErr
					}
					lastErr = execErr
					continue
				}
				raws[cmd] = out
				if looksLikeInvalidCLI(out) {
					lastErr = fmt.Errorf("invalid output for %s", cmd)
					continue
				}
				return cmd, out, nil
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("tidak ada command %s yang berhasil", key)
			}
			return "", "", lastErr
		}

		if _, out, e := runFirst("detail", []string{
			fmt.Sprintf("show gpon onu detail-info gpon-onu_%s:%d", ponPort, onuID),
			fmt.Sprintf("show gpon onu detail-info gpon-olt_%s %d", ponPort, onuID),
		}); e == nil {
			parseDetailInfoCLI(detail, out)
		} else {
			warnings = append(warnings, "detail-info tidak tersedia")
		}

		if _, out, e := runFirst("running-config", []string{
			fmt.Sprintf("show running-config interface gpon-onu_%s:%d", ponPort, onuID),
		}); e == nil {
			parseRunningConfigCLI(detail, out)
		} else {
			warnings = append(warnings, "running-config interface tidak tersedia")
		}

		if _, out, e := runFirst("onu-running-config", []string{
			fmt.Sprintf("show onu running config gpon-onu_%s:%d", ponPort, onuID),
			fmt.Sprintf("show running-config pon-onu-mng gpon-onu_%s:%d", ponPort, onuID),
		}); e == nil {
			parseRunningConfigCLI(detail, out)
		}

		if _, out, e := runFirst("service-port-table", []string{
			fmt.Sprintf("show service-port interface gpon-onu_%s:%d", ponPort, onuID),
			fmt.Sprintf("show service-port interface gpon-olt_%s %d", ponPort, onuID),
		}); e == nil {
			parseServicePortTableCLI(detail, out)
		}

		if _, out, e := runFirst("tcont", []string{
			fmt.Sprintf("show gpon onu tcont gpon-olt_%s %d", ponPort, onuID),
			fmt.Sprintf("show gpon onu tcont gpon-onu_%s:%d", ponPort, onuID),
		}); e == nil {
			parseTcontProfilesCLI(detail, out)
		} else {
			warnings = append(warnings, "command tcont tidak tersedia")
		}

		if _, out, e := runFirst("gemport", []string{
			fmt.Sprintf("show gpon onu gemport gpon-olt_%s %d", ponPort, onuID),
			fmt.Sprintf("show gpon onu gemport gpon-onu_%s:%d", ponPort, onuID),
		}); e == nil {
			parseGemportProfilesCLI(detail, out)
		} else {
			warnings = append(warnings, "command gemport tidak tersedia")
		}

		if _, out, e := runFirst("interface-rate", []string{
			fmt.Sprintf("show interface gpon-onu_%s:%d", ponPort, onuID),
		}); e == nil {
			parseInterfaceRatesCLI(detail, out)
		} else {
			warnings = append(warnings, "rate interface tidak tersedia")
		}

		if _, out, e := runFirst("attenuation", []string{
			fmt.Sprintf("show pon power attenuation gpon-onu_%s:%d", ponPort, onuID),
		}); e == nil {
			parseAttenuationSidesCLI(detail, out)
		} else {
			warnings = append(warnings, "attenuation optical tidak tersedia")
		}

		return "", nil
	})
	if err != nil {
		return nil, raws, err
	}

	detail.VLANs = uniqueSortedInts(detail.VLANs)
	detail.DBAProfiles = uniqueSortedStrings(detail.DBAProfiles)
	detail.UpstreamProfiles = uniqueSortedStrings(detail.UpstreamProfiles)
	detail.DownstreamProfiles = uniqueSortedStrings(detail.DownstreamProfiles)
	detail.Warnings = uniqueSortedStrings(warnings)
	detail.ConfigState = deriveONUProvisionState(detail)

	if len(raws) == 0 {
		if firstErr != nil {
			return nil, raws, firstErr
		}
		return nil, raws, fmt.Errorf("tidak ada output CLI yang bisa dipakai")
	}
	return detail, raws, nil
}

func looksLikeInvalidCLI(out string) bool {
	low := strings.ToLower(out)
	return strings.Contains(low, "%error") || strings.Contains(low, "invalid input") || strings.Contains(low, "unrecognized command")
}

func parseDetailInfoCLI(detail *ONUConfigDetail, raw string) {
	if m := onuNameRegex.FindStringSubmatch(raw); len(m) == 2 {
		detail.Name = strings.TrimSpace(m[1])
	}
	if m := onuDescRegex.FindStringSubmatch(raw); len(m) == 2 {
		detail.Description = strings.TrimSpace(m[1])
	}
	if m := onuSerialRegex.FindStringSubmatch(raw); len(m) == 2 {
		detail.SerialNumber = strings.TrimSpace(m[1])
	}
	if m := onuTypeRegex.FindStringSubmatch(raw); len(m) == 2 {
		detail.ONUType = strings.TrimSpace(m[1])
	}
	if m := onuDistanceRegex.FindStringSubmatch(raw); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			detail.DistanceM = float64(n)
		}
	}
	if m := onuPhaseStateRegex.FindStringSubmatch(raw); len(m) == 2 {
		status := strings.ToLower(strings.TrimSpace(m[1]))
		switch {
		case strings.Contains(status, "work"):
			detail.Status = "working"
		case strings.Contains(status, "dying"):
			detail.Status = "dying_gasp"
		case strings.Contains(status, "los"):
			detail.Status = "los"
		default:
			detail.Status = status
		}
	}
}

func parseRunningConfigCLI(detail *ONUConfigDetail, raw string) {
	serviceByID := map[int]*ONUServicePortConfig{}
	serviceOrder := make([]int, 0, 16)
	wanByID := map[int]*ONUWANIPConfig{}
	wanOrder := make([]int, 0, 8)
	vportModeMap := map[int]string{}
	globalSwitchportMode := ""
	ensureServicePort := func(id int) *ONUServicePortConfig {
		if existing, ok := serviceByID[id]; ok {
			return existing
		}
		sp := &ONUServicePortConfig{ID: id}
		serviceByID[id] = sp
		serviceOrder = append(serviceOrder, id)
		return sp
	}
	ensureWanIP := func(id int) *ONUWANIPConfig {
		if existing, ok := wanByID[id]; ok {
			return existing
		}
		row := &ONUWANIPConfig{ID: id}
		wanByID[id] = row
		wanOrder = append(wanOrder, id)
		return row
	}
	cleanupValue := func(raw string) string {
		v := strings.TrimSpace(raw)
		v = strings.Trim(v, `"'`)
		return v
	}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "!") {
			continue
		}
		if m := switchportVportModeRegex.FindStringSubmatch(line); len(m) >= 2 {
			mode, ok := normalizeServicePortMode(m[1])
			if ok {
				modeLabel := displayServicePortMode(mode)
				if len(m) == 3 && strings.TrimSpace(m[2]) != "" {
					if vp, err := strconv.Atoi(strings.TrimSpace(m[2])); err == nil && vp > 0 {
						vportModeMap[vp] = modeLabel
					}
				} else {
					globalSwitchportMode = modeLabel
				}
			}
			continue
		}
		if m := switchVlanModeRegex.FindStringSubmatch(line); len(m) >= 3 {
			mode := "Tagged"
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(m[2])), "untag") {
				mode = "Untagged"
			}
			if len(m) >= 4 && strings.TrimSpace(m[3]) != "" {
				if vp, err := strconv.Atoi(strings.TrimSpace(m[3])); err == nil && vp > 0 {
					vportModeMap[vp] = mode
				}
			}
			continue
		}
		if m := runningTcontRegex.FindStringSubmatch(line); len(m) == 3 {
			id, _ := strconv.Atoi(m[1])
			cfg := strings.TrimSpace(m[2])
			entry := ONUTcontConfig{ID: id, Config: cfg}
			if nm := tcontNameCaptureRegex.FindStringSubmatch(cfg); len(nm) == 2 {
				entry.Name = strings.TrimSpace(nm[1])
			}
			if p := profileCaptureRegex.FindStringSubmatch(cfg); len(p) == 2 {
				entry.Profile = strings.TrimSpace(p[1])
				detail.DBAProfiles = append(detail.DBAProfiles, entry.Profile)
			}
			if gm := dbaGapModeRegex.FindStringSubmatch(cfg); len(gm) == 2 {
				entry.DBAGapMode = strings.TrimSpace(gm[1])
			}
			detail.Tconts = append(detail.Tconts, entry)
			continue
		}
		if m := runningGemportRegex.FindStringSubmatch(line); len(m) == 3 {
			id, _ := strconv.Atoi(m[1])
			cfg := strings.TrimSpace(m[2])
			entry := ONUGemportConfig{ID: id, Config: cfg}
			if p := upstreamProfileRegex.FindStringSubmatch(cfg); len(p) == 2 {
				entry.UpstreamProfile = strings.TrimSpace(p[1])
				detail.UpstreamProfiles = append(detail.UpstreamProfiles, entry.UpstreamProfile)
			}
			if p := downstreamProfileRegex.FindStringSubmatch(cfg); len(p) == 2 {
				entry.DownstreamProfile = strings.TrimSpace(p[1])
				detail.DownstreamProfiles = append(detail.DownstreamProfiles, entry.DownstreamProfile)
			}
			detail.Gemports = append(detail.Gemports, entry)
			continue
		}
		if m := runningWanIPRegex.FindStringSubmatch(line); len(m) == 3 {
			id, _ := strconv.Atoi(m[1])
			if id < 1 {
				continue
			}
			cfg := strings.TrimSpace(m[2])
			row := ensureWanIP(id)
			if cfg != "" {
				if row.Raw == "" {
					row.Raw = cfg
				} else if !strings.Contains(strings.ToLower(row.Raw), strings.ToLower(cfg)) {
					row.Raw += " | " + cfg
				}
			}
			if mm := wanModeRegex.FindStringSubmatch(cfg); len(mm) == 2 {
				if mode, ok := normalizeWANMode(mm[1]); ok {
					row.Mode = displayWANMode(mode)
				} else {
					row.Mode = strings.ToUpper(strings.TrimSpace(mm[1]))
				}
			}
			if am := wanAuthModeRegex.FindStringSubmatch(cfg); len(am) == 2 {
				if auth, ok := normalizeWANAuthMode(am[1]); ok {
					row.AuthMode = displayWANAuthMode(auth)
				} else {
					row.AuthMode = strings.ToUpper(strings.TrimSpace(am[1]))
				}
			}
			if vm := wanVLANProfileRegex.FindStringSubmatch(cfg); len(vm) == 2 {
				row.VLANProfile = cleanupValue(vm[1])
			}
			if im := wanIPProfileRegex.FindStringSubmatch(cfg); len(im) == 2 {
				row.IPProfile = cleanupValue(im[1])
			}
			if sm := wanStaticIPRegex.FindStringSubmatch(cfg); len(sm) == 2 {
				row.StaticIP = cleanupValue(sm[1])
			}
			if um := wanUsernameRegex.FindStringSubmatch(cfg); len(um) == 2 {
				row.PPPoEUsername = cleanupValue(um[1])
			}
			if pm := wanPasswordRegex.FindStringSubmatch(cfg); len(pm) == 2 {
				row.PPPoEPassword = cleanupValue(pm[1])
			}
			if rm := wanPingResponseRegex.FindStringSubmatch(cfg); len(rm) == 2 {
				enabled := strings.EqualFold(strings.TrimSpace(rm[1]), "enable")
				row.RespondPing = &enabled
			}
			if rm := wanTracerouteResponseRegex.FindStringSubmatch(cfg); len(rm) == 2 {
				enabled := strings.EqualFold(strings.TrimSpace(rm[1]), "enable")
				row.RespondTraceroute = &enabled
			}
			continue
		}
		if m := runningServicePortRegex.FindStringSubmatch(line); len(m) == 3 {
			id, _ := strconv.Atoi(m[1])
			if id < 1 {
				continue
			}
			rawCfg := strings.TrimSpace(m[2])
			sp := ensureServicePort(id)
			if rawCfg != "" {
				if sp.Raw == "" {
					sp.Raw = rawCfg
				} else if !strings.Contains(strings.ToLower(sp.Raw), strings.ToLower(rawCfg)) {
					sp.Raw += " | " + rawCfg
				}
			}

			if dm := servicePortDescRegex.FindStringSubmatch(rawCfg); len(dm) == 2 {
				sp.Description = sanitizeCLIText(dm[1], 120)
			}

			if n := parseServicePortInt(vportRegex, rawCfg); n > 0 {
				sp.VPort = n
			}
			if n := parseServicePortInt(userVLANRegex, rawCfg); n > 0 {
				sp.UserVLAN = n
				detail.VLANs = append(detail.VLANs, n)
			}
			if n := parseServicePortInt(userSVLANRegex, rawCfg); n > 0 {
				sp.UserSVLAN = n
				detail.VLANs = append(detail.VLANs, n)
			}
			if n := parseServicePortInt(vlanRegex, rawCfg); n > 0 {
				sp.VLAN = n
				detail.VLANs = append(detail.VLANs, n)
			}
			if n := parseServicePortInt(svlanRegex, rawCfg); n > 0 {
				sp.SVLAN = n
				detail.VLANs = append(detail.VLANs, n)
			}
			if n := parseServicePortInt(cTagCOSRegex, rawCfg); n > 0 || strings.Contains(strings.ToLower(rawCfg), " cos 0") {
				sp.CTagCOS = n
			}
			if n := parseServicePortInt(sTagCOSRegex, rawCfg); n > 0 || strings.Contains(strings.ToLower(rawCfg), " scos 0") {
				sp.STagCOS = n
			}

			if mm := servicePortModeRegex.FindStringSubmatch(rawCfg); len(mm) == 2 {
				if mode, ok := normalizeServicePortMode(mm[1]); ok {
					sp.Mode = displayServicePortMode(mode)
				}
			}
			if sp.Mode == "" {
				if sp.VPort > 0 {
					if modeLabel, ok := vportModeMap[sp.VPort]; ok {
						sp.Mode = modeLabel
					}
				}
				if sp.Mode == "" && globalSwitchportMode != "" {
					sp.Mode = globalSwitchportMode
				}
			}
			if sp.Mode == "" {
				lowRaw := strings.ToLower(rawCfg)
				switch {
				case strings.Contains(lowRaw, "untag"):
					sp.Mode = "Untagged"
				case strings.Contains(lowRaw, "hybrid"):
					sp.Mode = "Hybrid"
				case sp.SVLAN > 0 && sp.VLAN > 0 && sp.SVLAN != sp.VLAN:
					sp.Mode = "Double VLAN"
				default:
					sp.Mode = "Tagged"
				}
			}

			if em := servicePortEtypeRegex.FindStringSubmatch(rawCfg); len(em) == 2 {
				if etype, ok := normalizeServiceEtherType(em[1]); ok {
					sp.EtherType = displayEtherType(etype)
				} else {
					sp.EtherType = strings.ToUpper(strings.TrimSpace(em[1]))
				}
			} else {
				lowRaw := strings.ToLower(rawCfg)
				switch {
				case strings.Contains(lowRaw, "pppoe"):
					sp.EtherType = "PPPoE"
				case strings.Contains(lowRaw, "ipoe"):
					sp.EtherType = "IPoE"
				}
			}
			if sp.EtherType == "" {
				sp.EtherType = "All Ether Type"
			}
		}
	}

	for _, id := range serviceOrder {
		sp := serviceByID[id]
		if sp == nil {
			continue
		}
		if sp.Mode == "" {
			sp.Mode = "Tagged"
		}
		if sp.EtherType == "" {
			sp.EtherType = "All Ether Type"
		}
		detail.ServicePorts = append(detail.ServicePorts, *sp)
	}
	for _, id := range wanOrder {
		row := wanByID[id]
		if row == nil {
			continue
		}
		if row.Mode == "" {
			row.Mode = "-"
		}
		if row.AuthMode == "" {
			row.AuthMode = "Auto"
		}
		detail.WANIPs = append(detail.WANIPs, *row)
	}
}

func parseServicePortTableCLI(detail *ONUConfigDetail, raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	byID := map[int]*ONUServicePortConfig{}
	for i := range detail.ServicePorts {
		sp := &detail.ServicePorts[i]
		byID[sp.ID] = sp
	}
	ensure := func(id int) *ONUServicePortConfig {
		if existing, ok := byID[id]; ok {
			return existing
		}
		detail.ServicePorts = append(detail.ServicePorts, ONUServicePortConfig{ID: id})
		sp := &detail.ServicePorts[len(detail.ServicePorts)-1]
		byID[id] = sp
		return sp
	}
	parseIntToken := func(token string) int {
		t := strings.TrimSpace(strings.Trim(token, ",;"))
		t = strings.Trim(t, "[]()")
		if t == "" || t == "--" || t == "-" {
			return 0
		}
		if strings.Contains(t, "/") {
			t = strings.SplitN(t, "/", 2)[0]
		}
		n, _ := strconv.Atoi(t)
		return n
	}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "sport") && strings.Contains(low, "vport") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		id := parseIntToken(fields[0])
		vp := parseIntToken(fields[1])
		if id < 1 || vp < 0 {
			continue
		}
		sp := ensure(id)
		if vp > 0 {
			sp.VPort = vp
		}
		for _, f := range fields {
			fl := strings.ToLower(strings.TrimSpace(f))
			switch fl {
			case "pppoe":
				sp.EtherType = "PPPoE"
			case "ipoe":
				sp.EtherType = "IPoE"
			case "all", "all-ether-type", "all_ether_type":
				sp.EtherType = "All Ether Type"
			case "tagged", "tag":
				sp.Mode = "Tagged"
			case "untagged", "untag":
				sp.Mode = "Untagged"
			case "hybrid":
				sp.Mode = "Hybrid"
			}
		}
		if sp.Mode == "" && strings.Contains(low, "double") {
			sp.Mode = "Double VLAN"
		}
		if sp.EtherType == "" {
			sp.EtherType = "All Ether Type"
		}
	}
}

func parseTcontProfilesCLI(detail *ONUConfigDetail, raw string) {
	for _, m := range tcontProfileLineRegex.FindAllStringSubmatch(raw, -1) {
		if len(m) == 2 {
			detail.DBAProfiles = append(detail.DBAProfiles, strings.TrimSpace(m[1]))
		}
	}
}

func parseGemportProfilesCLI(detail *ONUConfigDetail, raw string) {
	for _, m := range gemportDownProfileLineRegex.FindAllStringSubmatch(raw, -1) {
		if len(m) == 2 {
			detail.DownstreamProfiles = append(detail.DownstreamProfiles, strings.TrimSpace(m[1]))
		}
	}
	for _, m := range gemportUpProfileLineRegex.FindAllStringSubmatch(raw, -1) {
		if len(m) == 2 {
			detail.UpstreamProfiles = append(detail.UpstreamProfiles, strings.TrimSpace(m[1]))
		}
	}
}

func parseInterfaceRatesCLI(detail *ONUConfigDetail, raw string) {
	flat := strings.ReplaceAll(raw, "\r", " ")
	if m := ifaceInputRateRegex.FindStringSubmatch(flat); len(m) == 2 {
		detail.UpstreamBps = float64(parseTrafficBytes(m[1]))
	}
	if m := ifaceOutputRateRegex.FindStringSubmatch(flat); len(m) == 2 {
		detail.DownstreamBps = float64(parseTrafficBytes(m[1]))
	}
}

// parseAttenuationSidesCLI membaca output `show pon power attenuation`:
// - up Rx   = Rx OLT side
// - down Rx = Rx ONU side
func parseAttenuationSidesCLI(detail *ONUConfigDetail, raw string) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" {
			continue
		}
		if m := attenUpPair.FindStringSubmatch(line); len(m) == 3 {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil && v != 0 {
				detail.RxOLTSideDBM = v
			}
			if v, err := strconv.ParseFloat(m[2], 64); err == nil && v != 0 {
				detail.TxONUSideDBM = v
			}
		} else if m := attenUpPairAlt.FindStringSubmatch(line); len(m) == 3 {
			if v, err := strconv.ParseFloat(m[2], 64); err == nil && v != 0 {
				detail.RxOLTSideDBM = v
			}
			if v, err := strconv.ParseFloat(m[1], 64); err == nil && v != 0 {
				detail.TxONUSideDBM = v
			}
		}
		if m := attenDownPair.FindStringSubmatch(line); len(m) == 3 {
			if v, err := strconv.ParseFloat(m[2], 64); err == nil && v != 0 {
				detail.RxONUSideDBM = v
			}
		} else if m := attenDownAlt.FindStringSubmatch(line); len(m) == 3 {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil && v != 0 {
				detail.RxONUSideDBM = v
			}
		}
	}
}

func uniqueSortedInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(values))
	out := make([]int, 0, len(values))
	for _, v := range values {
		if v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func deriveONUProvisionState(detail *ONUConfigDetail) ONUProvisionState {
	state := ONUProvisionState{Status: "unconfigured", Access: "unknown"}
	if detail == nil {
		state.Reason = "detail ONU belum tersedia"
		return state
	}

	hasTcont := len(detail.Tconts) > 0
	hasGemport := len(detail.Gemports) > 0
	hasServicePort := len(detail.ServicePorts) > 0
	hasWAN := len(detail.WANIPs) > 0

	access := "unknown"
	if hasWAN {
		for _, row := range detail.WANIPs {
			mode, ok := normalizeWANMode(row.Mode)
			if !ok {
				continue
			}
			if mode == "pppoe" {
				access = "pppoe"
				break
			}
			if mode == "ipoe" || mode == "static" {
				access = "ipoe"
			}
		}
		if access == "unknown" {
			access = "ipoe"
		}
	} else if hasServicePort {
		access = "bridge"
	}
	state.Access = access

	missing := make([]string, 0, 4)
	if !hasTcont {
		missing = append(missing, "tcont")
	}
	if !hasGemport {
		missing = append(missing, "gemport")
	}
	if !hasServicePort {
		missing = append(missing, "service-port")
	}
	if access != "bridge" && !hasWAN {
		missing = append(missing, "wan-ip")
	}
	state.Missing = missing

	switch {
	case !hasServicePort && !hasWAN:
		state.Status = "unconfigured"
		state.Reason = "service-port dan wan-ip belum ada"
	case len(missing) > 0:
		state.Status = "partial"
		state.Reason = "konfigurasi ONU belum lengkap"
	default:
		state.Status = "configured"
		if access == "bridge" {
			state.Reason = "ONU mode bridge terdeteksi"
		} else {
			state.Reason = "konfigurasi ONU lengkap"
		}
	}

	return state
}

type ONUConfigApplyInput struct {
	PON                  string `json:"pon"`
	ONUID                int    `json:"onu_id"`
	Operation            string `json:"operation"`
	Name                 string `json:"name,omitempty"`
	Description          string `json:"description,omitempty"`
	TcontID              int    `json:"tcont_id,omitempty"`
	TcontName            string `json:"tcont_name,omitempty"`
	TcontProfile         string `json:"tcont_profile,omitempty"`
	GemportID            int    `json:"gemport_id,omitempty"`
	UpstreamProfile      string `json:"upstream_profile,omitempty"`
	DownstreamProfile    string `json:"downstream_profile,omitempty"`
	ServicePortID        int    `json:"service_port_id,omitempty"`
	VPort                int    `json:"vport,omitempty"`
	UserVLAN             int    `json:"user_vlan,omitempty"`
	UserSVLAN            int    `json:"user_svlan,omitempty"`
	VLAN                 int    `json:"vlan,omitempty"`
	CVID                 int    `json:"c_vid,omitempty"`
	SVLAN                int    `json:"svlan,omitempty"`
	SVID                 int    `json:"s_vid,omitempty"`
	CTagCOS              int    `json:"c_tag_cos,omitempty"`
	STagCOS              int    `json:"s_tag_cos,omitempty"`
	ServicePortMode      string `json:"service_port_mode,omitempty"`
	ServiceDescription   string `json:"service_description,omitempty"`
	WANIPID              int    `json:"wan_ip_id,omitempty"`
	WANMode              string `json:"wan_mode,omitempty"`
	WANAuthMode          string `json:"wan_auth_mode,omitempty"`
	WANVLANProfile       string `json:"wan_vlan_profile,omitempty"`
	WANIPProfile         string `json:"wan_ip_profile,omitempty"`
	WANStaticIP          string `json:"wan_static_ip,omitempty"`
	WANPPPoEUsername     string `json:"wan_pppoe_username,omitempty"`
	WANPPPoEPassword     string `json:"wan_pppoe_password,omitempty"`
	WANRespondPing       *bool  `json:"wan_respond_ping,omitempty"`
	WANRespondTraceroute *bool  `json:"wan_respond_traceroute,omitempty"`
	EtherType            string `json:"ether_type,omitempty"`
	ApplyVia             string `json:"apply_via,omitempty"`
}

type ONUConfigApplyResult struct {
	Operation string   `json:"operation"`
	Method    string   `json:"method"`
	Executed  []string `json:"executed"`
	Message   string   `json:"message,omitempty"`
}

func validProfileToken(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 64 {
		return false
	}
	for _, ch := range raw {
		ok := ch == '-' || ch == '_' || ch == '.' ||
			(ch >= '0' && ch <= '9') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= 'a' && ch <= 'z')
		if !ok {
			return false
		}
	}
	return true
}

func sanitizeCLIText(raw string, maxLen int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	clean := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ';' {
			return -1
		}
		return r
	}, raw)
	if maxLen > 0 && len(clean) > maxLen {
		clean = clean[:maxLen]
	}
	return strings.TrimSpace(clean)
}

func normalizeServicePortMode(raw string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "", "tagged", "tag":
		return "tagged", true
	case "untagged", "untag":
		return "untagged", true
	case "double_vlan", "double-vlan", "double vlan", "double":
		return "double_vlan", true
	case "hybrid":
		return "hybrid", true
	default:
		return "", false
	}
}

func normalizeServiceEtherType(raw string) (string, bool) {
	etype := strings.ToLower(strings.TrimSpace(raw))
	switch etype {
	case "", "all", "all_ether_type", "all-ether-type", "all ether type":
		return "all", true
	case "pppoe":
		return "pppoe", true
	case "ipoe":
		return "ipoe", true
	default:
		return "", false
	}
}

func normalizeWANMode(raw string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "pppoe":
		return "pppoe", true
	case "ipoe", "dhcp", "dynamic":
		return "ipoe", true
	case "static", "static-ip", "static_ip":
		return "static", true
	default:
		return "", false
	}
}

func normalizeWANAuthMode(raw string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "", "auto":
		return "auto", true
	case "pap":
		return "pap", true
	case "chap":
		return "chap", true
	default:
		return "", false
	}
}

func displayWANAuthMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto":
		return "Auto"
	case "pap":
		return "PAP"
	case "chap":
		return "CHAP"
	default:
		return strings.TrimSpace(mode)
	}
}

func displayWANMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "pppoe":
		return "PPPoE"
	case "ipoe", "dhcp", "dynamic":
		return "IPoE"
	case "static":
		return "Static"
	default:
		return strings.TrimSpace(mode)
	}
}

func sanitizeWANToken(raw string, maxLen int) string {
	clean := sanitizeCLIText(raw, maxLen)
	if clean == "" {
		return ""
	}
	if strings.ContainsAny(clean, "	\n\r\"'`") {
		return ""
	}
	return clean
}

func displayServicePortMode(mode string) string {
	switch mode {
	case "tagged":
		return "Tagged"
	case "untagged":
		return "Untagged"
	case "double_vlan":
		return "Double VLAN"
	case "hybrid":
		return "Hybrid"
	default:
		return ""
	}
}

func displayEtherType(etype string) string {
	switch etype {
	case "all":
		return "All Ether Type"
	case "pppoe":
		return "PPPoE"
	case "ipoe":
		return "IPoE"
	default:
		return ""
	}
}

func parseServicePortInt(regex *regexp.Regexp, raw string) int {
	if m := regex.FindStringSubmatch(raw); len(m) == 2 {
		if n, err := strconv.Atoi(strings.TrimSpace(m[1])); err == nil {
			return n
		}
	}
	return 0
}

// ApplyONUConfigCLI menjalankan perubahan konfigurasi ONU (name/description/
// tcont/gemport/service-port) secara aman via CLI dengan fallback mode command.
func (service *Service) ApplyONUConfigCLI(ctx context.Context, tenantID, id string, in ONUConfigApplyInput) (*ONUConfigApplyResult, error) {
	ponPort := sanitizePON(in.PON)
	if ponPort == "" || in.ONUID < 1 || in.ONUID > 128 {
		return nil, ErrInvalidInput
	}
	op := strings.ToLower(strings.TrimSpace(in.Operation))
	if op == "" {
		return nil, ErrInvalidInput
	}
	if op == "auto_config_onu" {
		return service.applyONUAutoConfigCLI(ctx, tenantID, id, ponPort, in)
	}

	var cmdCandidates []string
	postCommandAlternatives := make([][]string, 0, 4)
	message := ""
	serviceMode := ""
	serviceEtherType := ""
	serviceDescription := ""
	switch op {
	case "set_name":
		name := sanitizeCLIText(in.Name, 96)
		if name == "" {
			return nil, ErrInvalidInput
		}
		cmdCandidates = []string{"name " + name}
		message = "nama ONU diperbarui"
	case "set_description":
		desc := sanitizeCLIText(in.Description, 128)
		if desc == "" {
			return nil, ErrInvalidInput
		}
		cmdCandidates = []string{"description " + desc}
		message = "deskripsi ONU diperbarui"
	case "set_tcont":
		if in.TcontID < 1 || in.TcontID > 32 || !validProfileToken(in.TcontProfile) {
			return nil, ErrInvalidInput
		}
		profile := strings.TrimSpace(in.TcontProfile)
		tcontName := strings.TrimSpace(in.TcontName)
		if tcontName != "" {
			if !validProfileToken(tcontName) {
				return nil, ErrInvalidInput
			}
			cmdCandidates = append(cmdCandidates, fmt.Sprintf("tcont %d name %s profile %s", in.TcontID, tcontName, profile))
		}
		cmdCandidates = append(cmdCandidates, fmt.Sprintf("tcont %d profile %s", in.TcontID, profile))
		message = "tcont diperbarui"
	case "set_gemport":
		if in.GemportID < 1 || in.GemportID > 4095 {
			return nil, ErrInvalidInput
		}
		up := strings.TrimSpace(in.UpstreamProfile)
		down := strings.TrimSpace(in.DownstreamProfile)
		if up == "" && down == "" {
			return nil, ErrInvalidInput
		}
		if up != "" && !validProfileToken(up) {
			return nil, ErrInvalidInput
		}
		if down != "" && !validProfileToken(down) {
			return nil, ErrInvalidInput
		}
		switch {
		case up != "" && down != "":
			cmdCandidates = []string{fmt.Sprintf("gemport %d traffic-limit upstream %s downstream %s", in.GemportID, up, down)}
		case up != "":
			cmdCandidates = []string{fmt.Sprintf("gemport %d traffic-limit upstream %s", in.GemportID, up)}
		default:
			cmdCandidates = []string{fmt.Sprintf("gemport %d traffic-limit downstream %s", in.GemportID, down)}
		}
		message = "gemport diperbarui"
	case "set_service_port":
		if in.VLAN == 0 && in.CVID > 0 {
			in.VLAN = in.CVID
		}
		if in.SVLAN == 0 && in.SVID > 0 {
			in.SVLAN = in.SVID
		}
		if in.ServicePortID < 1 || in.ServicePortID > 4095 {
			return nil, fmt.Errorf("%w: service_port_id harus 1-4095", ErrInvalidInput)
		}
		if in.VPort < 1 || in.VPort > 32 {
			return nil, fmt.Errorf("%w: vport harus 1-32", ErrInvalidInput)
		}
		if in.UserVLAN < 1 || in.UserVLAN > 4094 {
			return nil, fmt.Errorf("%w: user_vlan harus 1-4094", ErrInvalidInput)
		}
		if in.UserSVLAN < 0 || in.UserSVLAN > 4094 {
			return nil, fmt.Errorf("%w: user_svlan harus 1-4094", ErrInvalidInput)
		}
		if in.VLAN < 1 || in.VLAN > 4094 {
			return nil, fmt.Errorf("%w: vlan/c_vid harus 1-4094", ErrInvalidInput)
		}
		sVLAN := in.SVLAN
		if sVLAN == 0 {
			sVLAN = in.VLAN
		}
		if sVLAN < 1 || sVLAN > 4094 {
			return nil, fmt.Errorf("%w: svlan/s_vid harus 1-4094", ErrInvalidInput)
		}
		if in.CTagCOS < 0 || in.CTagCOS > 7 {
			return nil, fmt.Errorf("%w: c_tag_cos harus 0-7", ErrInvalidInput)
		}
		if in.STagCOS < 0 || in.STagCOS > 7 {
			return nil, fmt.Errorf("%w: s_tag_cos harus 0-7", ErrInvalidInput)
		}
		mode, okMode := normalizeServicePortMode(in.ServicePortMode)
		if !okMode {
			return nil, fmt.Errorf("%w: service_port_mode tidak valid", ErrInvalidInput)
		}
		etype, okEtype := normalizeServiceEtherType(in.EtherType)
		if !okEtype {
			return nil, fmt.Errorf("%w: ether_type harus all/pppoe/ipoe", ErrInvalidInput)
		}
		if mode == "double_vlan" && in.UserSVLAN == 0 && in.SVLAN == 0 {
			return nil, fmt.Errorf("%w: mode double_vlan butuh User S-VID atau S-VID", ErrInvalidInput)
		}
		serviceMode = mode
		serviceEtherType = etype
		serviceDescription = sanitizeCLIText(in.ServiceDescription, 96)

		hasUserSVLAN := in.UserSVLAN > 0
		hasCTagCOS := in.CTagCOS > 0
		hasSTagCOS := in.STagCOS > 0
		hasAnyCos := hasCTagCOS || hasSTagCOS

		buildServicePortCmd := func(includeVPort bool, includeSVLAN bool, etypeToken string, useUserEtype bool) string {
			parts := []string{"service-port", strconv.Itoa(in.ServicePortID)}
			if includeVPort {
				parts = append(parts, "vport", strconv.Itoa(in.VPort))
			}
			parts = append(parts, "user-vlan", strconv.Itoa(in.UserVLAN))
			if hasUserSVLAN {
				parts = append(parts, "user-svlan", strconv.Itoa(in.UserSVLAN))
			}
			parts = append(parts, "vlan", strconv.Itoa(in.VLAN))
			if includeSVLAN {
				parts = append(parts, "svlan", strconv.Itoa(sVLAN))
			}
			if hasAnyCos {
				if hasCTagCOS {
					parts = append(parts, "cos", strconv.Itoa(in.CTagCOS))
				}
				if hasSTagCOS {
					parts = append(parts, "scos", strconv.Itoa(in.STagCOS))
				}
			}
			if etypeToken != "" {
				if useUserEtype {
					parts = append(parts, "user-etype", etypeToken)
				} else {
					parts = append(parts, "etype", etypeToken)
				}
			}
			return strings.Join(parts, " ")
		}

		appendCandidate := func(list *[]string, cmd string) {
			cmd = strings.TrimSpace(cmd)
			if cmd == "" {
				return
			}
			for _, existing := range *list {
				if strings.EqualFold(existing, cmd) {
					return
				}
			}
			*list = append(*list, cmd)
		}

		if etype != "all" {
			etypeToken := strings.ToUpper(etype)
			appendCandidate(&cmdCandidates, buildServicePortCmd(true, true, etypeToken, true))
			appendCandidate(&cmdCandidates, buildServicePortCmd(true, true, etypeToken, false))
			if mode != "double_vlan" {
				appendCandidate(&cmdCandidates, buildServicePortCmd(true, false, etypeToken, true))
				appendCandidate(&cmdCandidates, buildServicePortCmd(true, false, etypeToken, false))
			}
			appendCandidate(&cmdCandidates, buildServicePortCmd(false, true, etypeToken, true))
		} else {
			appendCandidate(&cmdCandidates, buildServicePortCmd(true, true, "", false))
			if mode != "double_vlan" {
				appendCandidate(&cmdCandidates, buildServicePortCmd(true, false, "", false))
			}
			appendCandidate(&cmdCandidates, buildServicePortCmd(false, false, "", false))
		}
		if mode == "untagged" {
			expanded := make([]string, 0, len(cmdCandidates)*3)
			for _, c := range cmdCandidates {
				appendCandidate(&expanded, c+" untagged")
				appendCandidate(&expanded, c+" untag")
				appendCandidate(&expanded, c)
			}
			cmdCandidates = expanded
		}

		if mode == "hybrid" {
			postCommandAlternatives = append(postCommandAlternatives, []string{
				fmt.Sprintf("switchport mode hybrid vport %d", in.VPort),
				"switchport mode hybrid",
			})
		}
		if mode == "untagged" {
			postCommandAlternatives = append(postCommandAlternatives, []string{
				fmt.Sprintf("switch vlan %d untag vport %d", in.UserVLAN, in.VPort),
				fmt.Sprintf("switch vlan %d untag", in.UserVLAN),
			})
		}
		if serviceDescription != "" {
			postCommandAlternatives = append(postCommandAlternatives, []string{
				fmt.Sprintf("service-port %d description %s", in.ServicePortID, serviceDescription),
				fmt.Sprintf("service-port %d desc %s", in.ServicePortID, serviceDescription),
			})
		}
		message = "service-port diperbarui"
	case "set_service_port_description":
		if in.ServicePortID < 1 || in.ServicePortID > 4095 {
			return nil, fmt.Errorf("%w: service_port_id harus 1-4095", ErrInvalidInput)
		}
		serviceDescription = sanitizeCLIText(in.ServiceDescription, 96)
		if serviceDescription == "" {
			return nil, fmt.Errorf("%w: service_description wajib diisi", ErrInvalidInput)
		}
		cmdCandidates = []string{
			fmt.Sprintf("service-port %d description %s", in.ServicePortID, serviceDescription),
			fmt.Sprintf("service-port %d desc %s", in.ServicePortID, serviceDescription),
		}
		message = "deskripsi service-port diperbarui"
	case "set_wan_ip":
		if in.WANIPID < 1 || in.WANIPID > 8 {
			return nil, fmt.Errorf("%w: wan_ip_id harus 1-8", ErrInvalidInput)
		}
		wanMode, ok := normalizeWANMode(in.WANMode)
		if !ok {
			return nil, fmt.Errorf("%w: wan_mode harus pppoe/ipoe/static", ErrInvalidInput)
		}
		wanAuthMode, okAuth := normalizeWANAuthMode(in.WANAuthMode)
		if !okAuth {
			return nil, fmt.Errorf("%w: wan_auth_mode harus auto/pap/chap", ErrInvalidInput)
		}
		wanVlanProfile := sanitizeWANToken(in.WANVLANProfile, 64)
		if wanVlanProfile == "" {
			return nil, fmt.Errorf("%w: wan_vlan_profile wajib diisi", ErrInvalidInput)
		}
		wanIPProfile := sanitizeWANToken(in.WANIPProfile, 64)
		wanStaticIP := sanitizeWANToken(in.WANStaticIP, 64)
		wanUsername := sanitizeWANToken(in.WANPPPoEUsername, 96)
		wanPassword := sanitizeWANToken(in.WANPPPoEPassword, 96)
		if wanMode == "pppoe" && (wanUsername == "" || wanPassword == "") {
			return nil, fmt.Errorf("%w: username dan password PPPoE wajib diisi", ErrInvalidInput)
		}
		if wanMode == "static" && wanStaticIP == "" {
			return nil, fmt.Errorf("%w: wan_static_ip wajib diisi untuk mode static", ErrInvalidInput)
		}

		respondPing := true
		if in.WANRespondPing != nil {
			respondPing = *in.WANRespondPing
		}
		respondTrace := true
		if in.WANRespondTraceroute != nil {
			respondTrace = *in.WANRespondTraceroute
		}

		appendWANCandidate := func(list *[]string, cmd string) {
			cmd = strings.TrimSpace(cmd)
			if cmd == "" {
				return
			}
			for _, existing := range *list {
				if strings.EqualFold(existing, cmd) {
					return
				}
			}
			*list = append(*list, cmd)
		}

		modeTokens := []string{wanMode}
		if wanMode == "ipoe" {
			modeTokens = []string{"ipoe", "dhcp"}
		}

		buildWAN := func(modeToken string, profileFirst bool, includeAuth bool, includeHost bool) string {
			parts := []string{"wan-ip", strconv.Itoa(in.WANIPID), "mode", modeToken}
			if profileFirst {
				parts = append(parts, "vlan-profile", wanVlanProfile)
				if wanIPProfile != "" {
					parts = append(parts, "ip-profile", wanIPProfile)
				}
			} else {
				if wanIPProfile != "" {
					parts = append(parts, "ip-profile", wanIPProfile)
				}
				parts = append(parts, "vlan-profile", wanVlanProfile)
			}
			if wanMode == "pppoe" {
				parts = append(parts, "username", wanUsername, "password", wanPassword)
				if includeAuth {
					parts = append(parts, "auth-mode", wanAuthMode)
				}
			}
			if wanMode == "static" {
				parts = append(parts, "ip-address", wanStaticIP)
			}
			if includeHost {
				parts = append(parts, "host", "1")
			}
			return strings.Join(parts, " ")
		}

		for _, token := range modeTokens {
			appendWANCandidate(&cmdCandidates, buildWAN(token, true, true, true))
			appendWANCandidate(&cmdCandidates, buildWAN(token, true, false, true))
			appendWANCandidate(&cmdCandidates, buildWAN(token, true, true, false))
			appendWANCandidate(&cmdCandidates, buildWAN(token, false, true, true))
			appendWANCandidate(&cmdCandidates, buildWAN(token, false, false, true))
			appendWANCandidate(&cmdCandidates, buildWAN(token, false, true, false))
		}

		pingWord := "disable"
		if respondPing {
			pingWord = "enable"
		}
		traceWord := "disable"
		if respondTrace {
			traceWord = "enable"
		}
		postCommandAlternatives = append(postCommandAlternatives,
			[]string{fmt.Sprintf("wan-ip %d ping-response %s traceroute-response %s", in.WANIPID, pingWord, traceWord)},
			[]string{fmt.Sprintf("wan-ip %d ping-response %s", in.WANIPID, pingWord)},
			[]string{fmt.Sprintf("wan-ip %d traceroute-response %s", in.WANIPID, traceWord)},
		)
		message = "WAN IP diperbarui"
	case "delete_wan_ip":
		if in.WANIPID < 1 || in.WANIPID > 8 {
			return nil, fmt.Errorf("%w: wan_ip_id harus 1-8", ErrInvalidInput)
		}
		cmdCandidates = []string{fmt.Sprintf("no wan-ip %d", in.WANIPID)}
		message = "WAN IP dihapus"
	case "delete_service_port":
		if in.ServicePortID < 1 || in.ServicePortID > 4095 {
			return nil, ErrInvalidInput
		}
		cmdCandidates = []string{fmt.Sprintf("no service-port %d", in.ServicePortID)}
		message = "service-port dihapus"
	case "delete_tcont":
		if in.TcontID < 1 || in.TcontID > 32 {
			return nil, ErrInvalidInput
		}
		cmdCandidates = []string{fmt.Sprintf("no tcont %d", in.TcontID)}
		message = "tcont dihapus"
	case "delete_gemport":
		if in.GemportID < 1 || in.GemportID > 4095 {
			return nil, ErrInvalidInput
		}
		cmdCandidates = []string{fmt.Sprintf("no gemport %d", in.GemportID)}
		message = "gemport dihapus"
	default:
		return nil, ErrInvalidInput
	}
	if len(cmdCandidates) == 0 {
		return nil, ErrInvalidInput
	}

	applyVia := strings.ToLower(strings.TrimSpace(in.ApplyVia))
	if applyVia == "" {
		applyVia = "auto"
	}
	if op != "set_service_port" {
		applyVia = "cli"
	}
	if applyVia != "cli" && applyVia != "snmp" && applyVia != "auto" {
		return nil, ErrInvalidInput
	}

	configTimeout := 26 * time.Second
	if op == "set_service_port" {
		configTimeout = 30 * time.Second
	}
	if op == "set_service_port_description" {
		configTimeout = 12 * time.Second
	}
	// wan-ip (PPPoE/IPoE) punya banyak kandidat sintaks + verifikasi CLI;
	// 16s terbukti kurang (HTTP 502) — beri budget lebih longgar.
	if op == "set_wan_ip" || op == "delete_wan_ip" {
		configTimeout = 40 * time.Second
	}
	ctxConfig, cancelConfig := context.WithTimeout(ctx, configTimeout)
	defer cancelConfig()

	methodUsed := "cli"
	snmpFallbackReason := ""
	if op == "set_service_port" && applyVia != "cli" {
		targetSVLAN := in.SVLAN
		if targetSVLAN == 0 {
			targetSVLAN = in.VLAN
		}
		ctxSNMP, cancelSNMP := context.WithTimeout(ctxConfig, 12*time.Second)
		snmpResult, snmpErr := service.applyServicePortViaSNMP(ctxSNMP, tenantID, id, ponPort, in, targetSVLAN)
		cancelSNMP()
		if snmpErr == nil {
			return &ONUConfigApplyResult{Operation: op, Method: "snmp", Executed: snmpResult.executed, Message: message}, nil
		}
		if applyVia == "snmp" {
			return nil, fmt.Errorf("apply via SNMP gagal: %w", snmpErr)
		}
		methodUsed = "cli-fallback"
		snmpFallbackReason = truncate(snmpErr.Error())
	}

	runInMode := func(session *ztecli.Session, enter []string, command string) ([]string, error) {
		makeSeq := func(preamble ...string) []string {
			seq := make([]string, 0, len(preamble)+len(enter)+2)
			seq = append(seq, preamble...)
			seq = append(seq, enter...)
			seq = append(seq, command, "end")
			return seq
		}
		sequences := [][]string{
			makeSeq(),
			makeSeq("configure terminal"),
			makeSeq("configure"),
			makeSeq("enable", "configure terminal"),
			makeSeq("enable", "configure"),
		}

		done := make([]string, 0, 32)
		errMsgs := make([]string, 0, len(sequences))
		for _, seq := range sequences {
			_, err := session.ExecuteSequence(ctxConfig, seq)
			done = append(done, seq...)
			if err == nil {
				return done, nil
			}
			errMsgs = append(errMsgs, err.Error())
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "forbidden") || strings.Contains(low, "rate limit") || strings.Contains(low, "autentikasi") {
				return done, err
			}
		}
		return done, fmt.Errorf("%s", strings.Join(errMsgs, " || "))
	}

	runCommand := func(command string) ([]string, error) {
		modeONU := []string{fmt.Sprintf("interface gpon-onu_%s:%d", ponPort, in.ONUID)}
		modePONONU := []string{
			"interface gpon-olt_" + ponPort,
			fmt.Sprintf("pon-onu-mng gpon-onu_%s:%d", ponPort, in.ONUID),
		}

		preferPONONU := op == "set_tcont" || op == "set_gemport" || op == "set_service_port" || op == "set_service_port_description" || op == "set_wan_ip" || op == "delete_wan_ip" || strings.HasPrefix(op, "delete_")
		firstEnter := modeONU
		secondEnter := modePONONU
		firstLabel := "mode-1"
		secondLabel := "mode-2"
		if preferPONONU {
			firstEnter = modePONONU
			secondEnter = modeONU
			firstLabel = "mode-2"
			secondLabel = "mode-1"
		}

		tryMode := func(label string, enter []string) ([]string, error) {
			var executed []string
			_, modeErr := service.withCLISession(ctxConfig, tenantID, id, func(session *ztecli.Session) (string, error) {
				raw, err := runInMode(session, enter, command)
				executed = append(executed, raw...)
				return "", err
			})
			if modeErr != nil {
				return executed, fmt.Errorf("%s (%v)", label, modeErr)
			}
			return executed, nil
		}

		exec1, err1 := tryMode(firstLabel, firstEnter)
		if err1 == nil {
			return exec1, nil
		}
		exec2, err2 := tryMode(secondLabel, secondEnter)
		if err2 == nil {
			return append(exec1, exec2...), nil
		}
		return append(exec1, exec2...), fmt.Errorf("command %q gagal: %v; %v", command, err1, err2)
	}

	var done []string
	applied := false
	var allErrors []string
	for _, command := range cmdCandidates {
		execAttempt, attemptErr := runCommand(command)
		done = append(done, execAttempt...)
		if attemptErr == nil {
			applied = true
			break
		}
		allErrors = append(allErrors, attemptErr.Error())
	}
	if !applied {
		if len(allErrors) == 0 {
			return nil, fmt.Errorf("tidak ada command konfigurasi yang berhasil")
		}
		return nil, fmt.Errorf("semua kandidat command gagal: %s", strings.Join(allErrors, " || "))
	}

	runCommandCandidates := func(candidates []string) ([]string, error) {
		if len(candidates) == 0 {
			return nil, nil
		}
		var doneCmd []string
		var errs []string
		for _, candidate := range candidates {
			execAttempt, attemptErr := runCommand(candidate)
			doneCmd = append(doneCmd, execAttempt...)
			if attemptErr == nil {
				return doneCmd, nil
			}
			errs = append(errs, attemptErr.Error())
		}
		if len(errs) == 0 {
			return doneCmd, fmt.Errorf("tidak ada kandidat command tambahan")
		}
		return doneCmd, fmt.Errorf("%s", strings.Join(errs, " || "))
	}
	if op == "set_service_port" || op == "set_wan_ip" {
		contextLabel := "service-port"
		if op == "set_wan_ip" {
			contextLabel = "wan-ip"
		}
		for _, candidateSet := range postCommandAlternatives {
			execExtra, errExtra := runCommandCandidates(candidateSet)
			done = append(done, execExtra...)
			if errExtra != nil {
				return nil, fmt.Errorf("opsi lanjutan %s gagal: %w", contextLabel, errExtra)
			}
		}
	}
	// lastVerified menampung snapshot config terbaru hasil probe verifikasi,
	// dipakai untuk memperbarui cache detail agar panel langsung menampilkan
	// perubahan (bukan cache lama).
	var lastVerified *ONUConfigDetail
	verifyApplied := func(maxAttempts int, waitEach time.Duration, check func(*ONUConfigDetail) bool) error {
		if maxAttempts < 1 {
			maxAttempts = 1
		}
		if waitEach <= 0 {
			waitEach = 300 * time.Millisecond
		}
		var lastErr error
		for attempt := 0; attempt < maxAttempts; attempt++ {
			verifyDetail, _, verifyErr := service.ProbeONUConfigCLI(ctxConfig, tenantID, id, ponPort, in.ONUID)
			if verifyErr != nil {
				lastErr = verifyErr
			} else if check(verifyDetail) {
				lastVerified = verifyDetail
				return nil
			} else {
				lastErr = fmt.Errorf("perubahan belum terbaca pada probe")
			}
			if attempt+1 < maxAttempts {
				time.Sleep(waitEach)
			}
		}
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("perubahan belum terbaca")
	}
	if op == "set_tcont" {
		targetProfile := strings.TrimSpace(in.TcontProfile)
		verifyErr := verifyApplied(6, 700*time.Millisecond, func(verifyDetail *ONUConfigDetail) bool {
			for _, tc := range verifyDetail.Tconts {
				if tc.ID == in.TcontID && strings.EqualFold(strings.TrimSpace(tc.Profile), targetProfile) {
					return true
				}
			}
			for _, p := range verifyDetail.DBAProfiles {
				if strings.EqualFold(strings.TrimSpace(p), targetProfile) {
					return true
				}
			}
			return false
		})
		if verifyErr != nil {
			return nil, fmt.Errorf("DBA profile %q belum terdeteksi setelah apply: %w", targetProfile, verifyErr)
		}
	}
	if op == "set_service_port" {
		targetSVLAN := in.SVLAN
		if targetSVLAN == 0 {
			targetSVLAN = in.VLAN
		}
		targetMode := serviceMode
		targetEtype := serviceEtherType
		targetDescription := strings.TrimSpace(serviceDescription)
		verifyErr := verifyApplied(3, 350*time.Millisecond, func(verifyDetail *ONUConfigDetail) bool {
			for _, sp := range verifyDetail.ServicePorts {
				if sp.ID != in.ServicePortID {
					continue
				}
				if sp.UserVLAN != in.UserVLAN || sp.VLAN != in.VLAN {
					continue
				}
				if sp.VPort > 0 && in.VPort > 0 && sp.VPort != in.VPort {
					continue
				}
				if targetSVLAN > 0 && sp.SVLAN > 0 && sp.SVLAN != targetSVLAN {
					continue
				}
				if in.UserSVLAN > 0 && sp.UserSVLAN > 0 && sp.UserSVLAN != in.UserSVLAN {
					continue
				}
				if in.CTagCOS > 0 && sp.CTagCOS > 0 && sp.CTagCOS != in.CTagCOS {
					continue
				}
				if in.STagCOS > 0 && sp.STagCOS > 0 && sp.STagCOS != in.STagCOS {
					continue
				}
				if targetDescription != "" && strings.TrimSpace(sp.Description) != "" && !strings.EqualFold(strings.TrimSpace(sp.Description), targetDescription) {
					continue
				}
				if targetMode != "" && strings.TrimSpace(sp.Mode) != "" {
					if modeSeen, ok := normalizeServicePortMode(sp.Mode); ok && modeSeen != targetMode {
						continue
					}
				}
				if targetEtype != "" && strings.TrimSpace(sp.EtherType) != "" {
					if etSeen, ok := normalizeServiceEtherType(sp.EtherType); ok && etSeen != targetEtype {
						continue
					}
				}
				return true
			}
			return false
		})
		if verifyErr != nil {
			return nil, fmt.Errorf("service-port %d belum terdeteksi setelah apply: %w", in.ServicePortID, verifyErr)
		}
		if targetMode != "" || targetEtype != "" || targetDescription != "" {
			parts := []string{}
			if targetMode != "" {
				parts = append(parts, "mode "+displayServicePortMode(targetMode))
			}
			if targetEtype != "" {
				parts = append(parts, "ether "+displayEtherType(targetEtype))
			}
			if targetDescription != "" {
				parts = append(parts, "description")
			}
			if len(parts) > 0 {
				message += " (" + strings.Join(parts, ", ") + ")"
			}
		}
	}
	if op == "set_wan_ip" {
		targetMode, _ := normalizeWANMode(in.WANMode)
		targetVlanProfile := strings.TrimSpace(in.WANVLANProfile)
		targetIPProfile := strings.TrimSpace(in.WANIPProfile)
		targetStatic := strings.TrimSpace(in.WANStaticIP)
		targetUsername := strings.TrimSpace(in.WANPPPoEUsername)
		targetPassword := strings.TrimSpace(in.WANPPPoEPassword)
		targetAuth, _ := normalizeWANAuthMode(in.WANAuthMode)
		targetPing := in.WANRespondPing
		targetTraceroute := in.WANRespondTraceroute
		verifyErr := verifyApplied(3, 350*time.Millisecond, func(verifyDetail *ONUConfigDetail) bool {
			for _, wan := range verifyDetail.WANIPs {
				if wan.ID != in.WANIPID {
					continue
				}
				if targetMode != "" && strings.TrimSpace(wan.Mode) != "" {
					if modeSeen, ok := normalizeWANMode(wan.Mode); ok && modeSeen != targetMode {
						continue
					}
				}
				if targetVlanProfile != "" && strings.TrimSpace(wan.VLANProfile) != "" && !strings.EqualFold(strings.TrimSpace(wan.VLANProfile), targetVlanProfile) {
					continue
				}
				if targetIPProfile != "" && strings.TrimSpace(wan.IPProfile) != "" && !strings.EqualFold(strings.TrimSpace(wan.IPProfile), targetIPProfile) {
					continue
				}
				if targetStatic != "" && strings.TrimSpace(wan.StaticIP) != "" && !strings.EqualFold(strings.TrimSpace(wan.StaticIP), targetStatic) {
					continue
				}
				if targetUsername != "" && strings.TrimSpace(wan.PPPoEUsername) != "" && strings.TrimSpace(wan.PPPoEUsername) != targetUsername {
					continue
				}
				if targetPassword != "" && strings.TrimSpace(wan.PPPoEPassword) != "" && strings.TrimSpace(wan.PPPoEPassword) != targetPassword {
					continue
				}
				if targetAuth != "" && strings.TrimSpace(wan.AuthMode) != "" {
					if authSeen, ok := normalizeWANAuthMode(wan.AuthMode); ok && authSeen != targetAuth {
						continue
					}
				}
				if targetPing != nil && wan.RespondPing != nil && *targetPing != *wan.RespondPing {
					continue
				}
				if targetTraceroute != nil && wan.RespondTraceroute != nil && *targetTraceroute != *wan.RespondTraceroute {
					continue
				}
				return true
			}
			return false
		})
		if verifyErr != nil {
			return nil, fmt.Errorf("WAN IP %d belum terdeteksi setelah apply: %w", in.WANIPID, verifyErr)
		}
		parts := make([]string, 0, 3)
		if targetMode != "" {
			parts = append(parts, "mode "+displayWANMode(targetMode))
		}
		if targetVlanProfile != "" {
			parts = append(parts, "vlan-profile")
		}
		if targetAuth != "" {
			parts = append(parts, "auth "+displayWANAuthMode(targetAuth))
		}
		if len(parts) > 0 {
			message += " (" + strings.Join(parts, ", ") + ")"
		}
	}
	if op == "delete_wan_ip" {
		verifyErr := verifyApplied(2, 250*time.Millisecond, func(verifyDetail *ONUConfigDetail) bool {
			for _, wan := range verifyDetail.WANIPs {
				if wan.ID == in.WANIPID {
					return false
				}
			}
			return true
		})
		if verifyErr != nil {
			return nil, fmt.Errorf("WAN IP %d masih terdeteksi setelah delete: %w", in.WANIPID, verifyErr)
		}
	}
	if op == "set_name" {
		name := strings.TrimSpace(in.Name)
		if name != "" {
			if uErr := service.repository.UpdateONUIdentityByRef(ctx, tenantID, id, ponPort, in.ONUID, &name, nil); uErr != nil {
				log.Printf("ApplyONUConfigCLI cache set_name %s:%d error: %v", ponPort, in.ONUID, uErr)
			}
		}
	}
	if op == "set_description" {
		desc := strings.TrimSpace(in.Description)
		if desc != "" {
			if uErr := service.repository.UpdateONUIdentityByRef(ctx, tenantID, id, ponPort, in.ONUID, nil, &desc); uErr != nil {
				log.Printf("ApplyONUConfigCLI cache set_description %s:%d error: %v", ponPort, in.ONUID, uErr)
			}
		}
	}
	if snmpFallbackReason != "" {
		done = append([]string{"snmp-fallback: " + snmpFallbackReason}, done...)
		if message != "" {
			message += " (SNMP gagal, fallback CLI aktif)"
		}
	}
	// Sinkronkan cache detail agar panel tidak menampilkan config lama:
	// simpan snapshot verifikasi (paling segar) atau invalidasi + refresh background.
	if op != "set_name" && op != "set_description" {
		cacheCtx, cancelCache := context.WithTimeout(context.Background(), 5*time.Second)
		if realIndex, idxErr := service.repository.FindONUIndexByRef(cacheCtx, tenantID, id, ponPort, in.ONUID); idxErr == nil && strings.TrimSpace(realIndex) != "" {
			if lastVerified != nil {
				if saveErr := service.repository.SaveONUDetailCache(cacheCtx, tenantID, id, realIndex, lastVerified); saveErr != nil {
					log.Printf("ApplyONUConfigCLI SaveONUDetailCache %s error: %v", realIndex, saveErr)
				}
			} else {
				if invErr := service.repository.InvalidateONUDetailCache(cacheCtx, tenantID, id, realIndex); invErr != nil {
					log.Printf("ApplyONUConfigCLI InvalidateONUDetailCache %s error: %v", realIndex, invErr)
				}
				service.scheduleDetailConfigFetch(tenantID, id, ponPort, in.ONUID, realIndex)
			}
		}
		cancelCache()
	}
	return &ONUConfigApplyResult{Operation: op, Method: methodUsed, Executed: done, Message: message}, nil
}

func (service *Service) applyONUAutoConfigCLI(ctx context.Context, tenantID, id, ponPort string, in ONUConfigApplyInput) (*ONUConfigApplyResult, error) {
	wanMode, ok := normalizeWANMode(in.WANMode)
	if !ok {
		return nil, fmt.Errorf("%w: wan_mode auto-config harus pppoe/ipoe/static", ErrInvalidInput)
	}
	if in.TcontID < 1 || strings.TrimSpace(in.TcontProfile) == "" {
		return nil, fmt.Errorf("%w: tcont_id dan tcont_profile wajib diisi", ErrInvalidInput)
	}
	if in.GemportID < 1 {
		return nil, fmt.Errorf("%w: gemport_id wajib diisi", ErrInvalidInput)
	}
	if strings.TrimSpace(in.UpstreamProfile) == "" && strings.TrimSpace(in.DownstreamProfile) == "" {
		return nil, fmt.Errorf("%w: upstream/downstream profile minimal satu wajib diisi", ErrInvalidInput)
	}
	if in.ServicePortID < 1 || in.VPort < 1 || in.UserVLAN < 1 || (in.VLAN == 0 && in.CVID == 0) {
		return nil, fmt.Errorf("%w: service_port_id, vport, user_vlan, vlan wajib diisi", ErrInvalidInput)
	}
	if in.WANIPID < 1 || strings.TrimSpace(in.WANVLANProfile) == "" {
		return nil, fmt.Errorf("%w: wan_ip_id dan wan_vlan_profile wajib diisi", ErrInvalidInput)
	}
	if wanMode == "pppoe" {
		if strings.TrimSpace(in.WANPPPoEUsername) == "" || strings.TrimSpace(in.WANPPPoEPassword) == "" {
			return nil, fmt.Errorf("%w: username/password PPPoE wajib diisi", ErrInvalidInput)
		}
	}
	if wanMode == "static" {
		if strings.TrimSpace(in.WANStaticIP) == "" {
			return nil, fmt.Errorf("%w: wan_static_ip wajib diisi untuk mode static", ErrInvalidInput)
		}
	}

	steps := []ONUConfigApplyInput{
		{
			PON:          ponPort,
			ONUID:        in.ONUID,
			Operation:    "set_tcont",
			TcontID:      in.TcontID,
			TcontName:    in.TcontName,
			TcontProfile: in.TcontProfile,
		},
		{
			PON:               ponPort,
			ONUID:             in.ONUID,
			Operation:         "set_gemport",
			GemportID:         in.GemportID,
			UpstreamProfile:   in.UpstreamProfile,
			DownstreamProfile: in.DownstreamProfile,
		},
		{
			PON:                ponPort,
			ONUID:              in.ONUID,
			Operation:          "set_service_port",
			ServicePortID:      in.ServicePortID,
			VPort:              in.VPort,
			UserVLAN:           in.UserVLAN,
			UserSVLAN:          in.UserSVLAN,
			VLAN:               in.VLAN,
			CVID:               in.CVID,
			SVLAN:              in.SVLAN,
			SVID:               in.SVID,
			CTagCOS:            in.CTagCOS,
			STagCOS:            in.STagCOS,
			ServicePortMode:    in.ServicePortMode,
			ServiceDescription: in.ServiceDescription,
			EtherType:          in.EtherType,
			ApplyVia:           "auto",
		},
		{
			PON:                  ponPort,
			ONUID:                in.ONUID,
			Operation:            "set_wan_ip",
			WANIPID:              in.WANIPID,
			WANMode:              wanMode,
			WANAuthMode:          in.WANAuthMode,
			WANVLANProfile:       in.WANVLANProfile,
			WANIPProfile:         in.WANIPProfile,
			WANStaticIP:          in.WANStaticIP,
			WANPPPoEUsername:     in.WANPPPoEUsername,
			WANPPPoEPassword:     in.WANPPPoEPassword,
			WANRespondPing:       in.WANRespondPing,
			WANRespondTraceroute: in.WANRespondTraceroute,
		},
	}

	executed := make([]string, 0, 64)
	methods := make([]string, 0, 4)
	seenMethod := map[string]struct{}{}
	for _, step := range steps {
		res, err := service.ApplyONUConfigCLI(ctx, tenantID, id, step)
		if err != nil {
			return nil, fmt.Errorf("auto-config gagal di step %s: %w", step.Operation, err)
		}
		executed = append(executed, "step:"+step.Operation+" method:"+res.Method)
		executed = append(executed, res.Executed...)
		method := strings.TrimSpace(res.Method)
		if method == "" {
			method = "cli"
		}
		if _, ok := seenMethod[method]; !ok {
			seenMethod[method] = struct{}{}
			methods = append(methods, method)
		}
	}

	methodUsed := strings.Join(methods, "+")
	if methodUsed == "" {
		methodUsed = "cli"
	}
	return &ONUConfigApplyResult{
		Operation: "auto_config_onu",
		Method:    methodUsed,
		Executed:  executed,
		Message:   fmt.Sprintf("auto-config ONU selesai (%s)", displayWANMode(wanMode)),
	}, nil
}

// DeleteONUCli menghapus konfigurasi ONU pada interface gpon-olt.
func (service *Service) DeleteONUCli(ctx context.Context, tenantID, id, ponPort string, onuID int) error {
	ponPort = sanitizePON(ponPort)
	if ponPort == "" || onuID < 1 || onuID > 128 {
		return ErrInvalidInput
	}
	_, err := service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
		commands := []string{
			"configure terminal",
			"interface gpon-olt_" + ponPort,
			fmt.Sprintf("no onu %d", onuID),
			"end",
		}
		_, execErr := session.ExecuteSequence(ctx, commands)
		return "", execErr
	})
	return err
}

// BounceONUCli melakukan reset ringan via CLI: state off lalu on.
func (service *Service) BounceONUCli(ctx context.Context, tenantID, id, ponPort string, onuID int) error {
	if err := service.SetONUStateCli(ctx, tenantID, id, ponPort, onuID, false); err != nil {
		return err
	}
	time.Sleep(1200 * time.Millisecond)
	if err := service.SetONUStateCli(ctx, tenantID, id, ponPort, onuID, true); err != nil {
		return err
	}
	return nil
}

// RebootONUCli melakukan reboot ONU via CLI (`onu reset <id>` dalam mode
// interface PON). Lebih andal lintas-firmware dibanding SNMP set.
func (service *Service) RebootONUCli(ctx context.Context, tenantID, id, ponPort string, onuID int) error {
	ponPort = sanitizePON(ponPort)
	if ponPort == "" || onuID < 1 || onuID > 128 {
		return ErrInvalidInput
	}
	_, err := service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
		commands := []string{
			"configure terminal",
			"interface gpon-olt_" + ponPort,
			fmt.Sprintf("onu reset %d", onuID),
			"end",
		}
		_, execErr := session.ExecuteSequence(ctx, commands)
		return "", execErr
	})
	return err
}

// SetONUStateCli meng-disable/enable ONU via CLI (`onu state`).
func (service *Service) SetONUStateCli(ctx context.Context, tenantID, id, ponPort string, onuID int, enable bool) error {
	ponPort = sanitizePON(ponPort)
	if ponPort == "" || onuID < 1 || onuID > 128 {
		return ErrInvalidInput
	}
	state := "off"
	if enable {
		state = "on"
	}
	_, err := service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
		commands := []string{
			"configure terminal",
			"interface gpon-olt_" + ponPort,
			fmt.Sprintf("onu state %d %s", onuID, state),
			"end",
		}
		_, execErr := session.ExecuteSequence(ctx, commands)
		return "", execErr
	})
	return err
}

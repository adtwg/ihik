package httpapi

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
	"isp-billing/internal/zte"
)

// Delta-test penuh subtree ONU 1082.500.10.2.* (semua sub-tabel:
// oktet, name, status, jarak, dan tabel yang belum terpetakan).
// Async: POST mulai, GET laporan teks. Tidak ada timeout gateway.

type octScanState struct {
	mu      sync.Mutex
	running bool
	report  string
}

var (
	octMu     sync.Mutex
	octStates = map[string]*octScanState{}
)

func octGetState(oltID string) *octScanState {
	octMu.Lock()
	defer octMu.Unlock()
	st, ok := octStates[oltID]
	if !ok {
		st = &octScanState{}
		octStates[oltID] = st
	}
	return st
}

const octGapSeconds = 45

func walkPrivateCounters(ctx context.Context, session *gosnmp.GoSNMP) (map[string]map[int]uint64, string) {
	out, err := zte.WalkAllPrivateCounters(session)
	if err != nil {
		return out, err.Error()
	}
	return out, ""
}

func (server *server) oltOctScan(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	oltID := request.PathValue("oltID")
	st := octGetState(oltID)
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if request.Method == http.MethodGet {
		st.mu.Lock()
		running, text := st.running, st.report
		st.mu.Unlock()
		if running {
			text = "SEDANG BERJALAN - tunggu, jalankan GET lagi."
		}
		if text == "" {
			text = "BELUM ADA HASIL. Jalankan POST dulu."
		}
		response.Write([]byte(text))
		return
	}

	st.mu.Lock()
	if st.running {
		st.mu.Unlock()
		response.Write([]byte("sudah berjalan, tunggu...\n"))
		return
	}
	st.running = true
	st.report = ""
	st.mu.Unlock()

	go server.runOctScan(context.Background(), tenantID, oltID, st)
	response.Write([]byte("DELTA-TEST DIMULAI (walk privat .500.10.2.3.2.2.1.*, ~1 menit). Tunggu hasil.\n"))
}

type colStat struct {
	rise  int
	total uint64
	ex    []string
}

func (server *server) runOctScan(ctx context.Context, tenantID, oltID string, st *octScanState) {
	defer func() {
		if r := recover(); r != nil {
			st.mu.Lock()
			st.running = false
			st.report = fmt.Sprintf("PANIC: %v", r)
			st.mu.Unlock()
		}
	}()
	fail := func(msg string) {
		st.mu.Lock()
		st.running = false
		st.report = msg
		st.mu.Unlock()
	}

	sess1, cleanup1, err := server.olts.OpenSession(ctx, tenantID, oltID)
	if err != nil {
		fail("connect gagal: " + err.Error())
		return
	}
	defer cleanup1()

	t0 := time.Now()
	a, errA := walkPrivateCounters(ctx, sess1)
	if len(a) == 0 {
		fail("walk1 kosong: " + errA)
		return
	}
	walk1s := time.Since(t0).Seconds()

	if d := octGapSeconds - int(time.Since(t0).Seconds()); d > 0 {
		time.Sleep(time.Duration(d) * time.Second)
	}

	sess2, cleanup2, err := server.olts.OpenSession(ctx, tenantID, oltID)
	if err != nil {
		fail("connect2 gagal: " + err.Error())
		return
	}
	defer cleanup2()

	b, errB := walkPrivateCounters(ctx, sess2)
	if len(b) == 0 {
		fail("walk2 kosong: " + errB)
		return
	}
	elapsed := time.Since(t0).Seconds()

	colStats := map[int]*colStat{}
	for idx := range b {
		vaRow, haveA := a[idx]
		if !haveA {
			continue
		}
		for col, vb := range b[idx] {
			va, have := vaRow[col]
			if !have || vb <= va {
				continue
			}
			s, ok := colStats[col]
			if !ok {
				s = &colStat{}
				colStats[col] = s
			}
			s.rise++
			s.total += vb - va
			if len(s.ex) < 4 {
				s.ex = append(s.ex, fmt.Sprintf("%s:+%d", idx, vb-va))
			}
		}
	}

	cols := make([]int, 0, len(colStats))
	for c := range colStats {
		cols = append(cols, c)
	}
	sort.Slice(cols, func(i, j int) bool {
		return colStats[cols[i]].total > colStats[cols[j]].total
	})

	var sb strings.Builder
	fmt.Fprintf(&sb, "walk1=%.0fs total=%.0fs onu1=%d onu2=%d (gap efektif=%.0fs)\n",
		walk1s, elapsed, len(a), len(b), elapsed-walk1s)
	sb.WriteString("KOLOM NAIK (urut total delta di semua ONU):\n")
	sb.WriteString("col rise      KB/s  contoh ONU:delta\n")
	for _, c := range cols {
		s := colStats[c]
		kb := float64(s.total) / (elapsed - walk1s) / 1024.0
		fmt.Fprintf(&sb, "%3d %-4d %10.1f  %s\n", c, s.rise, kb, strings.Join(s.ex, " "))
	}
	if len(cols) == 0 {
		sb.WriteString("TIDAK ADA yang naik — pastikan trafik jalan saat tes.\n")
	}
	if errA != "" {
		sb.WriteString("err walk1: " + errA + "\n")
	}
	if errB != "" {
		sb.WriteString("err walk2: " + errB + "\n")
	}

	st.mu.Lock()
	st.running = false
	st.report = sb.String()
	st.mu.Unlock()
	log.Printf("octscan %s selesai (%d kolom naik)", oltID, len(cols))
}

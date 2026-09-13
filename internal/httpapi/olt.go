package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
	"isp-billing/internal/olt"
	"isp-billing/internal/zte"
	"isp-billing/internal/ztecli"
)

// Guard sederhana agar tidak ada banyak goroutine refresh/sync OLT yang sama
// berjalan bersamaan dan membebani OLT.
var (
	syncInFlight    sync.Map
	refreshInFlight sync.Map
	opticalInFlight sync.Map
)

func oltErrorResponseWithContext(response http.ResponseWriter, err error, operation, oltID string) {
	// Log detail untuk debugging bottleneck multi-OLT: kunci terpecah jika
	// selalu muncul error cli_busy di OLT yang sedang tidak dipakai user.
	if errors.Is(err, olt.ErrLocked) {
		log.Printf("olt api error [operation=%s olt=%s]: %v", operation, oltID, err)
		writeError(response, http.StatusConflict, "cli_busy", err.Error())
		return
	}
	oltErrorResponse(response, err)
}

func oltErrorResponse(response http.ResponseWriter, err error) {
	oltErrorResponseWithContext(response, err, "", "")
}

func (server *server) listOLTs(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	items, err := server.olts.List(request.Context(), tenantID)
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (server *server) createOLT(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	input, ok := decodeOLTInput(response, request)
	if !ok {
		return
	}
	created, err := server.olts.Create(request.Context(), tenantID, input)
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (server *server) getOLT(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	item, err := server.olts.Get(request.Context(), tenantID, request.PathValue("oltID"))
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (server *server) updateOLT(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	input, ok := decodeOLTInput(response, request)
	if !ok {
		return
	}
	updated, err := server.olts.Update(request.Context(), tenantID, request.PathValue("oltID"), input)
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (server *server) deleteOLT(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	// Hapus OLT dari DB; tidak perlu sesi CLI jadi tidak mengganggu operasi OLT lain.
	// Timeout ringkas supaya tidak menunggu antrian CLI yang sedang sibuk.
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	if err := server.olts.Delete(ctx, tenantID, request.PathValue("oltID")); err != nil {
		oltErrorResponseWithContext(response, err, "delete-olt", request.PathValue("oltID"))
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

// testOLT menguji koneksi SNMP dan deteksi model.
func (server *server) testOLT(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	info, err := server.olts.Test(request.Context(), tenantID, request.PathValue("oltID"))
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, info)
}

// testOLTCLI menguji koneksi CLI (SSH/Telnet) OLT.
func (server *server) testOLTCLI(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	info, err := server.olts.TestCLI(request.Context(), tenantID, request.PathValue("oltID"))
	if err != nil {
		if errors.Is(err, olt.ErrInvalidInput) {
			writeError(response, http.StatusBadRequest, "invalid_request", "Kredensial CLI belum lengkap. Isi username/password CLI lalu simpan OLT.")
			return
		}
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, info)
}

// syncONUS membaca ulang seluruh ONU dari OLT ke database.
// Operasi bisa lama, segera balas 202 Accepted dan jalankan di background
// agar browser/Nginx tidak timeout.
func (server *server) syncONUS(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	oltID := request.PathValue("oltID")
	key := tenantID + "/" + oltID
	if _, running := syncInFlight.LoadOrStore(key, true); running {
		writeJSON(response, http.StatusAccepted, map[string]any{"ok": true, "message": "sinkronisasi ONU sudah berjalan"})
		return
	}
	go func() {
		defer syncInFlight.Delete(key)

		ctxSync, cancelSync := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancelSync()
		n, err := server.olts.SyncONUs(ctxSync, tenantID, oltID)
		if err != nil {
			log.Printf("syncONUS SyncONUs error: %v", err)
			return
		}
		log.Printf("syncONUS SyncONUs ok: %d ONU", n)

		// Probe CLI hanya jika OLT punya kredensial CLI dan tidak sedang dipakai.
		// Jika OLT CLI sibuk/gagal, optical/traffic tetap partial dari SNMP.
		ctxOpt, cancelOpt := context.WithTimeout(context.Background(), 900*time.Second)
		optUpdated, optErr := server.olts.SyncOpticalFromCLI(ctxOpt, tenantID, oltID)
		cancelOpt()
		if optErr != nil {
			log.Printf("syncONUS SyncOpticalFromCLI error: %v", optErr)
		} else {
			log.Printf("syncONUS SyncOpticalFromCLI updated: %d", optUpdated)
		}

		ctxTraffic, cancelTraffic := context.WithTimeout(context.Background(), 300*time.Second)
		if t, tErr := server.olts.RefreshTraffic(ctxTraffic, tenantID, oltID); tErr != nil {
			log.Printf("syncONUS RefreshTraffic error: %v", tErr)
		} else {
			log.Printf("syncONUS RefreshTraffic updated: %d", t)
		}
		cancelTraffic()
	}()
	writeJSON(response, http.StatusAccepted, map[string]any{"ok": true, "message": "sinkronisasi ONU berjalan di background"})
}

// syncOpticalONUS memicu pembacaan Rx/Tx dBm dan jarak per-ONU via CLI.
func (server *server) syncOpticalONUS(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	oltID := request.PathValue("oltID")
	key := tenantID + "/" + oltID
	if _, running := opticalInFlight.LoadOrStore(key, true); running {
		writeJSON(response, http.StatusAccepted, map[string]any{"ok": true, "message": "sinkronisasi optik sudah berjalan"})
		return
	}
	go func() {
		defer opticalInFlight.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
		defer cancel()
		count, err := server.olts.SyncOpticalFromCLI(ctx, tenantID, oltID)
		if err != nil {
			log.Printf("syncOpticalONUS SyncOpticalFromCLI error: %v", err)
			return
		}
		log.Printf("syncOpticalONUS SyncOpticalFromCLI updated: %d", count)
		if t, tErr := server.olts.RefreshTraffic(ctx, tenantID, oltID); tErr != nil {
			log.Printf("syncOpticalONUS RefreshTraffic error: %v", tErr)
		} else {
			log.Printf("syncOpticalONUS RefreshTraffic updated: %d", t)
		}
	}()
	writeJSON(response, http.StatusAccepted, map[string]any{"ok": true, "message": "sinkronisasi optik per-ONU berjalan di background"})
}

func (server *server) listONUS(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	items, err := server.olts.ListONUs(request.Context(), tenantID, request.PathValue("oltID"))
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	response.Header().Set("Pragma", "no-cache")
	response.Header().Set("Expires", "0")
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

// onuAction memproses aksi per-ONU dengan prioritas SNMP lalu fallback CLI.
// POST /api/v1/olts/{oltID}/onus/{action}
// body: {index, pon, onu_id}
func (server *server) onuAction(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Index string `json:"index"`
		PON   string `json:"pon"`
		ONUID int    `json:"onu_id"`
	}
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Index) == "" && (strings.TrimSpace(input.PON) == "" || input.ONUID < 1) {
		writeError(response, http.StatusBadRequest, "invalid_request", "index atau pasangan pon+onu_id wajib diisi.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	defer cancel()

	action := request.PathValue("action")
	result := olt.ONUActionResult{Action: action}
	var actionErr error
	switch action {
	case "reboot":
		result, actionErr = server.olts.RebootONUHybrid(ctx, tenantID, request.PathValue("oltID"), input.Index, input.PON, input.ONUID)
	case "reset":
		result, actionErr = server.olts.ResetONUHybrid(ctx, tenantID, request.PathValue("oltID"), input.Index, input.PON, input.ONUID)
	case "delete":
		result, actionErr = server.olts.DeleteONUHybrid(ctx, tenantID, request.PathValue("oltID"), input.Index, input.PON, input.ONUID)
	case "disable":
		if strings.TrimSpace(input.Index) == "" {
			writeError(response, http.StatusBadRequest, "invalid_request", "action disable butuh index ONU untuk SNMP.")
			return
		}
		actionErr = server.olts.SetONUEnabled(ctx, tenantID, request.PathValue("oltID"), input.Index, false)
		result.Method = "snmp"
	case "enable":
		if strings.TrimSpace(input.Index) == "" {
			writeError(response, http.StatusBadRequest, "invalid_request", "action enable butuh index ONU untuk SNMP.")
			return
		}
		actionErr = server.olts.SetONUEnabled(ctx, tenantID, request.PathValue("oltID"), input.Index, true)
		result.Method = "snmp"
	default:
		writeError(response, http.StatusNotFound, "not_found", "Aksi tidak dikenal.")
		return
	}
	if actionErr != nil {
		oltErrorResponse(response, actionErr)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"status": "ok", "action": result.Action, "method": result.Method, "fallback": result.Fallback, "message": result.Message})
}

// onuSyncOne melakukan update/sync satu ONU (status + redaman + jarak) via CLI
// lalu menulis balik ke cache DB agar tabel utama langsung konsisten.
// POST /api/v1/olts/{oltID}/onu-sync
// body: {index, pon, onu_id}
func (server *server) onuSyncOne(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Index string `json:"index"`
		PON   string `json:"pon"`
		ONUID int    `json:"onu_id"`
	}
	if err := decoder.Decode(&input); err != nil || (strings.TrimSpace(input.Index) == "" && (strings.TrimSpace(input.PON) == "" || input.ONUID < 1)) {
		writeError(response, http.StatusBadRequest, "invalid_request", "index atau pasangan pon+onu_id wajib diisi.")
		return
	}
	// Jalur SNMP-only cepat; bila SNMP gagal, CLI dijadwalkan async (non-blocking).
	ctx, cancel := context.WithTimeout(request.Context(), 25*time.Second)
	defer cancel()
	result, err := server.olts.SyncONULiveByRef(ctx, tenantID, request.PathValue("oltID"), input.Index, input.PON, input.ONUID)
	if err != nil {
		oltErrorResponseWithContext(response, err, "onu-sync", request.PathValue("oltID"))
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": "sync ONU berhasil",
		"result":  result,
	})
}

func decodeOLTInput(response http.ResponseWriter, request *http.Request) (olt.SaveInput, bool) {
	request.Body = http.MaxBytesReader(response, request.Body, 16*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input olt.SaveInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Data OLT tidak valid.")
		return olt.SaveInput{}, false
	}
	return input, true
}

// listONUSPaged: server-side DataTables-style pagination.
// GET /api/v1/olts/{oltID}/onus-paged?page=1&page_size=25&search=&sort=&order=
func (server *server) listONUSPaged(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	query := request.URL.Query()
	page := atoiDefault(query.Get("page"), 1)
	pageSize := atoiDefault(query.Get("page_size"), 25)
	if pageSize > 200 {
		pageSize = 200
	}
	result, err := server.olts.ListONUSPaged(request.Context(), tenantID,
		request.PathValue("oltID"), page, pageSize,
		query.Get("search"), query.Get("sort"), query.Get("order"), query.Get("pon"))
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	// Kontrak API tabel: items SELALU array (bukan null)
	// agar frontend tidak crash saat OLT belum punya ONU.
	if result.Items == nil {
		result.Items = []zte.ONU{}
	}
	response.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	response.Header().Set("Pragma", "no-cache")
	response.Header().Set("Expires", "0")
	writeJSON(response, http.StatusOK, result)
}

// refreshTraffic: sampling counter SNMP dan hitung bps realtime.
// Operasi bisa lama; segera balas 202 Accepted dan jalankan di background
// agar browser/Nginx tidak timeout dan tombol Trafik Live tidak stuck.
func (server *server) refreshTraffic(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	oltID := request.PathValue("oltID")
	key := tenantID + "/" + oltID
	if _, running := refreshInFlight.LoadOrStore(key, true); running {
		writeJSON(response, http.StatusAccepted, map[string]any{"ok": true, "message": "refresh trafik sudah berjalan"})
		return
	}
	go func() {
		defer refreshInFlight.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		_, _ = server.olts.RefreshTraffic(ctx, tenantID, oltID)
	}()
	writeJSON(response, http.StatusAccepted, map[string]any{"ok": true, "message": "refresh trafik berjalan di background"})
}

func atoiDefault(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

// listUnconfiguredONUS: scan ONU unconfigured (single PON atau bulk).
// GET /api/v1/olts/{oltID}/uncfg?pon=1/2/1
// - jika pon dikosongkan: scan bulk semua port PON dari cache ONU.
func (server *server) listUnconfiguredONUS(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 120*time.Second)
	defer cancel()
	result, err := server.olts.ListUnconfiguredONUsBulk(ctx, tenantID,
		request.PathValue("oltID"), request.URL.Query().Get("pon"))
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

// provisionONU: daftarkan ONU baru via CLI.
// POST /api/v1/olts/{oltID}/provision-onu  {pon,onu_id,type,serial,description}
func (server *server) provisionONU(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 8*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		PON         string `json:"pon"`
		ONUID       int    `json:"onu_id"`
		Type        string `json:"type"`
		Serial      string `json:"serial"`
		Description string `json:"description"`
	}
	if err := decoder.Decode(&input); err != nil || input.PON == "" || input.Serial == "" || input.Type == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "pon, onu_id, type, dan serial wajib diisi.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	defer cancel()
	executed, err := server.olts.ProvisionONU(ctx, tenantID, request.PathValue("oltID"),
		input.PON, input.ONUID, input.Type, input.Serial, input.Description)
	if err != nil {
		switch {
		case errors.Is(err, olt.ErrLocked):
			writeError(response, http.StatusConflict, "cli_busy", err.Error())
		case errors.Is(err, olt.ErrForbiddenCmd):
			writeError(response, http.StatusForbidden, "forbidden_command", err.Error())
		case errors.Is(err, olt.ErrInvalidInput):
			writeError(response, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, ztecli.ErrAuthFailed):
			writeError(response, http.StatusUnauthorized, "cli_auth_failed", "Autentikasi CLI OLT gagal.")
		case errors.Is(err, ztecli.ErrRateLimited):
			writeError(response, http.StatusTooManyRequests, "rate_limited", err.Error())
		default:
			writeError(response, http.StatusBadGateway, "cli_failed", err.Error())
		}
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"status": "provisioned", "executed": executed})
}

// onuActionCli: reboot/disable/enable via CLI.
// POST /api/v1/olts/{oltID}/onus-cli/{action} {pon, onu_id}
func (server *server) onuActionCli(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		PON   string `json:"pon"`
		ONUID int    `json:"onu_id"`
	}
	if err := decoder.Decode(&input); err != nil || input.PON == "" || input.ONUID < 1 {
		writeError(response, http.StatusBadRequest, "invalid_request", "pon dan onu_id wajib valid.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 60*time.Second)
	defer cancel()
	var actionErr error
	switch request.PathValue("action") {
	case "reboot":
		actionErr = server.olts.RebootONUCli(ctx, tenantID, request.PathValue("oltID"), input.PON, input.ONUID)
	case "disable":
		actionErr = server.olts.SetONUStateCli(ctx, tenantID, request.PathValue("oltID"), input.PON, input.ONUID, false)
	case "enable":
		actionErr = server.olts.SetONUStateCli(ctx, tenantID, request.PathValue("oltID"), input.PON, input.ONUID, true)
	default:
		writeError(response, http.StatusNotFound, "not_found", "Aksi tidak dikenal.")
		return
	}
	if actionErr != nil {
		switch {
		case errors.Is(actionErr, olt.ErrLocked):
			writeError(response, http.StatusConflict, "cli_busy", actionErr.Error())
		case errors.Is(actionErr, olt.ErrForbiddenCmd):
			writeError(response, http.StatusForbidden, "forbidden_command", actionErr.Error())
		case errors.Is(actionErr, ztecli.ErrRateLimited):
			writeError(response, http.StatusTooManyRequests, "rate_limited", actionErr.Error())
		default:
			writeError(response, http.StatusBadGateway, "cli_failed", actionErr.Error())
		}
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

// onuStats: ringkasan jumlah ONU per status + daftar PON port.
// GET /api/v1/olts/{oltID}/onus-stats
func (server *server) onuStats(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	stats, err := server.olts.GetStats(request.Context(), tenantID, request.PathValue("oltID"))
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	response.Header().Set("Pragma", "no-cache")
	response.Header().Set("Expires", "0")
	writeJSON(response, http.StatusOK, stats)
}

// onuDaily: riwayat trafik harian satu ONU.
// GET /api/v1/olts/{oltID}/onus/daily?index=...&days=30
func (server *server) onuDaily(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	index := request.URL.Query().Get("index")
	if index == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Parameter index wajib diisi.")
		return
	}
	days := atoiDefault(request.URL.Query().Get("days"), 30)
	rows, err := server.olts.GetDaily(request.Context(), tenantID,
		request.PathValue("oltID"), index, days)
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": rows})
}

// onuIntraday: sampel bps per menit untuk grafik naik-turun.
// GET /api/v1/olts/{oltID}/onus/intraday?index=...&minutes=180
func (server *server) onuIntraday(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	index := request.URL.Query().Get("index")
	if index == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Parameter index wajib diisi.")
		return
	}
	minutes := atoiDefault(request.URL.Query().Get("minutes"), 180)
	rows, err := server.olts.GetIntraday(request.Context(), tenantID,
		request.PathValue("oltID"), index, minutes)
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": rows})
}

// oltHealth: snapshot kesehatan fisik OLT (card/CPU/temp/fan/SFP).
// GET /api/v1/olts/{oltID}/health — cache 15s di service layer.
func (server *server) oltHealth(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	// Timeout pendek: health SNMP untuk C320 V2.1 sering tidak tersedia,
	// jangan membuat panel terasa macet. CLI fallback tidak dipakai di sini.
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	health, err := server.olts.GetHealth(ctx, tenantID, request.PathValue("oltID"))
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, health)
}

// oltChassis: layout fisik OLT (chassis + card + port) untuk visualisasi.
// GET /api/v1/olts/{oltID}/chassis — health SNMP + fallback CLI "show card".
func (server *server) oltChassis(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	// Timeout lebih longgar dari /health karena bisa memicu CLI "show card".
	ctx, cancel := context.WithTimeout(request.Context(), 25*time.Second)
	defer cancel()
	view, err := server.olts.Chassis(ctx, tenantID, request.PathValue("oltID"))
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, view)
}

// oltDebugWalk: endpoint diagnosa — walk tiap OID profil dan laporkan
// jumlah entri + 6 sampel "suffix=value" mentah. Dipakai untuk mencocokkan
// format index antar-tabel di firmware produksi. Hanya troubleshooting.
func (server *server) oltIfNameTraffic(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 120*time.Second)
	defer cancel()
	session, cleanup, err := server.olts.OpenSession(ctx, tenantID, request.PathValue("oltID"))
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	defer cleanup()
	t0 := time.Now()
	data, err := zte.IfNameTraffic(session)
	if err != nil {
		oltErrorResponse(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"count":      len(data),
		"elapsed_ms": time.Since(t0).Milliseconds(),
		"interfaces": data,
	})
}

func (server *server) oltDebugWalk(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 120*time.Second)
	defer cancel()
	result := map[string]any{}

	start := time.Now()
	session, cleanup, err := server.olts.OpenSession(ctx, tenantID, request.PathValue("oltID"))
	if err != nil {
		writeJSON(response, http.StatusOK, map[string]any{"step": "connect", "error": err.Error()})
		return
	}
	defer cleanup()

	sysDescr := sessionSysDescr(ctx, session)
	profile := zte.DetectFirmware(sysDescr)
	result["sys_descr"] = sysDescr
	result["profile"] = profile.Name

	addOID := func(suffix string) string {
		if suffix == "" {
			return ""
		}
		if strings.HasPrefix(suffix, "1.") {
			return suffix
		}
		return profile.BaseOID + suffix
	}
	tables := map[string]string{
		"name":       addOID(profile.ONUName),
		"serial_str": addOID(profile.ONUSerial),
		"serial_hex": addOID(profile.ONUSerialHex),
		"status":     addOID(profile.ONUStatus),
		"distance":   addOID(profile.Distance),
		"sfp_rx":     profile.OLTRxPower,
		"sfp_tx":     profile.OLTTxPower,
		"sfp_bias":   "1.3.6.1.4.1.3902.1015.3.1.13.1.9",
		"sfp_temp":   "1.3.6.1.4.1.3902.1015.3.1.13.1.12",
		"sfp_tx2":    "1.3.6.1.4.1.3902.1015.3.1.13.1.5",
		"sfp_rx2":    "1.3.6.1.4.1.3902.1015.3.1.13.1.2",
		"dist_1082":  "1.3.6.1.4.1.3902.1082.500.10.2.3.10.1.2",
		"onu_rx_alt": "1.3.6.1.4.1.3902.1082.500.10.2.3.35.1.2",
		"ifname":     "1.3.6.1.2.1.2.2.1.2",
		// Kandidat trafik PER-ONU — delta-test 2026-08-30: kolom 2
		// (oct_b) = upstream bytes (delta per-ONU ≈ hc_in port).
		// Kolom 3-6 diwalk terpisah (walk penuh tabel >30dtk = 502
		// Caddy); cari kolom downstream yang delta totalnya ≈ hc_out.
		"oct_a":      "1.3.6.1.4.1.3902.1082.500.10.2.3.2.2.1.1",
		"oct_b":      "1.3.6.1.4.1.3902.1082.500.10.2.3.2.2.1.2",
		"oct_c":      "1.3.6.1.4.1.3902.1082.500.10.2.3.2.2.1.3",
		"oct_d":      "1.3.6.1.4.1.3902.1082.500.10.2.3.2.2.1.4",
		"oct_e":      "1.3.6.1.4.1.3902.1082.500.10.2.3.2.2.1.5",
		"oct_f":      "1.3.6.1.4.1.3902.1082.500.10.2.3.2.2.1.6",
		"v22_onu_rx": "1.3.6.1.4.1.3902.1082.500.20.2.2.2.1.10",
		"v22_onu_tx": "1.3.6.1.4.1.3902.1082.500.20.2.2.2.1.14",
	}
	counts := map[string]int{}
	samples := map[string][]string{}
	errs := map[string]string{}
	for label, oid := range tables {
		if oid == "" {
			continue
		}
		maxSample := 12
		if label == "oct_table" {
			maxSample = 70 // perlu melewati seluruh kolom .1 untuk lihat .2
		}
		n := 0
		list := make([]string, 0, maxSample)
		onuHits := make([]string, 0, 8)
		werr := session.Walk(oid, func(pdu gosnmp.SnmpPDU) error {
			n++
			if len(list) < maxSample || len(onuHits) < 8 {
				name := strings.TrimPrefix(pdu.Name, ".")
				sfx := strings.TrimPrefix(name, strings.TrimPrefix(oid, ".")+".")
				var val string
				switch v := pdu.Value.(type) {
				case []byte:
					val = strings.TrimSpace(string(v))
				default:
					val = gosnmp.ToBigInt(pdu.Value).String()
				}
				if len(val) > 24 {
					val = val[:24]
				}
				if len(list) < 6 {
					list = append(list, sfx+" = "+val)
				}
				// entri interface milik ONU (label memuat "onu") —
				// bukti apakah ifTable mempublish ifIndex per-ONU.
				if strings.Contains(strings.ToLower(val), "onu") && len(onuHits) < 8 {
					onuHits = append(onuHits, sfx+" = "+val)
				}
			}
			return nil
		})
		counts[label] = n
		samples[label] = list
		if len(onuHits) > 0 {
			samples[label+"_onu_hits"] = onuHits
		}
		if werr != nil {
			errs[label] = werr.Error()
		}
	}
	// NOTE: colscan pohon privat & probe ifdescr/ifalias DIHAPUS
	// 2026-08-30 — sudah terjawab (hasil tersimpan di skill
	// references/per-onu-traffic-findings.md) dan membuat walk
	// melebihi timeout curl 110 dtk (d1.txt kosong).
	// Probe trafik: kunci hasil walk ifname = ifIndex port.
	indexProbe := make([]string, 0, 5)
	for _, s := range samples["ifname"] {
		idx := strings.SplitN(s, " ", 2)[0]
		if idx != "" {
			indexProbe = append(indexProbe, idx)
		}
		if len(indexProbe) >= 5 {
			break
		}
	}
	for _, idx := range indexProbe {
		variants := []struct{ label, oid string }{
			{"hc_in", zte.TrafficHCIn},
			{"hc_out", zte.TrafficHCOut},
			{"in32", zte.TrafficInOctets},
			{"out32", zte.TrafficOutOctets},
		}
		probeKey := "probe_" + idx
		list := make([]string, 0, len(variants))
		for _, va := range variants {
			res, perr := session.Get([]string{va.oid + "." + idx})
			if perr != nil {
				list = append(list, va.label+" ERR "+perr.Error())
				continue
			}
			if len(res.Variables) == 0 {
				list = append(list, va.label+" NOVAR")
				continue
			}
			v := res.Variables[0]
			if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance || v.Type == gosnmp.EndOfMibView {
				list = append(list, va.label+" NOSUCH")
				continue
			}
			list = append(list, va.label+" = "+gosnmp.ToBigInt(v.Value).String())
		}
		samples[probeKey] = list
	}
	result["counts"] = counts
	result["samples"] = samples
	result["errors"] = errs
	result["total_ms"] = time.Since(start).Milliseconds()
	writeJSON(response, http.StatusOK, result)
}

// onuDetailCLI: detail konfigurasi ONU via SNMP, fallback CLI.
// Query: ?pon=1/1/1&onu_id=1
func (server *server) onuDetailCLI(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	q := request.URL.Query()
	pon := q.Get("pon")
	onuID, _ := strconv.Atoi(q.Get("onu_id"))
	forceCLI := q.Get("force_cli") == "1" || q.Get("force_cli") == "true"
	if pon == "" || onuID < 1 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Query pon dan onu_id wajib diisi.")
		return
	}
	// Jalur default SNMP cepat (~15s). forceCLI menjalankan 8 perintah CLI
	// berurutan sehingga butuh timeout lebih longgar agar tidak ter-cancel.
	timeout := 15 * time.Second
	if forceCLI {
		timeout = 55 * time.Second
	}
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	defer cancel()
	sample, raws, err := server.olts.GetONUConfigDetail(ctx, tenantID, request.PathValue("oltID"), pon, onuID, forceCLI)
	if err != nil {
		log.Printf("[onuDetailCLI] olt=%s pon=%s onu=%d error=%v", request.PathValue("oltID"), pon, onuID, err)
		oltErrorResponseWithContext(response, err, "onu-detail-cli", request.PathValue("oltID"))
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"sample": sample, "raws": raws})
}

// onuConfigCLI: ubah konfigurasi ONU (name/description/tcont/gemport/service-port)
// via CLI aman + fallback mode command. Timeout 45s; frontend memakai 20s.
// POST /api/v1/olts/{oltID}/onu-config-cli
func (server *server) onuConfigCLI(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 8*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input olt.ONUConfigApplyInput
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Payload konfigurasi ONU tidak valid.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 45*time.Second)
	defer cancel()
	result, err := server.olts.ApplyONUConfigCLI(ctx, tenantID, request.PathValue("oltID"), input)
	if err != nil {
		switch {
		case errors.Is(err, olt.ErrInvalidInput):
			msg := "Parameter konfigurasi ONU tidak valid."
			if err != olt.ErrInvalidInput {
				msg = err.Error()
			}
			writeError(response, http.StatusBadRequest, "invalid_request", msg)
		case errors.Is(err, olt.ErrLocked):
			oltErrorResponseWithContext(response, err, "onu-config-cli", request.PathValue("oltID"))
		case errors.Is(err, olt.ErrForbiddenCmd):
			writeError(response, http.StatusForbidden, "forbidden_command", err.Error())
		case errors.Is(err, ztecli.ErrAuthFailed):
			writeError(response, http.StatusUnauthorized, "cli_auth_failed", "Autentikasi CLI OLT gagal.")
		case errors.Is(err, ztecli.ErrRateLimited):
			writeError(response, http.StatusTooManyRequests, "rate_limited", err.Error())
		default:
			writeError(response, http.StatusBadGateway, "cli_failed", err.Error())
		}
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"status":    "ok",
		"operation": result.Operation,
		"method":    result.Method,
		"executed":  result.Executed,
		"message":   result.Message,
	})
}

// onuTrafficCLI: probe trafik per-ONU lewat SSH/Telnet CLI (hybrid tree).
// Query: ?pon=1/1/1&onu_id=1
func (server *server) onuTrafficCLI(response http.ResponseWriter, request *http.Request) {
	tenantID, ok := server.tenantID(response, request)
	if !ok {
		return
	}
	q := request.URL.Query()
	pon := q.Get("pon")
	onuID, _ := strconv.Atoi(q.Get("onu_id"))
	if pon == "" || onuID < 1 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Query pon dan onu_id wajib diisi.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 60*time.Second)
	defer cancel()
	sample, raws, err := server.olts.ProbeONUTrafficCLI(ctx, tenantID, request.PathValue("oltID"), pon, onuID)
	if err != nil {
		oltErrorResponseWithContext(response, err, "onu-traffic-cli", request.PathValue("oltID"))
		return
	}
	result := map[string]any{
		"sample": sample,
		"raws":   raws,
	}
	writeJSON(response, http.StatusOK, result)
}

// internalONUTrafficCLI: endpoint internal (loopback/private only) untuk
// probe trafik per-ONU via CLI tanpa login web. Query: ?pon=1/1/1&onu_id=1
func (server *server) internalONUTrafficCLI(response http.ResponseWriter, request *http.Request) {
	q := request.URL.Query()
	pon := q.Get("pon")
	onuID, _ := strconv.Atoi(q.Get("onu_id"))
	if pon == "" || onuID < 1 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Query pon dan onu_id wajib diisi.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 60*time.Second)
	defer cancel()
	sample, raws, err := server.olts.ProbeONUTrafficCLI(ctx, "39d83ee8-0ad9-49ff-bd59-a6a3227b041b", request.PathValue("oltID"), pon, onuID)
	if err != nil {
		writeJSON(response, http.StatusOK, map[string]any{"error": err.Error(), "raws": raws})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"sample": sample, "raws": raws})
}

// internalCLIRaw: eksekusi satu command show mentah untuk discovery cepat
// dari terminal server tanpa login web. Query: ?cmd=show+traffic+%3F
func (server *server) internalCLIRaw(response http.ResponseWriter, request *http.Request) {
	cmd := strings.TrimSpace(request.URL.Query().Get("cmd"))
	if cmd == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Query cmd wajib diisi.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 120*time.Second)
	defer cancel()
	out, err := server.olts.ExecuteCLIShow(ctx, "39d83ee8-0ad9-49ff-bd59-a6a3227b041b", request.PathValue("oltID"), cmd)
	if err != nil {
		writeJSON(response, http.StatusOK, map[string]any{"command": cmd, "error": err.Error()})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"command": cmd, "output": out})
}

// internalONUOIDScan: endpoint internal (loopback only) untuk menjelajah OID
// SNMP kandidat satu ONU pada index asli, guna verifikasi firmware tree sebelum
// mengunci OID. Query: ?pon=1/1/1&onu_id=1
func (server *server) internalONUOIDScan(response http.ResponseWriter, request *http.Request) {
	q := request.URL.Query()
	pon := q.Get("pon")
	onuID, _ := strconv.Atoi(q.Get("onu_id"))
	if pon == "" || onuID < 1 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Query pon dan onu_id wajib diisi.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 60*time.Second)
	defer cancel()
	report, err := server.olts.DiscoverONUSNMP(ctx, "39d83ee8-0ad9-49ff-bd59-a6a3227b041b", request.PathValue("oltID"), pon, onuID)
	if err != nil {
		writeJSON(response, http.StatusOK, map[string]any{"error": err.Error(), "pon": pon, "onu_id": onuID})
		return
	}
	writeJSON(response, http.StatusOK, report)
}

// internalOpticalRaw: endpoint internal (loopback only) untuk melihat output
// mentah command optik ZTE. Query: ?pon=1/1/1&onu_id=1
func (server *server) internalOpticalRaw(response http.ResponseWriter, request *http.Request) {
	q := request.URL.Query()
	pon := q.Get("pon")
	onuID, _ := strconv.Atoi(q.Get("onu_id"))
	if pon == "" || onuID < 1 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Query pon dan onu_id wajib diisi.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 120*time.Second)
	defer cancel()
	sample, raws, err := server.olts.ProbeONUOpticalCLI(ctx, "39d83ee8-0ad9-49ff-bd59-a6a3227b041b", request.PathValue("oltID"), pon, onuID)
	if err != nil {
		writeJSON(response, http.StatusOK, map[string]any{"error": err.Error(), "sample": sample, "raws": raws})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"sample": sample, "raws": raws})
}

// internalONUSyncOne: endpoint internal (loopback only) untuk memaksa sync
// satu ONU (status + redaman + jarak) tanpa login web.
// Query: ?pon=1/1/1&onu_id=1
func (server *server) internalONUSyncOne(response http.ResponseWriter, request *http.Request) {
	q := request.URL.Query()
	pon := q.Get("pon")
	onuID, _ := strconv.Atoi(q.Get("onu_id"))
	if pon == "" || onuID < 1 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Query pon dan onu_id wajib diisi.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 120*time.Second)
	defer cancel()
	result, err := server.olts.SyncONULiveByRef(ctx, "39d83ee8-0ad9-49ff-bd59-a6a3227b041b", request.PathValue("oltID"), "", pon, onuID)
	if err != nil {
		writeJSON(response, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "pon": pon, "onu_id": onuID})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"ok": true, "result": result})
}

// internalOpticalTable: endpoint internal (loopback only) untuk melihat hasil
// parse tabel optical-info per PON. Query: ?pon=1/1/1
func (server *server) internalOpticalTable(response http.ResponseWriter, request *http.Request) {
	q := request.URL.Query()
	pon := q.Get("pon")
	if pon == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Query pon wajib diisi.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 120*time.Second)
	defer cancel()
	tbl, err := server.olts.ProbeAllOpticalForPON(ctx, "39d83ee8-0ad9-49ff-bd59-a6a3227b041b", request.PathValue("oltID"), pon)
	if err != nil {
		writeJSON(response, http.StatusOK, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"samples": tbl})
}

// internalSyncOptical: endpoint internal (loopback only) untuk memaksa sync
// nilai optik per-ONU tanpa login web, khusus troubleshooting produksi.
// Dibuat async supaya tidak kena timeout proxy 30 detik.
func (server *server) internalSyncOptical(response http.ResponseWriter, request *http.Request) {
	oltID := request.PathValue("oltID")
	key := "internal/" + oltID
	if _, running := opticalInFlight.LoadOrStore(key, true); running {
		writeJSON(response, http.StatusAccepted, map[string]any{"ok": true, "message": "sync optical internal sudah berjalan"})
		return
	}
	go func() {
		defer opticalInFlight.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
		defer cancel()
		n, err := server.olts.SyncOpticalFromCLI(ctx, "39d83ee8-0ad9-49ff-bd59-a6a3227b041b", oltID)
		if err != nil {
			log.Printf("internalSyncOptical error: updated=%d err=%v", n, err)
			return
		}
		if t, tErr := server.olts.RefreshTraffic(ctx, "39d83ee8-0ad9-49ff-bd59-a6a3227b041b", oltID); tErr != nil {
			log.Printf("internalSyncOptical RefreshTraffic error: %v", tErr)
		} else {
			log.Printf("internalSyncOptical done: optical_updated=%d traffic_updated=%d", n, t)
		}
	}()
	writeJSON(response, http.StatusAccepted, map[string]any{"ok": true, "message": "sync optical internal berjalan di background"})
}

// sessionSysDescr membaca sysDescr singkat untuk deteksi firmware.
func sessionSysDescr(ctx context.Context, session *gosnmp.GoSNMP) string {
	res, err := session.Get([]string{"1.3.6.1.2.1.1.1.0"})
	if err != nil {
		return ""
	}
	for _, v := range res.Variables {
		if b, ok := v.Value.([]byte); ok {
			return string(b)
		}
	}
	return ""
}

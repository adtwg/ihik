package olt

import (
	"context"
	"time"
)

// OnuDailyRow adalah satu titik riwayat harian trafik ONU.
type OnuDailyRow struct {
	Day         string  `json:"day"`
	InBpsPeak   float64 `json:"in_bps_peak"`
	OutBpsPeak  float64 `json:"out_bps_peak"`
	InBytesTot  int64   `json:"in_bytes_total"`
	OutBytesTot int64   `json:"out_bytes_total"`
	Samples     int     `json:"samples"`
}

// OnuStats adalah ringkasan realtime jumlah ONU per status.
type OnuStats struct {
	Total    int64         `json:"total"`
	Working  int64         `json:"working"`
	Los      int64         `json:"los"`
	Offline  int64         `json:"offline"`
	Other    int64         `json:"other"`
	PonPorts []PonPortStat `json:"pon_ports"`
}

// PonPortStat: jumlah ONU per port PON (untuk filter).
type PonPortStat struct {
	Port  string `json:"port"`
	Count int64  `json:"count"`
}

// DailyQuery param untuk grafik harian.
type DailyQuery struct {
	OnuIndex string
	Days     int
}

var _ = time.Now

// GetDaily mengembalikan riwayat harian satu ONU (grafik).
func (service *Service) GetDaily(ctx context.Context, tenantID, oltID, index string, days int) ([]OnuDailyRow, error) {
	if days < 1 || days > 90 {
		days = 30
	}
	return service.repository.GetDaily(ctx, tenantID, oltID, index, days)
}

// GetStats mengembalikan ringkasan jumlah ONU per status + daftar PON port.
func (service *Service) GetStats(ctx context.Context, tenantID, oltID string) (OnuStats, error) {
	return service.repository.GetStats(ctx, tenantID, oltID)
}

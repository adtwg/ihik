"use client";

import { useEffect, useRef, useState } from "react";
import { Activity, ArrowDown, ArrowUp, Pause, Play, RefreshCw, X } from "lucide-react";
import { clientAPI } from "@/lib/api/client";
import type { OLT, ONU } from "@/lib/olts/types";
import { counterRate, formatTraffic, mergeTraffic, type TrafficCounter, type TrafficPoint } from "@/lib/olts/traffic";
import styles from "./onu-traffic-modal.module.css";

const WIDTH = 760;
const HEIGHT = 260;
const LEFT = 82;
const RIGHT = WIDTH - 18;
const TOP = 18;
const BOTTOM = HEIGHT - 32;

function timeLabel(seconds: number) {
  return new Date(seconds * 1000).toLocaleTimeString("id-ID", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function TrafficChart({ points, minutes, now }: { points: TrafficPoint[]; minutes: number; now: number }) {
  const [selectedTime, setSelectedTime] = useState<number | null>(null);
  const maximum = Math.max(1000, ...points.flatMap(point => [point.in_bps, point.out_bps])) * 1.15;
  const start = now - minutes * 60;
  const positionX = (time: number) => LEFT + (time - start) / (minutes * 60) * (RIGHT - LEFT);
  const positionY = (value: number) => BOTTOM - value / maximum * (BOTTOM - TOP);
  const active = points.find(point => point.t === selectedTime) ?? points[points.length - 1];
  const path = (key: "in_bps" | "out_bps") => points.map((point, index) => {
    const gap = index === 0 || point.t - points[index - 1].t > 90;
    return `${gap ? "M" : "L"}${positionX(point.t).toFixed(2)},${positionY(point[key]).toFixed(2)}`;
  }).join(" ");

  return <>
    <div className={styles.chartScroll}>
      <svg className={styles.chart} viewBox={`0 0 ${WIDTH} ${HEIGHT}`} role="img" aria-label="Grafik trafik ONU, unduh dan unggah dalam bit per detik"
        onPointerMove={event => {
          if (!points.length) return;
          const rect = event.currentTarget.getBoundingClientRect();
          const time = start + ((event.clientX - rect.left) / rect.width * WIDTH - LEFT) / (RIGHT - LEFT) * minutes * 60;
          const nearest = points.reduce((best, point) => Math.abs(point.t - time) < Math.abs(best.t - time) ? point : best);
          setSelectedTime(nearest.t);
        }} onPointerLeave={() => setSelectedTime(null)}>
        {[0, .25, .5, .75, 1].map(fraction => <g key={fraction}>
          <line x1={LEFT} x2={RIGHT} y1={positionY(maximum * fraction)} y2={positionY(maximum * fraction)} stroke="#dce7e2" strokeDasharray="3 5" />
          <text x={LEFT - 10} y={positionY(maximum * fraction) + 4} textAnchor="end" fontSize="11" fill="#66786f">{formatTraffic(maximum * fraction)}</text>
        </g>)}
        {[0, .25, .5, .75, 1].map(fraction => <text key={fraction} x={LEFT + fraction * (RIGHT - LEFT)} y={HEIGHT - 9} textAnchor={fraction === 0 ? "start" : fraction === 1 ? "end" : "middle"} fontSize="11" fill="#66786f">{timeLabel(start + fraction * minutes * 60)}</text>)}
        <path d={path("out_bps")} fill="none" stroke="#059669" strokeWidth="2.5" strokeLinejoin="round" />
        <path d={path("in_bps")} fill="none" stroke="#0284c7" strokeWidth="2.5" strokeLinejoin="round" />
        {active && <g>
          <line x1={positionX(active.t)} x2={positionX(active.t)} y1={TOP} y2={BOTTOM} stroke="#8fa49a" strokeDasharray="4 4" />
          <circle cx={positionX(active.t)} cy={positionY(active.out_bps)} r="4" fill="#059669" stroke="white" strokeWidth="2" />
          <circle cx={positionX(active.t)} cy={positionY(active.in_bps)} r="4" fill="#0284c7" stroke="white" strokeWidth="2" />
        </g>}
        {!points.length && <text x={(LEFT + RIGHT) / 2} y={HEIGHT / 2} textAnchor="middle" fontSize="13" fill="#66786f">Belum ada sampel trafik</text>}
      </svg>
    </div>
    <div className={styles.readout}>
      <time>{active ? timeLabel(active.t) : "-"}</time>
      <span className={styles.download}>Unduh <strong>{formatTraffic(active?.out_bps)}</strong></span>
      <span className={styles.upload}>Unggah <strong>{formatTraffic(active?.in_bps)}</strong></span>
    </div>
    {points.length > 1 && <input className={styles.sampleSlider} type="range" aria-label="Pilih sampel waktu trafik" min={0} max={points.length - 1}
      value={active ? points.findIndex(point => point.t === active.t) : 0}
      onChange={event => setSelectedTime(points[Number(event.target.value)].t)} />}
  </>;
}

export function OnuDailyChart({ olt, onu, reference, onClose }: {
  olt: OLT;
  onu: ONU;
  reference: { pon: string; onuID: number } | null;
  onClose: () => void;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [history, setHistory] = useState<TrafficPoint[]>([]);
  const [samples, setSamples] = useState<TrafficPoint[]>([]);
  const [minutes, setMinutes] = useState(15);
  const [mode, setMode] = useState("snmp");
  const [live, setLive] = useState(true);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(false);
  const [probing, setProbing] = useState(false);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const [probeError, setProbeError] = useState<string | null>(null);
  const [source, setSource] = useState("-");
  const [lastRead, setLastRead] = useState<number | null>(null);
  const [now, setNow] = useState(() => Date.now() / 1000);
  const previousCounter = useRef<TrafficCounter | null>(null);
  const pon = reference?.pon;
  const onuID = reference?.onuID;

  useEffect(() => {
    const timer = setInterval(() => {
      if (!document.hidden) setNow(Date.now() / 1000);
    }, 5000);
    return () => clearInterval(timer);
  }, []);

  useEffect(() => {
    const dialog = dialogRef.current;
    const focused = document.activeElement as HTMLElement | null;
    const overflow = document.body.style.overflow;
    dialog?.showModal();
    document.body.style.overflow = "hidden";
    return () => {
      dialog?.close();
      document.body.style.overflow = overflow;
      if (focused?.isConnected) focused.focus();
    };
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setHistoryError(null);
    const query = new URLSearchParams({ index: onu.index, minutes: String(minutes) });
    void clientAPI<{ items: TrafficPoint[] }>(`/api/v1/olts/${olt.id}/onus/intraday?${query}`, { signal: controller.signal, timeoutMs: 10000 })
      .then(result => { if (!controller.signal.aborted) setHistory(result.items ?? []); })
      .catch(error => { if (!controller.signal.aborted) setHistoryError(error instanceof Error ? error.message : "Riwayat trafik tidak tersedia."); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [olt.id, onu.index, minutes, revision]);

  useEffect(() => {
    let disposed = false;
    let request: AbortController | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let failures = 0;
    previousCounter.current = null;
    setLastRead(null);
    setSource("-");
    setProbing(false);
    const probe = async () => {
      if (disposed || request) return;
      if (!pon || !onuID) { setProbeError("Alamat PON/ONU tidak valid."); return; }
      if (document.hidden) { previousCounter.current = null; if (live) timer = setTimeout(probe, 5000); return; }
      const controller = new AbortController();
      request = controller;
      setProbing(true);
      try {
        const query = new URLSearchParams({ pon, onu_id: String(onuID) });
        if (mode === "snmp") query.set("snmp_only", "1");
        const result = await clientAPI<{ sample?: Omit<TrafficCounter, "t"> }>(`/api/v1/olts/${olt.id}/onu-traffic-cli?${query}`, { signal: controller.signal, timeoutMs: 14000 });
        if (disposed || controller.signal.aborted) return;
        const sample = result.sample;
        if (!sample || !Number.isSafeInteger(sample.in_octets) || !Number.isSafeInteger(sample.out_octets) || sample.in_octets < 0 || sample.out_octets < 0) throw new Error("Counter trafik tidak valid atau tidak tersedia.");
        const current = { ...sample, t: Date.now() / 1000 };
        const point = counterRate(previousCounter.current, current);
        previousCounter.current = current;
        if (point) setSamples(rows => mergeTraffic(rows, [point], current.t - 10800).slice(-2160));
        setLastRead(current.t);
        setNow(current.t);
        setSource(sample.method?.startsWith("snmp") ? "SNMP" : sample.method === "cli" ? "CLI" : "Perangkat");
        setProbeError(null);
        failures = 0;
      } catch (error) {
        if (!disposed) {
          previousCounter.current = null;
          failures += 1;
          setProbeError(error instanceof Error ? error.message : "Trafik live tidak tersedia.");
        }
      } finally {
        request = null;
        if (!disposed) {
          setProbing(false);
          if (live) timer = setTimeout(probe, failures ? Math.min(30000, failures * 10000) : mode === "snmp" ? 5000 : 15000);
        }
      }
    };
    if (live) void probe();
    return () => { disposed = true; clearTimeout(timer); request?.abort(); };
  }, [olt.id, onu.index, pon, onuID, mode, live, revision]);

  const points = mergeTraffic(history, samples, now - minutes * 60).filter(point => point.t <= now);
  const latest = samples[samples.length - 1];
  const fresh = live && !probeError && latest && lastRead === latest.t && now - latest.t < 30;
  const peakDownload = points.length ? Math.max(...points.map(point => point.out_bps)) : undefined;
  const peakUpload = points.length ? Math.max(...points.map(point => point.in_bps)) : undefined;

  return <dialog ref={dialogRef} className={styles.dialog} aria-labelledby="onu-traffic-title" onCancel={onClose}
    onKeyDown={event => { if (event.key === "Escape") { event.preventDefault(); onClose(); } }}
    onClick={event => { if (event.target === event.currentTarget) { const box = event.currentTarget.getBoundingClientRect(); if (event.clientX < box.left || event.clientX > box.right || event.clientY < box.top || event.clientY > box.bottom) onClose(); } }}>
    <header className={styles.header}>
      <div className={styles.identity}><Activity size={20} /><div><h2 id="onu-traffic-title">Trafik ONU</h2><p>{onu.name || onu.serial_number || onu.index}</p></div></div>
      <button type="button" className={styles.iconButton} title="Tutup trafik" aria-label="Tutup trafik" onClick={onClose}><X size={20} /></button>
    </header>
    <div className={styles.content}>
      <div className={styles.meta}><span>{olt.name}</span><span className={styles.mono}>{onu.onu_number || onu.index}</span><span>{onu.serial_number || "-"}</span></div>
      <div className={styles.toolbar}>
        <div className={styles.segments} role="group" aria-label="Rentang waktu trafik">{[5, 15, 60, 180].map(value => <button type="button" key={value} aria-pressed={minutes === value} onClick={() => { setMinutes(value); setNow(Date.now() / 1000); }}>{value < 60 ? `${value} menit` : `${value / 60} jam`}</button>)}</div>
        <div className={styles.controls}>
          <select aria-label="Sumber probe trafik" value={mode} onChange={event => setMode(event.target.value)}><option value="snmp">SNMP</option><option value="hybrid">SNMP + fallback CLI</option></select>
          <button type="button" className={styles.iconButton} aria-label={live ? "Jeda trafik" : "Lanjutkan trafik"} title={live ? "Jeda trafik" : "Lanjutkan trafik"} onClick={() => setLive(value => !value)}>{live ? <Pause size={16} /> : <Play size={16} />}</button>
          <button type="button" className={styles.iconButton} aria-label="Refresh trafik" title="Refresh trafik" disabled={probing || loading} onClick={() => { setRevision(value => value + 1); setNow(Date.now() / 1000); }}><RefreshCw size={16} className={probing || loading ? styles.spinning : ""} /></button>
        </div>
      </div>
      <div className={styles.metrics}>
        <div className={styles.download}><span><ArrowDown size={16} /> Unduh</span><strong>{formatTraffic(fresh ? latest.out_bps : undefined)}</strong><small>Puncak {formatTraffic(peakDownload)}</small></div>
        <div className={styles.upload}><span><ArrowUp size={16} /> Unggah</span><strong>{formatTraffic(fresh ? latest.in_bps : undefined)}</strong><small>Puncak {formatTraffic(peakUpload)}</small></div>
        <div className={styles.session}><span className={styles.state}><i data-live={Boolean(fresh)} />{!live ? "Dijeda" : probeError ? "Live tidak tersedia" : fresh ? "Live" : "Menunggu dua sampel"}</span><small>Sumber {source} / riwayat DB</small><small>{lastRead ? `Terbaca ${timeLabel(lastRead)}` : "Belum ada pembacaan live"}</small></div>
      </div>
      {(probeError || historyError) && <div className={styles.error} role="status">{probeError && <p>Live: {probeError}</p>}{historyError && <p>Riwayat: {historyError}</p>}</div>}
      <div className={styles.chartHeading}><h3>Kecepatan transfer</h3><span>{loading ? "Memuat riwayat..." : `${points.length} sampel`} / {minutes} menit</span></div>
      <TrafficChart points={points} minutes={minutes} now={now} />
      <footer className={styles.footer}><span>Unduh: OLT Tx / Unggah: OLT Rx</span><span>{mode === "snmp" ? "Interval live 5 detik" : "Interval live 15 detik"}</span></footer>
    </div>
  </dialog>;
}
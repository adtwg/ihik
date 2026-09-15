"use client";

// Panel detail trafik + konfigurasi per-ONU via CLI (expand row).

import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Activity, Pencil, Plus, RefreshCw, Settings, Trash2, X } from "lucide-react";
import { clientAPI } from "@/lib/api/client";
import type {
  OLT,
  ONU,
  ONUConfigDetail,
  ONUConfigApplyInput,
  ONUConfigApplyResponse,
} from "@/lib/olts/types";
import { normalizeStatusKey, ONLINE_STATUS } from "@/lib/olts/status";

type BpsPoint = { t: number; in_bps: number; out_bps: number };
type CLISample = { in_octets: number; out_octets: number };
type CLIResp = { sample?: CLISample };
type CounterPoint = { t: number; in_octets: number; out_octets: number };
type DetailResp = { sample?: ONUConfigDetail; raws?: Record<string, string> };
type ConfigMode = "edit" | "add";
type ConfigTarget = "name" | "description" | "tcont" | "gemport" | "service_port" | "wan_ip" | "auto_config";
type ServicePortModeInput = "tagged" | "untagged" | "double_vlan" | "hybrid";
type EtherTypeInput = "all" | "pppoe" | "ipoe";
type WanModeInput = "pppoe" | "ipoe" | "static";
type WanAuthModeInput = "auto" | "pap" | "chap";
type ServicePortRow = NonNullable<ONUConfigDetail["service_ports"]>[number];
type WanIPRow = NonNullable<ONUConfigDetail["wan_ips"]>[number];

function fmtBps(v?: number): string {
  if (!v) return "—";
  if (v >= 1e9) return `${(v / 1e9).toFixed(2)} Gbps`;
  if (v >= 1e6) return `${(v / 1e6).toFixed(1)} Mbps`;
  if (v >= 1e3) return `${(v / 1e3).toFixed(0)} Kbps`;
  return `${Math.round(v)} bps`;
}

function fmt(v: number): string {
  if (v >= 1e9) return `${(v / 1e9).toFixed(1)}G`;
  if (v >= 1e6) return `${(v / 1e6).toFixed(1)}M`;
  if (v >= 1e3) return `${(v / 1e3).toFixed(0)}K`;
  return `${Math.round(v)}`;
}

function fmtDbm(v?: number): string {
  if (!v) return "—";
  return `${v.toFixed(2)} dBm`;
}

function hhmm(unixSec: number): string {
  const d = new Date(unixSec * 1000);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

function joinList(values?: Array<string | number>): string {
  if (!values || values.length === 0) return "—";
  return values.join(", ");
}

function serviceModeLabel(mode?: string): string {
  const v = (mode || "").trim().toLowerCase();
  if (v === "tagged" || v === "tag") return "Tagged";
  if (v === "untagged" || v === "untag") return "Untagged";
  if (v === "double_vlan" || v === "double-vlan" || v === "double vlan" || v === "double") return "Double VLAN";
  if (v === "hybrid") return "Hybrid";
  return mode?.trim() || "Tagged";
}

function normalizeServiceMode(mode?: string): ServicePortModeInput {
  const v = (mode || "").trim().toLowerCase();
  if (v.includes("untag")) return "untagged";
  if (v.includes("double")) return "double_vlan";
  if (v.includes("hybrid")) return "hybrid";
  return "tagged";
}

function etherTypeLabel(etype?: string): string {
  const v = (etype || "").trim().toLowerCase();
  if (v === "pppoe") return "PPPoE";
  if (v === "ipoe") return "IPoE";
  return "All Ether Type";
}

function normalizeEtherType(etype?: string): EtherTypeInput {
  const v = (etype || "").trim().toLowerCase();
  if (v.includes("pppoe")) return "pppoe";
  if (v.includes("ipoe")) return "ipoe";
  return "all";
}

function wanModeLabel(mode?: string): string {
  const v = (mode || "").trim().toLowerCase();
  if (v === "pppoe") return "PPPoE";
  if (v === "ipoe" || v === "dhcp" || v === "dynamic") return "IPoE";
  if (v === "static") return "Static IP";
  return mode?.trim() || "-";
}

function normalizeWanMode(mode?: string): WanModeInput {
  const v = (mode || "").trim().toLowerCase();
  if (v.includes("static")) return "static";
  if (v.includes("ipoe") || v === "dhcp" || v === "dynamic") return "ipoe";
  return "pppoe";
}

function normalizeWanAuthMode(auth?: string): WanAuthModeInput {
  const v = (auth || "").trim().toLowerCase();
  if (v === "pap") return "pap";
  if (v === "chap") return "chap";
  return "auto";
}

function configStatusLabel(status?: string): string {
  const v = (status || "").trim();
  if (v === "—") return "—";
  const vl = v.toLowerCase();
  if (vl === "configured") return "Configured";
  if (vl === "partial") return "Partial";
  if (vl === "unconfigured") return "Belum Config";
  return v || "-";
}

function configAccessLabel(access?: string): string {
  const v = (access || "").trim();
  if (v === "—") return "—";
  const vl = v.toLowerCase();
  if (vl === "pppoe") return "PPPoE";
  if (vl === "ipoe") return "IPoE";
  if (vl === "bridge") return "Bridge";
  return v || "Unknown";
}

function resolveOnuRef(onu: ONU): { pon: string; onuID: number } | null {
  const label = (onu.onu_number || "").trim();
  const m = label.match(/^(\d+\/\d+\/\d+):(\d+)$/);
  if (m) {
    return { pon: m[1], onuID: Number(m[2]) };
  }

  const idx = (onu.index || "").trim();
  const parts = idx.split(".");
  if (parts.length >= 2) {
    const base = Number(parts[0]);
    const onuID = Number(parts[parts.length - 1]);
    if (Number.isFinite(base) && Number.isFinite(onuID) && base > 0xffff && onuID > 0) {
      // Encoding ZTE .1082: 0x11 | shelf | slot | pon (slot di bits 8-15).
      const shelf = Math.floor(base / 65536) % 256;
      let slot = Math.floor(base / 256) % 256;
      let port = base % 256;
      if (port === 0) {
        slot = Math.floor(base / 65536) % 256;
        port = Math.floor(base / 256) % 256;
      }
      if (shelf > 0 && slot > 0 && port > 0) {
        return { pon: `${shelf}/${slot}/${port}`, onuID };
      }
    }
  }
  const raw = idx.toLowerCase();
  const mm = raw.match(/gpon-onu_(\d+\/\d+\/\d+):(\d+)/);
  if (mm) return { pon: mm[1], onuID: Number(mm[2]) };
  return null;
}

const W = 560;
const H = 140;
const PAD_L = 40;
const PAD_B = 18;
const PAD_T = 8;

function Sparkline({ points, color }: { points: { x: number; y: number }[]; color: string }) {
  if (points.length < 2) return null;
  const d = points.map((p, i) => `${i === 0 ? "M" : "L"}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ");
  const area = `${d} L${points[points.length - 1].x.toFixed(1)},${(H - PAD_B).toFixed(1)} L${points[0].x.toFixed(1)},${(H - PAD_B).toFixed(1)} Z`;
  return (
    <>
      <path d={area} fill={color} opacity={0.07} />
      <path d={d} fill="none" stroke={color} strokeWidth="1.5" />
    </>
  );
}

export function OnuTrafficDetail({
  olt,
  onu,
  live,
  onLiveDetail,
}: {
  olt: OLT;
  onu: ONU;
  live: boolean;
  onLiveDetail?: (patch: Partial<ONU>) => void;
}) {
  const [series, setSeries] = useState<BpsPoint[]>([]);
  const [lastCounter, setLastCounter] = useState<CounterPoint | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [detail, setDetail] = useState<ONUConfigDetail | null>(null);
  const [detailEnriching, setDetailEnriching] = useState(false);
  const [configBusy, setConfigBusy] = useState<string | null>(null);
  const [configMessage, setConfigMessage] = useState<{ text: string; error?: boolean } | null>(null);
  const [configModalOpen, setConfigModalOpen] = useState(false);
  const [configMode, setConfigMode] = useState<ConfigMode>("edit");
  const [configTarget, setConfigTarget] = useState<ConfigTarget>("name");
  const [configEditorOpen, setConfigEditorOpen] = useState(false);

  const [nameDraft, setNameDraft] = useState("");
  const [descDraft, setDescDraft] = useState("");
  const [tcontID, setTcontID] = useState("1");
  const [tcontName, setTcontName] = useState("");
  const [tcontProfile, setTcontProfile] = useState("DBA-100M");
  const [gemportID, setGemportID] = useState("1");
  const [upProfile, setUpProfile] = useState("UP-100M");
  const [downProfile, setDownProfile] = useState("DW-100M");
  const [servicePortID, setServicePortID] = useState("1");
  const [vport, setVport] = useState("1");
  const [servicePortMode, setServicePortMode] = useState<ServicePortModeInput>("tagged");
  const [serviceDescDraft, setServiceDescDraft] = useState("");
  const [userVlan, setUserVlan] = useState("100");
  const [userSVlan, setUserSVlan] = useState("");
  const [vlan, setVlan] = useState("100");
  const [cTagCos, setCTagCos] = useState("");
  const [svlan, setSvlan] = useState("100");
  const [sTagCos, setSTagCos] = useState("");
  const [etherType, setEtherType] = useState<EtherTypeInput>("all");
  const [editingServicePort, setEditingServicePort] = useState<ServicePortRow | null>(null);
  const [wanIPID, setWanIPID] = useState("1");
  const [wanMode, setWanMode] = useState<WanModeInput>("pppoe");
  const [wanVlanProfile, setWanVlanProfile] = useState("");
  const [wanIpProfile, setWanIpProfile] = useState("");
  const [wanStaticIP, setWanStaticIP] = useState("");
  const [wanAuthMode, setWanAuthMode] = useState<WanAuthModeInput>("auto");
  const [wanPPPoEUsername, setWanPPPoEUsername] = useState("");
  const [wanPPPoEPassword, setWanPPPoEPassword] = useState("");
  const [wanRespondPing, setWanRespondPing] = useState(true);
  const [wanRespondTraceroute, setWanRespondTraceroute] = useState(true);
  const [editingWanIP, setEditingWanIP] = useState<WanIPRow | null>(null);

  // Dep by value (bukan identitas objek onu) agar patch baris tidak memicu refetch.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const ref = useMemo(() => resolveOnuRef(onu), [onu.index, onu.onu_number]);

  const loadTraffic = useCallback(async (silent = false) => {
    if (!ref) {
      setError("Format index ONU tidak dikenali untuk probe CLI.");
      return;
    }
    if (!silent) setLoading(true);
    setError(null);
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 15000);
    try {
      const q = new URLSearchParams({ pon: ref.pon, onu_id: String(ref.onuID) });
      const res = await clientAPI<CLIResp>(`/api/v1/olts/${olt.id}/onu-traffic-cli?${q.toString()}`, { signal: controller.signal });
      const sample = res.sample;
      if (!sample) throw new Error("Counter CLI kosong.");

      const now = Math.floor(Date.now() / 1000);
      setLastCounter((prev) => {
        if (prev && now > prev.t) {
          const inDelta = sample.in_octets >= prev.in_octets ? sample.in_octets - prev.in_octets : 0;
          const outDelta = sample.out_octets >= prev.out_octets ? sample.out_octets - prev.out_octets : 0;
          const secs = Math.max(1, now - prev.t);
          const inBps = (inDelta * 8) / secs;
          const outBps = (outDelta * 8) / secs;
          setSeries((rows) => [...rows, { t: now, in_bps: inBps, out_bps: outBps }].slice(-120));
        }
        return { t: now, in_octets: sample.in_octets, out_octets: sample.out_octets };
      });
    } catch (e) {
      const isAbort = e instanceof Error && (e.name === "AbortError" || /aborted/i.test(e.message));
      if (isAbort && (controller.signal as AbortSignal).aborted) {
        setError("Timeout membaca trafik ONU. OLT sedang sibuk, coba lagi sebentar.");
      } else if (e && typeof e === "object" && "message" in e) {
        setError(String((e as Error).message));
      } else {
        setError("Gagal memuat trafik CLI.");
      }
    } finally {
      clearTimeout(timer);
      if (!silent) setLoading(false);
    }
  }, [olt.id, ref]);

  // Batas auto-refresh menunggu deep-config background (hindari loop bila CLI gagal terus).
  const configRefreshLeft = useRef(3);

  const loadDetail = useCallback(async (forceCLI = false, silent = false) => {
    if (!ref) return;
    if (!silent) setDetailEnriching(true);
    const controller = new AbortController();
    const timeoutMs = forceCLI ? 50000 : 12000;
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const q = new URLSearchParams({ pon: ref.pon, onu_id: String(ref.onuID) });
      // Index asli baris = otoritatif di backend (kebal label onu_number basi).
      if (onu.index) q.set("index", onu.index);
      if (forceCLI) q.set("force_cli", "1");
      const res = await clientAPI<DetailResp>(`/api/v1/olts/${olt.id}/onu-detail-cli?${q.toString()}`, { signal: controller.signal });
      const sample = res.sample ?? null;
      setDetail(sample);
      if (sample) {
        setNameDraft((prev) => prev || sample.name || onu.name || "");
        setDescDraft((prev) => prev || sample.description || onu.description || "");
        const patchedStatus = sample.status || onu.status;
        const statusKey = normalizeStatusKey(patchedStatus);
        const showOptical = ONLINE_STATUS.has(statusKey);
        // Fallback ke nilai baris lama: pembacaan optical per-ONU bisa kosong,
        // jangan menimpa rx/tx tabel dengan 0 saat ONU online.
        const rxVal = typeof sample.rx_onu_side_dbm === "number" && sample.rx_onu_side_dbm !== 0 ? sample.rx_onu_side_dbm : onu.rx_power_dbm;
        const txVal = typeof sample.tx_onu_side_dbm === "number" && sample.tx_onu_side_dbm !== 0 ? sample.tx_onu_side_dbm : onu.tx_power_dbm;
        onLiveDetail?.({
          status: normalizeStatusKey(patchedStatus),
          name: sample.name || onu.name,
          serial_number: sample.serial_number || onu.serial_number,
          distance_m: typeof sample.distance_m === "number" && sample.distance_m !== 0 ? sample.distance_m : onu.distance_m,
          rx_power_dbm: showOptical ? rxVal : 0,
          tx_power_dbm: showOptical ? txVal : 0,
        });
        // Deep-config sedang dimuat di background: refresh senyap (tanpa
        // spinner) agar tombol tidak terlihat "ngeklik sendiri".
        if (res.raws?.config_loading === "1" && !forceCLI && configRefreshLeft.current > 0) {
          configRefreshLeft.current -= 1;
          window.setTimeout(() => void loadDetail(false, true), 20000);
        }
      }
    } catch {
      // Silent: data dasar sudah ada dari props onu. Enrichment optional.
    } finally {
      clearTimeout(timer);
      if (!silent) setDetailEnriching(false);
    }
  }, [olt.id, onu.description, onu.distance_m, onu.index, onu.name, onu.rx_power_dbm, onu.serial_number, onu.status, onu.tx_power_dbm, onLiveDetail, ref]);

  // Fetch awal SEKALI per ONU — patch baris (nama/rx) mengubah identitas prop
  // onu dan pernah memicu refetch beruntun tanpa henti.
  const detailFetchedFor = useRef<string | null>(null);
  useEffect(() => {
    if (detailFetchedFor.current === onu.index) return;
    detailFetchedFor.current = onu.index;
    const id = window.setTimeout(() => void loadDetail(), 50);
    return () => window.clearTimeout(id);
  }, [loadDetail, onu.index]);

  // Anti-stack: satu request trafik berjalan pada satu waktu per panel.
  const trafficBusyRef = useRef(false);

  useEffect(() => {
    // Trafik jalan otomatis selama panel terbuka; berhenti saat tab tersembunyi
    // dan tidak menumpuk request bila OLT lambat merespons.
    const tick = () => {
      if (document.hidden || trafficBusyRef.current) return;
      trafficBusyRef.current = true;
      void loadTraffic(true).finally(() => { trafficBusyRef.current = false; });
    };
    tick();
    const timer = setInterval(tick, live ? 5000 : 10000);
    return () => clearInterval(timer);
  }, [live, loadTraffic]);

  const isListTarget = useCallback((target: ConfigTarget) => (
    target === "tcont" || target === "gemport" || target === "service_port" || target === "wan_ip"
  ), []);

  const openConfigModal = useCallback((mode: ConfigMode = "edit", target: ConfigTarget = "name") => {
    setConfigMode(mode);
    setConfigTarget(target);
    if (target !== "service_port" || mode === "add") setEditingServicePort(null);
    if (target !== "wan_ip" || mode === "add") setEditingWanIP(null);
    setConfigEditorOpen(!isListTarget(target));
    setConfigMessage(null);
    setConfigBusy(null);
    setConfigModalOpen(true);
  }, [isListTarget]);

  const applyConfig = useCallback(async (payload: ONUConfigApplyInput, busyKey: string) => {
    setConfigBusy(busyKey);
    setConfigMessage(null);
    const controller = new AbortController();
    // Backend butuh sampai 40s utk wan-ip (banyak kandidat sintaks + verifikasi).
    const timer = setTimeout(() => controller.abort(), 48000);
    try {
      const res = await clientAPI<ONUConfigApplyResponse>(`/api/v1/olts/${olt.id}/onu-config-cli`, {
        method: "POST",
        body: JSON.stringify(payload),
        signal: controller.signal,
      });
      const method = res.method ? ` via ${String(res.method).toUpperCase()}` : "";
      setConfigMessage({ text: (res.message || `Konfigurasi ${res.operation} berhasil`) + method });
      await loadDetail();
    } catch (e) {
      setConfigMessage({ text: e instanceof Error ? e.message : "Gagal update konfigurasi ONU.", error: true });
    } finally {
      setConfigBusy(null);
    }
  }, [loadDetail, olt.id]);

  const doSetName = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    if (!ref) return;
    const name = nameDraft.trim();
    if (!name) return;
    await applyConfig({ pon: ref.pon, onu_id: ref.onuID, operation: "set_name", name }, "set_name");
  }, [applyConfig, nameDraft, ref]);

  const doSetDescription = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    if (!ref) return;
    const description = descDraft.trim();
    if (!description) return;
    await applyConfig({ pon: ref.pon, onu_id: ref.onuID, operation: "set_description", description }, "set_description");
  }, [applyConfig, descDraft, ref]);

  const doSetTcont = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    if (!ref) return;
    const id = Number(tcontID || "0");
    const profile = tcontProfile.trim();
    if (!id || !profile) {
      setConfigMessage({ text: "ID dan Bandwidth Profile T-CONT wajib diisi.", error: true });
      return;
    }
    await applyConfig({
      pon: ref.pon,
      onu_id: ref.onuID,
      operation: "set_tcont",
      tcont_id: id,
      tcont_name: tcontName.trim() || undefined,
      tcont_profile: profile,
    }, "set_tcont");
  }, [applyConfig, ref, tcontID, tcontName, tcontProfile]);

  const doSetGemport = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    if (!ref) return;
    await applyConfig({
      pon: ref.pon,
      onu_id: ref.onuID,
      operation: "set_gemport",
      gemport_id: Number(gemportID || "0"),
      upstream_profile: upProfile.trim(),
      downstream_profile: downProfile.trim(),
    }, "set_gemport");
  }, [applyConfig, downProfile, gemportID, ref, upProfile]);

  const doSetServicePort = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    if (!ref) return;
    const servicePort = Number(servicePortID || "0");
    const vPort = Number(vport || "0");
    const uv = Number(userVlan || "0");
    const usv = Number(userSVlan || "0");
    const hasUserSVInput = userSVlan.trim() !== "";
    const cv = Number(vlan || "0");
    const cCosText = cTagCos.trim();
    const cCos = cCosText === "" ? undefined : Number(cCosText);
    const hasSVInput = svlan.trim() !== "";
    const sv = Number(svlan || String(cv) || "0");
    const sCosText = sTagCos.trim();
    const sCos = sCosText === "" ? undefined : Number(sCosText);
    const desc = serviceDescDraft.trim();

    if (!servicePort || !vPort || !uv || !cv) {
      setConfigMessage({ text: "Service Port, vPort, User VID, dan C-VID wajib diisi.", error: true });
      return;
    }
    if (servicePort < 1 || servicePort > 4095) {
      setConfigMessage({ text: "Service Port harus 1-4095.", error: true });
      return;
    }
    if (vPort < 1 || vPort > 32) {
      setConfigMessage({ text: "vPort harus 1-32.", error: true });
      return;
    }
    if (uv < 1 || uv > 4094 || cv < 1 || cv > 4094) {
      setConfigMessage({ text: "User VID dan C-VID harus 1-4094.", error: true });
      return;
    }
    if (usv && (usv < 1 || usv > 4094)) {
      setConfigMessage({ text: "User S-VID harus 1-4094.", error: true });
      return;
    }
    if (sv < 1 || sv > 4094) {
      setConfigMessage({ text: "S-VID harus 1-4094.", error: true });
      return;
    }
    if (servicePortMode === "double_vlan" && !hasUserSVInput && !hasSVInput) {
      setConfigMessage({ text: "Mode Double VLAN butuh User S-VID atau S-VID.", error: true });
      return;
    }
    if (cCos != null && (!Number.isFinite(cCos) || cCos < 0 || cCos > 7)) {
      setConfigMessage({ text: "C-Tag CoS harus 0-7.", error: true });
      return;
    }
    if (sCos != null && (!Number.isFinite(sCos) || sCos < 0 || sCos > 7)) {
      setConfigMessage({ text: "S-Tag CoS harus 0-7.", error: true });
      return;
    }

    const descOnlyUpdate = (() => {
      if (configMode !== "edit" || !editingServicePort) return false;
      if (editingServicePort.id !== servicePort) return false;
      if (!desc) return false;

      const sameVPort = vPort === ((editingServicePort.vport && editingServicePort.vport > 0) ? editingServicePort.vport : 1);
      const sameUserVlan = uv === (editingServicePort.user_vlan || 0);
      const sameCVid = cv === (editingServicePort.vlan || 0);
      const sameSVid = sv === (editingServicePort.svlan || editingServicePort.vlan || 0);
      const sameUserSVid = usv === (editingServicePort.user_svlan || 0);
      const sameCCos = (cCos ?? null) === (editingServicePort.c_tag_cos ?? null);
      const sameSCos = (sCos ?? null) === (editingServicePort.s_tag_cos ?? null);
      const sameMode = servicePortMode === normalizeServiceMode(editingServicePort.mode);
      const sameEther = etherType === normalizeEtherType(editingServicePort.ether_type);
      const previousDesc = (editingServicePort.description || "").trim();
      const descChanged = previousDesc !== desc;

      return sameVPort && sameUserVlan && sameCVid && sameSVid && sameUserSVid && sameCCos && sameSCos && sameMode && sameEther && descChanged;
    })();

    if (descOnlyUpdate) {
      await applyConfig({
        pon: ref.pon,
        onu_id: ref.onuID,
        operation: "set_service_port_description",
        service_port_id: servicePort,
        service_description: desc,
      }, "set_service_port");
      return;
    }

    await applyConfig({
      pon: ref.pon,
      onu_id: ref.onuID,
      operation: "set_service_port",
      service_port_id: servicePort,
      vport: vPort,
      service_port_mode: servicePortMode,
      service_description: desc || undefined,
      user_vlan: uv,
      user_svlan: usv || undefined,
      vlan: cv,
      c_vid: cv,
      c_tag_cos: cCos,
      svlan: sv,
      s_vid: sv,
      s_tag_cos: sCos,
      ether_type: etherType,
      apply_via: "auto",
    }, "set_service_port");
  }, [
    applyConfig,
    cTagCos,
    configMode,
    editingServicePort,
    etherType,
    ref,
    sTagCos,
    serviceDescDraft,
    servicePortID,
    servicePortMode,
    svlan,
    userSVlan,
    userVlan,
    vlan,
    vport,
  ]);

  const doDeleteServicePort = useCallback(async (idInput?: number) => {
    if (!ref) return;
    const id = idInput && idInput > 0 ? idInput : Number(servicePortID || "0");
    if (!id) return;
    if (!window.confirm(`Hapus service-port ${id} pada ONU ${onu.onu_number || onu.index}?`)) return;
    await applyConfig({
      pon: ref.pon,
      onu_id: ref.onuID,
      operation: "delete_service_port",
      service_port_id: id,
    }, "delete_service_port");
  }, [applyConfig, onu.index, onu.onu_number, ref, servicePortID]);

  const doDeleteTcont = useCallback(async (id: number) => {
    if (!ref || !id) return;
    if (!window.confirm(`Hapus T-CONT ${id} pada ONU ${onu.onu_number || onu.index}?`)) return;
    await applyConfig({
      pon: ref.pon,
      onu_id: ref.onuID,
      operation: "delete_tcont",
      tcont_id: id,
    }, "delete_tcont");
    await loadDetail();
  }, [applyConfig, loadDetail, onu.index, onu.onu_number, ref]);

  const doDeleteGemport = useCallback(async (id: number) => {
    if (!ref || !id) return;
    if (!window.confirm(`Hapus GEM-Port ${id} pada ONU ${onu.onu_number || onu.index}?`)) return;
    await applyConfig({
      pon: ref.pon,
      onu_id: ref.onuID,
      operation: "delete_gemport",
      gemport_id: id,
    }, "delete_gemport");
    await loadDetail();
  }, [applyConfig, loadDetail, onu.index, onu.onu_number, ref]);

  const doSetWanIP = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    if (!ref) return;
    const id = Number(wanIPID || "0");
    const vlanProfile = wanVlanProfile.trim();
    const ipProfile = wanIpProfile.trim();
    const staticIP = wanStaticIP.trim();
    const username = wanPPPoEUsername.trim();
    const password = wanPPPoEPassword.trim();

    if (!id || id < 1 || id > 8) {
      setConfigMessage({ text: "WAN ID harus 1-8.", error: true });
      return;
    }
    if (!vlanProfile) {
      setConfigMessage({ text: "VLAN Profile wajib diisi.", error: true });
      return;
    }
    if (wanMode === "pppoe" && (!username || !password)) {
      setConfigMessage({ text: "Username & password PPPoE wajib diisi.", error: true });
      return;
    }
    if (wanMode === "static" && !staticIP) {
      setConfigMessage({ text: "Static IP wajib diisi untuk mode Static.", error: true });
      return;
    }

    await applyConfig({
      pon: ref.pon,
      onu_id: ref.onuID,
      operation: "set_wan_ip",
      wan_ip_id: id,
      wan_mode: wanMode,
      wan_auth_mode: wanAuthMode,
      wan_vlan_profile: vlanProfile,
      wan_ip_profile: wanMode !== "pppoe" ? (ipProfile || undefined) : undefined,
      wan_static_ip: wanMode === "static" ? staticIP : undefined,
      wan_pppoe_username: wanMode === "pppoe" ? username : undefined,
      wan_pppoe_password: wanMode === "pppoe" ? password : undefined,
      wan_respond_ping: wanRespondPing,
      wan_respond_traceroute: wanRespondTraceroute,
    }, "set_wan_ip");
  }, [
    applyConfig,
    ref,
    wanAuthMode,
    wanIPID,
    wanIpProfile,
    wanMode,
    wanPPPoEPassword,
    wanPPPoEUsername,
    wanRespondPing,
    wanRespondTraceroute,
    wanStaticIP,
    wanVlanProfile,
  ]);

  const doDeleteWanIP = useCallback(async (idInput?: number) => {
    if (!ref) return;
    const id = idInput && idInput > 0 ? idInput : Number(wanIPID || "0");
    if (!id) return;
    if (!window.confirm(`Hapus WAN IP ${id} pada ONU ${onu.onu_number || onu.index}?`)) return;
    await applyConfig({
      pon: ref.pon,
      onu_id: ref.onuID,
      operation: "delete_wan_ip",
      wan_ip_id: id,
    }, "delete_wan_ip");
    await loadDetail();
  }, [applyConfig, loadDetail, onu.index, onu.onu_number, ref, wanIPID]);

  const doAutoConfigONU = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    if (!ref) return;

    const tID = Number(tcontID || "0");
    const gID = Number(gemportID || "0");
    const spID = Number(servicePortID || "0");
    const vp = Number(vport || "0");
    const uv = Number(userVlan || "0");
    const cv = Number(vlan || "0");
    const sv = Number(svlan || String(cv) || "0");
    const wID = Number(wanIPID || "0");
    const up = upProfile.trim();
    const down = downProfile.trim();
    const tProf = tcontProfile.trim();
    const vlanProfile = wanVlanProfile.trim();
    const ipProf = wanIpProfile.trim();
    const staticIP = wanStaticIP.trim();
    const pppUser = wanPPPoEUsername.trim();
    const pppPass = wanPPPoEPassword.trim();

    if (!tID || !gID || !spID || !vp || !uv || !cv || !wID || !tProf || !vlanProfile) {
      setConfigMessage({ text: "Field wajib auto-config: TCONT/GEMPORT/SERVICE/VLAN/WAN harus terisi.", error: true });
      return;
    }
    if (!up && !down) {
      setConfigMessage({ text: "Minimal isi upstream atau downstream profile GEM-Port.", error: true });
      return;
    }
    if (wanMode === "pppoe" && (!pppUser || !pppPass)) {
      setConfigMessage({ text: "Mode PPPoE butuh username & password.", error: true });
      return;
    }
    if (wanMode === "static" && !staticIP) {
      setConfigMessage({ text: "Mode Static butuh wan_static_ip.", error: true });
      return;
    }

    await applyConfig({
      pon: ref.pon,
      onu_id: ref.onuID,
      operation: "auto_config_onu",
      tcont_id: tID,
      tcont_name: tcontName.trim() || undefined,
      tcont_profile: tProf,
      gemport_id: gID,
      upstream_profile: up || undefined,
      downstream_profile: down || undefined,
      service_port_id: spID,
      vport: vp,
      user_vlan: uv,
      user_svlan: Number(userSVlan || "0") || undefined,
      vlan: cv,
      c_vid: cv,
      svlan: sv,
      s_vid: sv,
      c_tag_cos: cTagCos.trim() === "" ? undefined : Number(cTagCos),
      s_tag_cos: sTagCos.trim() === "" ? undefined : Number(sTagCos),
      service_port_mode: servicePortMode,
      service_description: serviceDescDraft.trim() || undefined,
      ether_type: etherType,
      wan_ip_id: wID,
      wan_mode: wanMode,
      wan_auth_mode: wanAuthMode,
      wan_vlan_profile: vlanProfile,
      wan_ip_profile: wanMode !== "pppoe" ? (ipProf || undefined) : undefined,
      wan_static_ip: wanMode === "static" ? staticIP : undefined,
      wan_pppoe_username: wanMode === "pppoe" ? pppUser : undefined,
      wan_pppoe_password: wanMode === "pppoe" ? pppPass : undefined,
      wan_respond_ping: wanRespondPing,
      wan_respond_traceroute: wanRespondTraceroute,
      apply_via: "auto",
    }, "auto_config_onu");
  }, [
    applyConfig,
    cTagCos,
    downProfile,
    etherType,
    gemportID,
    ref,
    sTagCos,
    serviceDescDraft,
    servicePortID,
    servicePortMode,
    svlan,
    tcontID,
    tcontName,
    tcontProfile,
    upProfile,
    userSVlan,
    userVlan,
    vlan,
    vport,
    wanAuthMode,
    wanIPID,
    wanIpProfile,
    wanMode,
    wanPPPoEPassword,
    wanPPPoEUsername,
    wanRespondPing,
    wanRespondTraceroute,
    wanStaticIP,
    wanVlanProfile,
  ]);

  const pickTcontForEdit = useCallback((id: number, profile?: string, name?: string) => {
    setConfigMode("edit");
    setConfigTarget("tcont");
    setConfigEditorOpen(true);
    setConfigMessage(null);
    setTcontID(String(id));
    setTcontProfile((profile || "").trim());
    setTcontName((name || "").trim());
  }, []);

  const pickGemportForEdit = useCallback((id: number, up?: string, down?: string) => {
    setConfigMode("edit");
    setConfigTarget("gemport");
    setConfigEditorOpen(true);
    setConfigMessage(null);
    setGemportID(String(id));
    setUpProfile((up || "").trim());
    setDownProfile((down || "").trim());
  }, []);

  const pickServicePortForEdit = useCallback((row: ServicePortRow) => {
    setConfigMode("edit");
    setConfigTarget("service_port");
    setConfigEditorOpen(true);
    setConfigMessage(null);
    setEditingServicePort(row);
    setServicePortID(String(row.id));
    setVport(row.vport && row.vport > 0 ? String(row.vport) : "1");
    setServiceDescDraft((row.description || "").trim());
    setUserVlan(row.user_vlan ? String(row.user_vlan) : "");
    setUserSVlan(row.user_svlan ? String(row.user_svlan) : "");
    setVlan(row.vlan ? String(row.vlan) : "");
    setCTagCos(row.c_tag_cos != null ? String(row.c_tag_cos) : "");
    setSvlan(row.svlan ? String(row.svlan) : (row.vlan ? String(row.vlan) : ""));
    setSTagCos(row.s_tag_cos != null ? String(row.s_tag_cos) : "");

    setServicePortMode(normalizeServiceMode(row.mode));
    setEtherType(normalizeEtherType(row.ether_type));
  }, []);

  const pickWanIPForEdit = useCallback((row: WanIPRow) => {
    setConfigMode("edit");
    setConfigTarget("wan_ip");
    setConfigEditorOpen(true);
    setConfigMessage(null);
    setEditingWanIP(row);
    setWanIPID(String(row.id));
    setWanMode(normalizeWanMode(row.mode));
    setWanVlanProfile((row.vlan_profile || "").trim());
    setWanIpProfile((row.ip_profile || "").trim());
    setWanStaticIP((row.static_ip || "").trim());
    setWanAuthMode(normalizeWanAuthMode(row.auth_mode));
    setWanPPPoEUsername((row.pppoe_username || "").trim());
    setWanPPPoEPassword((row.pppoe_password || "").trim());
    setWanRespondPing(row.respond_ping ?? true);
    setWanRespondTraceroute(row.respond_traceroute ?? true);
  }, []);

  const plotW = W - PAD_L - 8;
  const plotH = H - PAD_B - PAD_T;
  const last = series[series.length - 1];

  const points = series.length >= 2 ? (() => {
    const maxV = Math.max(1, ...series.map((r) => Math.max(r.in_bps, r.out_bps)));
    const t0 = series[0].t;
    const t1 = series[series.length - 1].t;
    const span = Math.max(1, t1 - t0);
    const toPts = (key: "in_bps" | "out_bps") =>
      series.map((r) => ({
        x: PAD_L + ((r.t - t0) / span) * plotW,
        y: PAD_T + plotH - (r[key] / maxV) * plotH,
      }));
    return { in: toPts("in_bps"), out: toPts("out_bps"), maxV };
  })() : null;

  const inputCls = "rounded border border-[#d5e2dc] bg-white px-2 py-1 text-xs text-[#1f2f27] focus:border-[#2c7a5b] focus:outline-none";
  const btnCls = "inline-flex items-center justify-center rounded border border-[#d5e2dc] bg-white px-2 py-1 text-xs font-medium text-[#44554d] hover:border-[#2c7a5b] disabled:opacity-50";
  const configTargetLabel: Record<ConfigTarget, string> = {
    name: "Nama ONU",
    description: "Description",
    tcont: "T-CONT",
    gemport: "GEM-Port",
    service_port: "VLAN (Service Port)",
    wan_ip: "WAN IP",
    auto_config: "Auto Config ONU",
  };
  const quickConfigRows: Array<{ label: string; target: ConfigTarget }> = [
    { label: "Nama ONU", target: "name" },
    { label: "Description", target: "description" },
    { label: "T-CONT", target: "tcont" },
    { label: "GEM-Port", target: "gemport" },
    { label: "VLAN (Service Port)", target: "service_port" },
    { label: "WAN IP", target: "wan_ip" },
    { label: "Auto Config ONU", target: "auto_config" },
  ];
  const showListFirst = isListTarget(configTarget);
  const tcontRows = [...(detail?.tconts || [])].sort((a, b) => a.id - b.id);
  const gemportRows = [...(detail?.gemports || [])].sort((a, b) => a.id - b.id);
  const servicePortRows = [...(detail?.service_ports || [])].sort((a, b) => a.id - b.id);
  const wanIPRows = [...(detail?.wan_ips || [])].sort((a, b) => a.id - b.id);
  const wanVlanProfileOptions = [...new Set(wanIPRows.map((row) => (row.vlan_profile || "").trim()).filter(Boolean))].sort((a, b) => a.localeCompare(b));
  const wanIPProfileOptions = [...new Set(wanIPRows.map((row) => (row.ip_profile || "").trim()).filter(Boolean))].sort((a, b) => a.localeCompare(b));
  const nextTcontID = String((tcontRows.reduce((max, row) => Math.max(max, row.id), 0) || 0) + 1);
  const nextGemportID = String((gemportRows.reduce((max, row) => Math.max(max, row.id), 0) || 0) + 1);
  const nextServicePortID = String((servicePortRows.reduce((max, row) => Math.max(max, row.id), 0) || 0) + 1);
  const nextWanIPID = String((wanIPRows.reduce((max, row) => Math.max(max, row.id), 0) || 0) + 1);
  const defaultVPort = String(servicePortRows.find((row) => (row.vport || 0) > 0)?.vport || 1);
  const configState = detail?.config_state;
  const configStateStatus = configStatusLabel(configState?.status || "—");
  const configStateAccess = configAccessLabel(configState?.access || "—");
  const configStateMissing = joinList(configState?.missing || []) || "—";

  return (
    <div className="rounded-lg border border-[#e5eeea] bg-white p-3 shadow-sm">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-3">
          <span className="inline-flex items-center gap-1.5 text-xs text-[#607067]">
            <Activity size={14} className="text-emerald-600" />
            <span className="font-medium text-[#10251d]">Unduh</span>
            <span className="font-mono font-semibold text-emerald-700">{fmtBps(last?.in_bps ?? onu.in_bps)}</span>
          </span>
          <span className="inline-flex items-center gap-1.5 text-xs text-[#607067]">
            <Activity size={14} className="text-sky-600" />
            <span className="font-medium text-[#10251d]">Unggah</span>
            <span className="font-mono font-semibold text-sky-700">{fmtBps(last?.out_bps ?? onu.out_bps)}</span>
          </span>
          <span className="rounded-full border border-[#d8e7df] bg-[#f6faf8] px-2 py-0.5 text-[10px] font-semibold text-[#4f645a]">Sumber: CLI</span>
        </div>
        <div className="flex items-center gap-1.5">
          <button
            className={btnCls}
            onClick={() => void loadDetail(true)}
            disabled={detailEnriching}
            title="Tarik detail konfigurasi via CLI (akan memakan waktu)"
          >
            <RefreshCw size={12} className={detailEnriching ? "animate-spin" : ""} /> Refresh detail
          </button>
          <button
            className={btnCls}
            onClick={() => openConfigModal("edit", "name")}
            title="Buka popup konfigurasi ONU"
          >
            <Settings size={12} /> Konfigurasi
          </button>
        </div>
      </div>

      {error && <div className="mb-2 rounded bg-red-50 px-2 py-1 text-xs text-red-700">{error}</div>}
      {configMessage && (
        <div className={`mb-2 rounded px-2 py-1 text-xs ${configMessage.error ? "bg-red-50 text-red-700" : "bg-emerald-50 text-emerald-700"}`}>
          {configMessage.text}
        </div>
      )}
      

      {points ? (
        <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ maxHeight: 180 }}>
          {[0.25, 0.5, 0.75, 1].map((f) => {
            const y = PAD_T + plotH - f * plotH;
            return (
              <g key={f}>
                <line x1={PAD_L} y1={y} x2={W - 8} y2={y} stroke="#eef4f1" strokeWidth="1" />
                <text x={PAD_L - 4} y={y + 3} textAnchor="end" fontSize="8" fill="#8aa096">{fmt(points.maxV * f)}</text>
              </g>
            );
          })}
          <Sparkline points={points.in} color="#059669" />
          <Sparkline points={points.out} color="#0284c7" />
          {series.map((r, i) =>
            i % Math.max(1, Math.ceil(series.length / 6)) === 0 ? (
              <text key={r.t} x={PAD_L + ((r.t - series[0].t) / (series[series.length - 1].t - series[0].t || 1)) * plotW} y={H - 5} textAnchor="middle" fontSize="8" fill="#8aa096">{hhmm(r.t)}</text>
            ) : null
          )}
        </svg>
      ) : (
        <div className="flex h-24 items-center justify-center rounded bg-[#f6faf8] text-xs text-[#8aa096]">
          Belum ada sampel CLI. Nyalakan <b className="mx-1">Trafik Live</b> atau klik Refresh.
        </div>
      )}

      <div className="mt-3 grid gap-2 text-xs md:grid-cols-2">
        <div className="rounded-lg border border-[#e5eeea] bg-[#f8fbfa] p-2">
          <div className="mb-1 text-[10px] font-bold uppercase tracking-wider text-[#8aa096]">Konfigurasi ONU</div>
          <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-[#44554d]">
            <span className="text-[#8aa096]">PON/ONU</span><span className="font-mono">{detail?.pon_port && detail?.onu_id ? `${detail.pon_port}:${detail.onu_id}` : (ref ? `${ref.pon}:${ref.onuID}` : "—")}</span>
            <span className="text-[#8aa096]">Status</span><span>{detail?.status || onu.status || "—"}</span>
            <span className="text-[#8aa096]">Nama</span><span className="truncate" title={detail?.name}>{detail?.name || onu.name || "—"}</span>
            <span className="text-[#8aa096]">Serial</span><span className="font-mono">{detail?.serial_number || onu.serial_number || "—"}</span>
            <span className="text-[#8aa096]">Status Config</span><span>{configStateStatus}</span>
            <span className="text-[#8aa096]">Mode Akses</span><span>{configStateAccess}</span>
            <span className="text-[#8aa096]">Missing</span><span className="truncate" title={configStateMissing}>{configStateMissing}</span>
            <span className="text-[#8aa096]">RX OLT Side</span><span className="font-mono">{fmtDbm(detail?.rx_olt_side_dbm)}</span>
            <span className="text-[#8aa096]">RX ONU Side</span><span className="font-mono">{fmtDbm(detail?.rx_onu_side_dbm ?? onu.rx_power_dbm)}</span>
            <span className="text-[#8aa096]">TX ONU Side</span><span className="font-mono">{fmtDbm(detail?.tx_onu_side_dbm ?? onu.tx_power_dbm)}</span>
            <span className="text-[#8aa096]">VLAN</span><span>{joinList((detail?.vlans || []).map((x) => typeof x === "string" ? x : String(x)))}</span>
            <span className="text-[#8aa096]">DBA</span><span className="truncate" title={joinList((detail?.dba_profiles || []).map((x) => typeof x === "string" ? x : String(x)))}>{joinList((detail?.dba_profiles || []).map((x) => typeof x === "string" ? x : String(x)))}</span>
          </div>
        </div>

        <div className="rounded-lg border border-[#e5eeea] bg-[#f8fbfa] p-2">
          <div className="mb-1 text-[10px] font-bold uppercase tracking-wider text-[#8aa096]">Profile & Rate</div>
          <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-[#44554d]">
            <span className="text-[#8aa096]">Upstream Profile</span><span className="truncate" title={joinList((detail?.upstream_profiles || []).map((x) => typeof x === "string" ? x : String(x)))}>{joinList((detail?.upstream_profiles || []).map((x) => typeof x === "string" ? x : String(x)))}</span>
            <span className="text-[#8aa096]">Downstream Profile</span><span className="truncate" title={joinList((detail?.downstream_profiles || []).map((x) => typeof x === "string" ? x : String(x)))}>{joinList((detail?.downstream_profiles || []).map((x) => typeof x === "string" ? x : String(x)))}</span>
            <span className="text-[#8aa096]">Upstream Rate</span><span className="font-mono text-sky-700">{fmtBps(detail?.upstream_bps ?? onu.in_bps)}</span>
            <span className="text-[#8aa096]">Downstream Rate</span><span className="font-mono text-emerald-700">{fmtBps(detail?.downstream_bps ?? onu.out_bps)}</span>
            <span className="text-[#8aa096]">Distance</span><span className="font-mono">{detail?.distance_m ? `${Math.round(detail.distance_m)} m` : (onu.distance_m ? `${Math.round(onu.distance_m)} m` : "—")}</span>
            <span className="text-[#8aa096]">Warnings</span><span className="truncate" title={joinList((detail?.warnings || []).map((x) => typeof x === "string" ? x : String(x)))}>{joinList((detail?.warnings || []).map((x) => typeof x === "string" ? x : String(x)))}</span>
          </div>
        </div>
      </div>

      <div className="mt-3 rounded-lg border border-[#e5eeea] bg-white p-2">
        <div className="inline-flex items-center gap-1.5 text-[10px] font-bold uppercase tracking-wider text-[#70847b]">
          <Settings size={12} /> Aksi & Edit Konfigurasi ONU
        </div>
        <div className="mt-2 overflow-hidden rounded-md border border-[#e5eeea] bg-[#f8fbfa]">
          {quickConfigRows.map((item) => (
            <div
              key={item.target}
              className="grid grid-cols-[minmax(0,1fr)_14px_auto] items-center gap-2 border-b border-[#e5eeea] px-2 py-1.5 text-xs last:border-b-0"
            >
              <span className="text-[#44554d]">{item.label}</span>
              <span className="text-[#8aa096]">:</span>
              <button
                type="button"
                className={`${btnCls} h-7 w-7 p-0`}
                onClick={() => openConfigModal("edit", item.target)}
                title={`Edit ${item.label}`}
              >
                <Settings size={14} className="text-sky-600" />
              </button>
            </div>
          ))}
        </div>
        <div className="mt-2 text-[11px] text-[#607067]">
          Klik gear di samping kalimat (contoh: <b>GEM-Port : gear</b>) untuk buka popup. Mode <b>Edit</b> (pensil)
          dan <b>Tambah</b> (plus) tetap ada di dalam popup.
        </div>
      </div>

      {configModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/65 p-2 sm:p-4">
          <button
            type="button"
            className="absolute inset-0"
            aria-label="Tutup popup"
            onClick={() => {
              setConfigModalOpen(false);
              setConfigEditorOpen(false);
              setConfigMessage(null);
              setEditingServicePort(null);
              setEditingWanIP(null);
            }}
          />
          <div className="relative z-10 w-full max-w-3xl overflow-hidden rounded-xl border border-[#dbe6e0] bg-white shadow-2xl">
            <div className="flex items-center justify-between border-b border-[#e5eeea] bg-[#f6faf8] px-3 py-2.5">
              <div>
                <div className="text-sm font-semibold text-[#10251d]">Konfigurasi ONU ke OLT</div>
                <div className="font-mono text-[11px] text-[#607067]">{ref ? `${ref.pon}:${ref.onuID}` : onu.onu_number || onu.index}</div>
                <div className="text-[11px] text-[#607067]">Target: {configTargetLabel[configTarget]}</div>
              </div>
              <div className="flex items-center gap-1.5">
                {showListFirst && (
                  <span className="rounded-full border border-[#d8e7df] bg-[#f6faf8] px-2 py-0.5 text-[10px] font-semibold text-[#4f645a]">
                    {configMode === "add" ? "Mode: Tambah" : "Mode: Edit"}
                  </span>
                )}
                {showListFirst && configEditorOpen && (
                  <button type="button" className={btnCls} onClick={() => setConfigEditorOpen(false)}>
                    Kembali ke List
                  </button>
                )}
                <button type="button" className={btnCls} onClick={() => {
                  setConfigModalOpen(false);
                  setConfigEditorOpen(false);
                  setConfigMessage(null);
                  setEditingServicePort(null);
                  setEditingWanIP(null);
                }} aria-label="Tutup">
                  <X size={12} />
                </button>
              </div>
            </div>

            <div className="max-h-[72vh] overflow-y-auto p-3 sm:p-4">
              {configMessage && (
                <div className={`mb-2 rounded px-2 py-1 text-xs ${configMessage.error ? "bg-red-50 text-red-700" : "bg-emerald-50 text-emerald-700"}`}>
                  {configMessage.text}
                </div>
              )}

              {showListFirst && configTarget === "tcont" && (
                <div className="mb-3 overflow-hidden rounded-md border border-[#e5eeea] bg-[#fbfdfc]">
                  <div className="overflow-x-auto">
                    <table className="min-w-full text-xs">
                      <thead className="bg-[#f1f7f4] text-[#607067]">
                        <tr>
                          <th className="px-2 py-1.5 text-left font-semibold">ID</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Name</th>
                          <th className="px-2 py-1.5 text-left font-semibold">ONU</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Bandwidth Profile</th>
                          <th className="px-2 py-1.5 text-left font-semibold">DBA Gap Mode</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Aksi</th>
                        </tr>
                      </thead>
                      <tbody>
                        {tcontRows.length === 0 ? (
                          <tr>
                            <td className="px-2 py-2 text-[#8aa096]" colSpan={6}>Belum ada data T-CONT.</td>
                          </tr>
                        ) : tcontRows.map((row) => (
                          <tr key={`tcont-${row.id}`} className="border-t border-[#e5eeea]">
                            <td className="px-2 py-1.5 font-mono">{row.id}</td>
                            <td className="px-2 py-1.5">{row.name || "-"}</td>
                            <td className="px-2 py-1.5 font-mono">{detail?.interface || "-"}</td>
                            <td className="px-2 py-1.5">{row.profile || "-"}</td>
                            <td className="px-2 py-1.5">{row.dba_gap_mode || "-"}</td>
                            <td className="px-2 py-1.5">
                              <div className="flex items-center gap-1">
                                <button
                                  type="button"
                                  className={`${btnCls} h-7 w-7 p-0`}
                                  title="Edit"
                                  onClick={() => pickTcontForEdit(row.id, row.profile, row.name)}
                                >
                                  <Pencil size={13} className="text-sky-600" />
                                </button>
                                <button
                                  type="button"
                                  className={`${btnCls} h-7 w-7 border-red-200 p-0 text-red-600 hover:border-red-400`}
                                  title="Hapus"
                                  onClick={() => void doDeleteTcont(row.id)}
                                  disabled={configBusy === "delete_tcont"}
                                >
                                  <Trash2 size={13} />
                                </button>
                              </div>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  <div className="flex items-center justify-end border-t border-[#e5eeea] bg-[#f6faf8] px-2 py-2">
                    <button
                      type="button"
                      className={`${btnCls} border-sky-200 text-sky-700 hover:border-sky-400`}
                      onClick={() => {
                        setConfigMode("add");
                        setConfigEditorOpen(true);
                        setTcontID(nextTcontID);
                        setTcontName("");
                        setTcontProfile("");
                      }}
                    >
                      <Plus size={12} /> Tambah
                    </button>
                  </div>
                </div>
              )}

              {showListFirst && configTarget === "gemport" && (
                <div className="mb-3 overflow-hidden rounded-md border border-[#e5eeea] bg-[#fbfdfc]">
                  <div className="overflow-x-auto">
                    <table className="min-w-full text-xs">
                      <thead className="bg-[#f1f7f4] text-[#607067]">
                        <tr>
                          <th className="px-2 py-1.5 text-left font-semibold">ID</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Upstream</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Downstream</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Aksi</th>
                        </tr>
                      </thead>
                      <tbody>
                        {gemportRows.length === 0 ? (
                          <tr>
                            <td className="px-2 py-2 text-[#8aa096]" colSpan={4}>Belum ada data GEM-Port.</td>
                          </tr>
                        ) : gemportRows.map((row) => (
                          <tr key={`gemport-${row.id}`} className="border-t border-[#e5eeea]">
                            <td className="px-2 py-1.5 font-mono">{row.id}</td>
                            <td className="px-2 py-1.5">{row.upstream_profile || "-"}</td>
                            <td className="px-2 py-1.5">{row.downstream_profile || "-"}</td>
                            <td className="px-2 py-1.5">
                              <div className="flex items-center gap-1">
                                <button
                                  type="button"
                                  className={`${btnCls} h-7 w-7 p-0`}
                                  title="Edit"
                                  onClick={() => pickGemportForEdit(row.id, row.upstream_profile, row.downstream_profile)}
                                >
                                  <Pencil size={13} className="text-sky-600" />
                                </button>
                                <button
                                  type="button"
                                  className={`${btnCls} h-7 w-7 border-red-200 p-0 text-red-600 hover:border-red-400`}
                                  title="Hapus"
                                  onClick={() => void doDeleteGemport(row.id)}
                                  disabled={configBusy === "delete_gemport"}
                                >
                                  <Trash2 size={13} />
                                </button>
                              </div>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  <div className="flex items-center justify-end border-t border-[#e5eeea] bg-[#f6faf8] px-2 py-2">
                    <button
                      type="button"
                      className={`${btnCls} border-sky-200 text-sky-700 hover:border-sky-400`}
                      onClick={() => {
                        setConfigMode("add");
                        setConfigEditorOpen(true);
                        setGemportID(nextGemportID);
                        setUpProfile("");
                        setDownProfile("");
                      }}
                    >
                      <Plus size={12} /> Tambah
                    </button>
                  </div>
                </div>
              )}

              {showListFirst && configTarget === "service_port" && (
                <div className="mb-3 overflow-hidden rounded-md border border-[#e5eeea] bg-[#fbfdfc]">
                  <div className="overflow-x-auto">
                    <table className="min-w-full text-xs">
                      <thead className="bg-[#f1f7f4] text-[#607067]">
                        <tr>
                          <th className="px-2 py-1.5 text-left font-semibold">vPort</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Service Port</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Description</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Mode</th>
                          <th className="px-2 py-1.5 text-left font-semibold">VID/S-VID</th>
                          <th className="px-2 py-1.5 text-left font-semibold">C-VID/C-Cos</th>
                          <th className="px-2 py-1.5 text-left font-semibold">S-VID/S-Cos</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Ether Type</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Aksi</th>
                        </tr>
                      </thead>
                      <tbody>
                        {servicePortRows.length === 0 ? (
                          <tr>
                            <td className="px-2 py-2 text-[#8aa096]" colSpan={9}>Belum ada data Service-Port.</td>
                          </tr>
                        ) : servicePortRows.map((row) => (
                          <tr key={`sp-${row.id}`} className="border-t border-[#e5eeea]">
                            <td className="px-2 py-1.5 font-mono">{row.vport || "-"}</td>
                            <td className="px-2 py-1.5 font-mono">{row.id}</td>
                            <td className="px-2 py-1.5">{row.description || "-"}</td>
                            <td className="px-2 py-1.5">{serviceModeLabel(row.mode)}</td>
                            <td className="px-2 py-1.5 font-mono">{`${row.user_vlan || "--"}/${row.user_svlan || "--"}`}</td>
                            <td className="px-2 py-1.5 font-mono">{`${row.vlan || "--"}/${row.c_tag_cos ?? "--"}`}</td>
                            <td className="px-2 py-1.5 font-mono">{`${row.svlan || "--"}/${row.s_tag_cos ?? "--"}`}</td>
                            <td className="px-2 py-1.5">{etherTypeLabel(row.ether_type)}</td>
                            <td className="px-2 py-1.5">
                              <div className="flex items-center gap-1">
                                <button
                                  type="button"
                                  className={`${btnCls} h-7 w-7 p-0`}
                                  title="Edit"
                                  onClick={() => pickServicePortForEdit(row)}
                                >
                                  <Pencil size={13} className="text-sky-600" />
                                </button>
                                <button
                                  type="button"
                                  className={`${btnCls} h-7 w-7 border-red-200 p-0 text-red-600 hover:border-red-400`}
                                  title="Hapus"
                                  onClick={() => {
                                    pickServicePortForEdit(row);
                                    setConfigMessage(null);
                                    void doDeleteServicePort(row.id);
                                  }}
                                  disabled={configBusy === "delete_service_port"}
                                >
                                  <Trash2 size={13} />
                                </button>
                              </div>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  <div className="flex items-center justify-end border-t border-[#e5eeea] bg-[#f6faf8] px-2 py-2">
                    <button
                      type="button"
                      className={`${btnCls} border-sky-200 text-sky-700 hover:border-sky-400`}
                      onClick={() => {
                        setConfigMode("add");
                        setConfigEditorOpen(true);
                        setServicePortID(nextServicePortID);
                        setEditingServicePort(null);
                        setVport(defaultVPort);
                        setServicePortMode("tagged");
                        setServiceDescDraft("");
                        setUserVlan("");
                        setUserSVlan("");
                        setVlan("");
                        setCTagCos("");
                        setSvlan("");
                        setSTagCos("");
                        setEtherType("all");
                      }}
                    >
                      <Plus size={12} /> Tambah
                    </button>
                  </div>
                </div>
              )}

              {showListFirst && configTarget === "wan_ip" && (
                <div className="mb-3 overflow-hidden rounded-md border border-[#e5eeea] bg-[#fbfdfc]">
                  <div className="overflow-x-auto">
                    <table className="min-w-full text-xs">
                      <thead className="bg-[#f1f7f4] text-[#607067]">
                        <tr>
                          <th className="px-2 py-1.5 text-left font-semibold">ID</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Mode</th>
                          <th className="px-2 py-1.5 text-left font-semibold">VLAN Profile</th>
                          <th className="px-2 py-1.5 text-left font-semibold">IP Profile</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Static IP</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Username PPPoE</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Password PPPoE</th>
                          <th className="px-2 py-1.5 text-left font-semibold">Aksi</th>
                        </tr>
                      </thead>
                      <tbody>
                        {wanIPRows.length === 0 ? (
                          <tr>
                            <td className="px-2 py-2 text-[#8aa096]" colSpan={8}>Belum ada data WAN IP.</td>
                          </tr>
                        ) : wanIPRows.map((row) => (
                          <tr key={`wan-${row.id}`} className="border-t border-[#e5eeea]">
                            <td className="px-2 py-1.5 font-mono">{row.id}</td>
                            <td className="px-2 py-1.5">{wanModeLabel(row.mode)}</td>
                            <td className="px-2 py-1.5">{row.vlan_profile || "-"}</td>
                            <td className="px-2 py-1.5">{row.ip_profile || "-"}</td>
                            <td className="px-2 py-1.5 font-mono">{row.static_ip || "-"}</td>
                            <td className="px-2 py-1.5">{row.pppoe_username || "-"}</td>
                            <td className="px-2 py-1.5">{row.pppoe_password || "-"}</td>
                            <td className="px-2 py-1.5">
                              <div className="flex items-center gap-1">
                                <button
                                  type="button"
                                  className={`${btnCls} h-7 w-7 p-0`}
                                  title="Edit"
                                  onClick={() => pickWanIPForEdit(row)}
                                >
                                  <Pencil size={13} className="text-sky-600" />
                                </button>
                                <button
                                  type="button"
                                  className={`${btnCls} h-7 w-7 border-red-200 p-0 text-red-600 hover:border-red-400`}
                                  title="Hapus"
                                  onClick={() => {
                                    pickWanIPForEdit(row);
                                    setConfigMessage(null);
                                    void doDeleteWanIP(row.id);
                                  }}
                                  disabled={configBusy === "delete_wan_ip"}
                                >
                                  <Trash2 size={13} />
                                </button>
                              </div>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  <div className="flex items-center justify-end border-t border-[#e5eeea] bg-[#f6faf8] px-2 py-2">
                    <button
                      type="button"
                      className={`${btnCls} border-sky-200 text-sky-700 hover:border-sky-400`}
                      onClick={() => {
                        setConfigMode("add");
                        setConfigEditorOpen(true);
                        setEditingWanIP(null);
                        setWanIPID(nextWanIPID);
                        setWanMode("pppoe");
                        setWanVlanProfile(wanVlanProfileOptions[0] || "");
                        setWanIpProfile(wanIPProfileOptions[0] || "");
                        setWanAuthMode("auto");
                        setWanStaticIP("");
                        setWanPPPoEUsername("");
                        setWanPPPoEPassword("");
                        setWanRespondPing(true);
                        setWanRespondTraceroute(true);
                      }}
                    >
                      <Plus size={12} /> Tambah
                    </button>
                  </div>
                </div>
              )}

              {configTarget === "name" && (
                <form className="grid gap-1.5 md:max-w-xl" onSubmit={doSetName}>
                  <label className="text-[11px] font-semibold text-[#607067]">Nama ONU</label>
                  <div className="flex gap-1.5">
                    <input className={`${inputCls} flex-1`} value={nameDraft} onChange={(e) => setNameDraft(e.target.value)} placeholder="nama onu" />
                    <button className={btnCls} disabled={configBusy === "set_name"}>Simpan</button>
                  </div>
                </form>
              )}

              {configTarget === "description" && (
                <form className="grid gap-1.5 md:max-w-xl" onSubmit={doSetDescription}>
                  <label className="text-[11px] font-semibold text-[#607067]">Description</label>
                  <div className="flex gap-1.5">
                    <input className={`${inputCls} flex-1`} value={descDraft} onChange={(e) => setDescDraft(e.target.value)} placeholder="description" />
                    <button className={btnCls} disabled={configBusy === "set_description"}>Simpan</button>
                  </div>
                </form>
              )}

              {configTarget === "tcont" && (!showListFirst || configEditorOpen) && (
                <form className="grid gap-1.5 md:max-w-2xl" onSubmit={doSetTcont}>
                  <label className="text-[11px] font-semibold text-[#607067]">{configMode === "add" ? "Tambah T-CONT" : "Edit T-CONT"}</label>
                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-3">
                    <input className={inputCls} value={tcontID} onChange={(e) => setTcontID(e.target.value)} placeholder="ID" />
                    <input className={inputCls} value={tcontName} onChange={(e) => setTcontName(e.target.value)} placeholder="Name (opsional)" />
                    <input className={inputCls} value={tcontProfile} onChange={(e) => setTcontProfile(e.target.value)} placeholder="Bandwidth Profile" />
                  </div>
                  <div className="flex items-center gap-1.5">
                    <button className={btnCls} disabled={configBusy === "set_tcont"}>{configMode === "add" ? "Tambah" : "Simpan"}</button>
                  </div>
                </form>
              )}

              {configTarget === "gemport" && (!showListFirst || configEditorOpen) && (
                <form className="grid gap-1.5 md:max-w-2xl" onSubmit={doSetGemport}>
                  <label className="text-[11px] font-semibold text-[#607067]">{configMode === "add" ? "Tambah GEM-Port" : "Edit GEM-Port"}</label>
                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-3">
                    <input className={inputCls} value={gemportID} onChange={(e) => setGemportID(e.target.value)} placeholder="ID" />
                    <input className={inputCls} value={upProfile} onChange={(e) => setUpProfile(e.target.value)} placeholder="Upstream" />
                    <input className={inputCls} value={downProfile} onChange={(e) => setDownProfile(e.target.value)} placeholder="Downstream" />
                  </div>
                  <button className={btnCls} disabled={configBusy === "set_gemport"}>{configMode === "add" ? "Tambah" : "Simpan"}</button>
                </form>
              )}

              {configTarget === "service_port" && (!showListFirst || configEditorOpen) && (
                <form className="grid gap-2 md:max-w-3xl" onSubmit={doSetServicePort}>
                  <label className="text-[11px] font-semibold text-[#607067]">{configMode === "add" ? "Tambah Service-Port / VLAN" : "Edit Service-Port / VLAN"}</label>

                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-3">
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      Service Port Mode
                      <select className={inputCls} value={servicePortMode} onChange={(e) => setServicePortMode(e.target.value as "tagged" | "untagged" | "double_vlan" | "hybrid") }>
                        <option value="tagged">Tagged</option>
                        <option value="untagged">Untagged</option>
                        <option value="double_vlan">Double VLAN</option>
                        <option value="hybrid">Hybrid</option>
                      </select>
                    </label>
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      Service Port
                      <input type="number" min={1} max={4095} className={inputCls} value={servicePortID} onChange={(e) => setServicePortID(e.target.value)} placeholder="Service Port" />
                    </label>
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      Description
                      <input className={inputCls} value={serviceDescDraft} onChange={(e) => setServiceDescDraft(e.target.value)} placeholder="Internet" maxLength={96} />
                    </label>
                  </div>

                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-3">
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      User VID
                      <input type="number" min={1} max={4094} className={inputCls} value={userVlan} onChange={(e) => setUserVlan(e.target.value)} placeholder="User VID" />
                    </label>
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      User S-VID
                      <input type="number" min={1} max={4094} className={inputCls} value={userSVlan} onChange={(e) => setUserSVlan(e.target.value)} placeholder="Opsional" />
                    </label>
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      vPort
                      <input type="number" min={1} max={32} className={inputCls} value={vport} onChange={(e) => setVport(e.target.value)} placeholder="vPort" />
                    </label>
                  </div>

                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      Ether Type
                      <select className={inputCls} value={etherType} onChange={(e) => setEtherType(e.target.value as "all" | "pppoe" | "ipoe") }>
                        <option value="all">All Ether Type</option>
                        <option value="pppoe">PPPoE</option>
                        <option value="ipoe">IPoE</option>
                      </select>
                    </label>
                  </div>

                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-4">
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      C-VID
                      <input type="number" min={1} max={4094} className={inputCls} value={vlan} onChange={(e) => setVlan(e.target.value)} placeholder="C-VID" />
                    </label>
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      C-Tag CoS
                      <input type="number" min={0} max={7} className={inputCls} value={cTagCos} onChange={(e) => setCTagCos(e.target.value)} placeholder="0-7" />
                    </label>
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      S-VID
                      <input type="number" min={1} max={4094} className={inputCls} value={svlan} onChange={(e) => setSvlan(e.target.value)} placeholder="S-VID" />
                    </label>
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      S-Tag CoS
                      <input type="number" min={0} max={7} className={inputCls} value={sTagCos} onChange={(e) => setSTagCos(e.target.value)} placeholder="0-7" />
                    </label>
                  </div>

                  <button className={btnCls} disabled={configBusy === "set_service_port"}>{configMode === "add" ? "Tambah" : "Simpan"}</button>
                </form>
              )}

              {configTarget === "wan_ip" && (!showListFirst || configEditorOpen) && (
                <form className="grid gap-2 md:max-w-2xl" onSubmit={doSetWanIP}>
                  <label className="text-[11px] font-semibold text-[#607067]">{configMode === "add" ? "Tambah WAN IP" : `Edit WAN IP #${editingWanIP?.id || wanIPID}`}</label>

                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      ID
                      <input type="number" min={1} max={8} className={inputCls} value={wanIPID} onChange={(e) => setWanIPID(e.target.value)} placeholder="1-8" />
                    </label>
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      Mode
                      <select className={inputCls} value={wanMode} onChange={(e) => setWanMode(e.target.value as WanModeInput)}>
                        <option value="pppoe">PPPoE</option>
                        <option value="ipoe">IPoE</option>
                        <option value="static">Static IP</option>
                      </select>
                    </label>
                  </div>

                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      VLAN Profile *
                      <select className={inputCls} value={wanVlanProfile} onChange={(e) => setWanVlanProfile(e.target.value)}>
                        <option value="">Pilih VLAN Profile</option>
                        {wanVlanProfileOptions.map((opt) => (
                          <option key={`wan-vlan-${opt}`} value={opt}>{opt}</option>
                        ))}
                      </select>
                    </label>
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      Auth Mode
                      <select className={inputCls} value={wanAuthMode} onChange={(e) => setWanAuthMode(e.target.value as WanAuthModeInput)}>
                        <option value="auto">Auto</option>
                        <option value="pap">PAP</option>
                        <option value="chap">CHAP</option>
                      </select>
                    </label>
                  </div>

                  {wanVlanProfileOptions.length === 0 && (
                    <div className="rounded border border-amber-200 bg-amber-50 px-2 py-1 text-[11px] text-amber-700">
                      List VLAN Profile belum terbaca dari running-config ONU ini.
                    </div>
                  )}

                  {wanMode !== "pppoe" && (
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      IP Profile
                      <select className={inputCls} value={wanIpProfile} onChange={(e) => setWanIpProfile(e.target.value)}>
                        <option value="">Pilih IP Profile (opsional)</option>
                        {wanIPProfileOptions.map((opt) => (
                          <option key={`wan-ipprofile-${opt}`} value={opt}>{opt}</option>
                        ))}
                      </select>
                    </label>
                  )}

                  {wanMode === "static" && (
                    <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                      Static IP *
                      <input className={inputCls} value={wanStaticIP} onChange={(e) => setWanStaticIP(e.target.value)} placeholder="contoh: 10.10.10.2" maxLength={64} />
                    </label>
                  )}

                  {wanMode === "pppoe" && (
                    <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                      <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                        Username *
                        <input className={inputCls} value={wanPPPoEUsername} onChange={(e) => setWanPPPoEUsername(e.target.value)} placeholder="username" maxLength={96} />
                      </label>
                      <label className="grid gap-1 text-[10px] font-semibold uppercase tracking-wide text-[#607067]">
                        Password *
                        <input className={inputCls} value={wanPPPoEPassword} onChange={(e) => setWanPPPoEPassword(e.target.value)} placeholder="password" maxLength={96} />
                      </label>
                    </div>
                  )}

                  <div className="flex flex-wrap items-center gap-4 rounded border border-[#e5eeea] bg-[#f6faf8] px-2 py-2 text-[11px] text-[#44554d]">
                    <label className="inline-flex items-center gap-1.5">
                      <input type="checkbox" checked={wanRespondPing} onChange={(e) => setWanRespondPing(e.target.checked)} />
                      Allow Respond Ping
                    </label>
                    <label className="inline-flex items-center gap-1.5">
                      <input type="checkbox" checked={wanRespondTraceroute} onChange={(e) => setWanRespondTraceroute(e.target.checked)} />
                      Allow Respond Traceroute
                    </label>
                  </div>

                  <div className="flex items-center justify-end gap-1.5">
                    <button
                      type="button"
                      className={btnCls}
                      onClick={() => {
                        setConfigEditorOpen(false);
                        setEditingWanIP(null);
                        setConfigMessage(null);
                      }}
                    >
                      Cancel
                    </button>
                    <button className={`${btnCls} border-sky-200 text-sky-700 hover:border-sky-400`} disabled={configBusy === "set_wan_ip"}>
                      {configBusy === "set_wan_ip" ? "Submitting..." : "Submit"}
                    </button>
                  </div>
                </form>
              )}

              {configTarget === "auto_config" && (
                <form className="grid gap-2 md:max-w-3xl" onSubmit={doAutoConfigONU}>
                  <label className="text-[11px] font-semibold text-[#607067]">Auto Config ONU (end-to-end)</label>
                  <div className="rounded border border-[#d8e7df] bg-[#f6faf8] px-2 py-1 text-[11px] text-[#4f645a]">
                    Mode target: pilih <b>PPPoE</b>, <b>IPoE</b>, atau <b>Static IP</b>, lalu sekali submit sistem akan apply berurutan: T-CONT → GEM-Port → Service-Port → WAN IP.
                  </div>

                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-3">
                    <input type="number" min={1} max={32} className={inputCls} value={tcontID} onChange={(e) => setTcontID(e.target.value)} placeholder="TCONT ID" />
                    <input className={inputCls} value={tcontProfile} onChange={(e) => setTcontProfile(e.target.value)} placeholder="TCONT Profile" />
                    <input className={inputCls} value={tcontName} onChange={(e) => setTcontName(e.target.value)} placeholder="TCONT Name (opsional)" />
                  </div>

                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-3">
                    <input type="number" min={1} max={4095} className={inputCls} value={gemportID} onChange={(e) => setGemportID(e.target.value)} placeholder="GEM-Port ID" />
                    <input className={inputCls} value={upProfile} onChange={(e) => setUpProfile(e.target.value)} placeholder="Upstream Profile" />
                    <input className={inputCls} value={downProfile} onChange={(e) => setDownProfile(e.target.value)} placeholder="Downstream Profile" />
                  </div>

                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-4">
                    <input type="number" min={1} max={4095} className={inputCls} value={servicePortID} onChange={(e) => setServicePortID(e.target.value)} placeholder="Service Port" />
                    <input type="number" min={1} max={32} className={inputCls} value={vport} onChange={(e) => setVport(e.target.value)} placeholder="vPort" />
                    <input type="number" min={1} max={4094} className={inputCls} value={userVlan} onChange={(e) => setUserVlan(e.target.value)} placeholder="User VID" />
                    <input type="number" min={1} max={4094} className={inputCls} value={vlan} onChange={(e) => setVlan(e.target.value)} placeholder="C-VID" />
                  </div>

                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-4">
                    <select className={inputCls} value={servicePortMode} onChange={(e) => setServicePortMode(e.target.value as ServicePortModeInput)}>
                      <option value="tagged">Tagged</option>
                      <option value="untagged">Untagged</option>
                      <option value="double_vlan">Double VLAN</option>
                      <option value="hybrid">Hybrid</option>
                    </select>
                    <select className={inputCls} value={etherType} onChange={(e) => setEtherType(e.target.value as EtherTypeInput)}>
                      <option value="all">All Ether Type</option>
                      <option value="pppoe">PPPoE</option>
                      <option value="ipoe">IPoE</option>
                    </select>
                    <input type="number" min={1} max={4094} className={inputCls} value={svlan} onChange={(e) => setSvlan(e.target.value)} placeholder="S-VID" />
                    <input className={inputCls} value={serviceDescDraft} onChange={(e) => setServiceDescDraft(e.target.value)} placeholder="Service Description" maxLength={96} />
                  </div>

                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-3">
                    <input type="number" min={1} max={8} className={inputCls} value={wanIPID} onChange={(e) => setWanIPID(e.target.value)} placeholder="WAN ID" />
                    <select className={inputCls} value={wanMode} onChange={(e) => setWanMode(e.target.value as WanModeInput)}>
                      <option value="pppoe">PPPoE</option>
                      <option value="ipoe">IPoE</option>
                      <option value="static">Static IP</option>
                    </select>
                    <select className={inputCls} value={wanVlanProfile} onChange={(e) => setWanVlanProfile(e.target.value)}>
                      <option value="">Pilih VLAN Profile</option>
                      {wanVlanProfileOptions.map((opt) => (
                        <option key={`autowan-vlan-${opt}`} value={opt}>{opt}</option>
                      ))}
                    </select>
                  </div>

                  {wanMode !== "pppoe" && (
                    <input className={inputCls} value={wanIpProfile} onChange={(e) => setWanIpProfile(e.target.value)} placeholder="IP Profile (opsional)" />
                  )}

                  {wanMode === "static" && (
                    <input className={inputCls} value={wanStaticIP} onChange={(e) => setWanStaticIP(e.target.value)} placeholder="Static IP (contoh: 10.10.10.2)" maxLength={64} />
                  )}

                  {wanMode === "pppoe" && (
                    <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                      <input className={inputCls} value={wanPPPoEUsername} onChange={(e) => setWanPPPoEUsername(e.target.value)} placeholder="PPPoE Username" maxLength={96} />
                      <input className={inputCls} value={wanPPPoEPassword} onChange={(e) => setWanPPPoEPassword(e.target.value)} placeholder="PPPoE Password" maxLength={96} />
                    </div>
                  )}

                  <div className="flex flex-wrap items-center gap-4 rounded border border-[#e5eeea] bg-[#f6faf8] px-2 py-2 text-[11px] text-[#44554d]">
                    <label className="inline-flex items-center gap-1.5">
                      <input type="checkbox" checked={wanRespondPing} onChange={(e) => setWanRespondPing(e.target.checked)} />
                      Allow Respond Ping
                    </label>
                    <label className="inline-flex items-center gap-1.5">
                      <input type="checkbox" checked={wanRespondTraceroute} onChange={(e) => setWanRespondTraceroute(e.target.checked)} />
                      Allow Respond Traceroute
                    </label>
                  </div>

                  <div className="flex items-center justify-end gap-1.5">
                    <button type="submit" className={`${btnCls} border-emerald-200 text-emerald-700 hover:border-emerald-400`} disabled={configBusy === "auto_config_onu"}>
                      {configBusy === "auto_config_onu" ? "Menjalankan..." : "Jalankan Auto Config"}
                    </button>
                  </div>
                </form>
              )}
            </div>

            <div className="flex items-center justify-end border-t border-[#e5eeea] bg-[#f8fbfa] px-3 py-2">
              <button type="button" className={btnCls} onClick={() => {
                setConfigModalOpen(false);
                setConfigEditorOpen(false);
                setConfigMessage(null);
                setEditingServicePort(null);
                setEditingWanIP(null);
              }}>
                Tutup
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

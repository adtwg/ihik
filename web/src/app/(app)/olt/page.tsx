"use client";

import { useEffect, useState } from "react";
import { clientAPI } from "@/lib/api/client";
import { type OLT } from "@/lib/olts/types";
import { OltManager } from "./olt-manager";

export default function OltsPage() {
  const [olts, setOlts] = useState<OLT[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const controller = new AbortController();
    const t = setTimeout(() => controller.abort(), 12000);
    clientAPI<{ items: OLT[] }>("/api/v1/olts", { signal: controller.signal })
      .then((data) => {
        if (!cancelled) setOlts(data.items ?? []);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : "Gagal memuat daftar OLT.");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
        clearTimeout(t);
      });
    return () => {
      cancelled = true;
      controller.abort();
    };
  }, []);

  return (
    <div className="page">
      <div className="page-heading"><div><h1>OLT</h1><p>Manajemen OLT ZTE C320/C300 via SNMP: status, redaman ONU, reboot, dan disable/enable.</p></div></div>
      <OltManager initialOlts={olts} loading={loading} error={error} />
    </div>
  );
}

"use client";

import { useEffect, useState } from "react";
import { clientAPI } from "@/lib/api/client";
import { type OLT } from "@/lib/olts/types";
import { ChassisView } from "./chassis-view";

export default function OltDevicePage() {
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
      <div className="page-heading">
        <div>
          <h1>Perangkat OLT</h1>
          <p>Skema fisik chassis OLT ZTE: card terpasang (SMXA/GTGH/GTGO), slot, dan status per port.</p>
        </div>
      </div>
      <ChassisView olts={olts} loading={loading} error={error} />
    </div>
  );
}

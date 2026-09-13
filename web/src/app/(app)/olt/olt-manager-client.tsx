"use client";

import { clientAPI } from "@/lib/api/client";
import { type OLT } from "@/lib/olts/types";
import { useEffect, useState } from "react";
import { OltManager } from "./olt-manager";

export function OltManagerClient({ initialOlts, ssrFailed }: { initialOlts: OLT[]; ssrFailed: boolean }) {
  const [olts, setOlts] = useState<OLT[]>(initialOlts);
  const [loading, setLoading] = useState(ssrFailed && initialOlts.length === 0);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    // Jika SSR sudah berhasil kirim OLT, tidak perlu fetch ulang.
    if (!ssrFailed) {
      setLoading(false);
      return;
    }
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
        clearTimeout(t);
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; controller.abort(); };
  }, [ssrFailed]);

  return <OltManager initialOlts={olts} loading={loading} error={error} />;
}

import { serverAPI } from "@/lib/api/server";
import type { RouterConfig } from "@/lib/router/types";
import { RouterSettings } from "./router-settings";

export default async function RouterPage() {
  const config = await serverAPI<RouterConfig>("/api/v1/router");
  return (
    <div className="page">
      <div className="page-heading"><div><h1>Router MikroTik</h1><p>Hubungkan router untuk sinkronisasi PPPoE, cek koneksi, dan isolir otomatis.</p></div></div>
      <RouterSettings config={config} />
    </div>
  );
}

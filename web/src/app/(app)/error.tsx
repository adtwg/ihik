"use client";

import { AlertTriangle, RefreshCw } from "lucide-react";

export default function AppError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
	return <div className="page"><section className="panel mx-auto mt-10 max-w-xl"><div className="panel-body text-center"><AlertTriangle className="mx-auto mb-4 text-[#b42318]" size={32} /><h1 className="m-0 text-xl font-bold">Data belum dapat dimuat</h1><p className="mb-5 mt-2 text-sm text-[#607067]">Koneksi ke layanan sedang bermasalah. Coba muat ulang halaman ini.</p><button className="primary-button" onClick={reset}><RefreshCw size={17} /> Muat ulang</button></div></section></div>;
}
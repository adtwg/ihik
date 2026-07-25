import { Activity, Database, Server, ShieldCheck } from "lucide-react";

export default function PlatformPage() {
	const status = [
		{ label: "API", value: "Tersedia", icon: Server },
		{ label: "Database", value: "Terhubung", icon: Database },
		{ label: "Keamanan", value: "Aktif", icon: ShieldCheck },
		{ label: "Worker", value: "Belum diaktifkan", icon: Activity },
	];
	return <div className="page"><div className="page-heading"><div><h1>Platform</h1><p>Status komponen inti ISP Billing.</p></div></div><section className="kpi-grid" style={{ gridTemplateColumns: "repeat(4, minmax(0, 1fr))" }}>{status.map(({ label, value, icon: Icon }) => <div className="kpi-cell" key={label}><span className="flex items-center gap-2 text-xs font-semibold text-[#607067]"><Icon size={16} />{label}</span><strong className="kpi-value text-base">{value}</strong></div>)}</section></div>;
}
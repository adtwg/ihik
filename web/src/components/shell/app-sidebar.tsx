"use client";

import { useEffect, useRef } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Boxes, Building2, FileText, Gauge, Network, PanelLeftClose, PanelLeftOpen, Users, Wallet, X } from "lucide-react";
import type { Role } from "@/lib/api/types";

const mitraNavigation = [
  { href: "/dashboard", label: "Dashboard", icon: Gauge },
  { href: "/pelanggan", label: "Pelanggan", icon: Users },
  { href: "/paket", label: "Paket", icon: Boxes },
  { href: "/layanan", label: "Layanan", icon: Network },
  { href: "/tagihan", label: "Tagihan", icon: FileText },
  { href: "/pembayaran", label: "Pembayaran", icon: Wallet },
];

const platformNavigation = [
	{ href: "/platform", label: "Platform", icon: Gauge },
	{ href: "/platform/mitra", label: "Mitra", icon: Building2 },
];

export function AppSidebar({ role, collapsed, mobileOpen, onCollapse, onMobileClose }: { role: Role; collapsed: boolean; mobileOpen: boolean; onCollapse: () => void; onMobileClose: () => void }) {
  const pathname = usePathname();
	const navigation = role === "super_admin" ? platformNavigation : mitraNavigation;
  const mobilePanel = useRef<HTMLElement>(null);

  useEffect(() => {
    if (!mobileOpen) return;
    const previousFocus = document.activeElement as HTMLElement | null;
    const panel = mobilePanel.current;
    const focusable = panel?.querySelectorAll<HTMLElement>('a[href], button:not([disabled])');
    focusable?.[0]?.focus();
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onMobileClose();
      if (event.key !== "Tab" || !focusable?.length) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }
    document.addEventListener("keydown", handleKeyDown);
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      document.body.style.overflow = "";
      previousFocus?.focus();
    };
  }, [mobileOpen, onMobileClose]);
  const links = navigation.map((item) => {
    const Icon = item.icon;
    const active = item.href === "/platform" ? pathname === item.href : pathname === item.href || pathname.startsWith(`${item.href}/`);
    return (
      <Link key={item.href} href={item.href} title={collapsed ? item.label : undefined} onClick={onMobileClose} className={`flex h-11 items-center gap-3 rounded-md px-3 text-sm font-semibold transition-colors ${active ? "bg-white text-[#10251d]" : "text-[#dbe7e1] hover:bg-white/10"}`}>
        <Icon size={20} className="shrink-0" /><span className={collapsed ? "sr-only" : "truncate"}>{item.label}</span>
      </Link>
    );
  });

  return (
    <>
      <aside className="app-sidebar">
        <div className="flex h-16 items-center justify-between border-b border-white/10 px-3">
          <div className="flex min-w-0 items-center gap-3"><div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#d9a514] font-black text-[#10251d]">IB</div>{!collapsed && <strong className="truncate">ISP Billing</strong>}</div>
          {!collapsed && <button className="icon-button text-[#dbe7e1] hover:bg-white/10" onClick={onCollapse} aria-label="Ringkas menu"><PanelLeftClose size={19} /></button>}
        </div>
        <nav className="grid gap-1 p-2" aria-label="Menu utama">{links}</nav>
        {collapsed && <button className="icon-button absolute bottom-3 left-[10px] text-[#dbe7e1] hover:bg-white/10" onClick={onCollapse} aria-label="Perluas menu" title="Perluas menu"><PanelLeftOpen size={19} /></button>}
      </aside>
      {mobileOpen && <div className="fixed inset-0 z-50 lg:hidden" role="dialog" aria-modal="true" aria-label="Navigasi" id="mobile-navigation"><button className="absolute inset-0 bg-black/45" onClick={onMobileClose} aria-label="Tutup menu" /><aside ref={mobilePanel} className="relative h-full w-[min(84vw,320px)] bg-[#10251d] p-3 text-white shadow-2xl"><div className="mb-3 flex h-12 items-center justify-between"><strong>ISP Billing</strong><button className="icon-button hover:bg-white/10" onClick={onMobileClose} aria-label="Tutup menu"><X size={20} /></button></div><nav className="grid gap-1" aria-label="Menu mobile">{links}</nav></aside></div>}
    </>
  );
}
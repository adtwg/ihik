"use client";

import { useState } from "react";
import type { Principal } from "@/lib/api/types";
import { AppSidebar } from "./app-sidebar";
import { LogOut, Menu, UserRound, XCircle } from "lucide-react";
import { clientAPI } from "@/lib/api/client";

export function AppShell({ user, impersonating, initialCollapsed, children }: { user: Principal; impersonating?: boolean; initialCollapsed: boolean; children: React.ReactNode }) {
  const [collapsed, setCollapsed] = useState(initialCollapsed);
  const [mobileOpen, setMobileOpen] = useState(false);

  function toggleCollapsed() {
    setCollapsed((current) => {
      const next = !current;
      document.cookie = `sidebar=${next ? "collapsed" : "expanded"}; Path=/; Max-Age=31536000; SameSite=Lax`;
      return next;
    });
  }

  async function logout() {
    await clientAPI("/api/v1/auth/logout", { method: "POST" });
    window.location.assign("/login");
  }

  function stopImpersonation() {
    // Hapus cookie impersonasi lalu kembali ke daftar mitra.
    document.cookie = "impersonate_tenant=; Path=/; Max-Age=0; SameSite=Strict";
    window.location.assign("/platform/mitra");
  }

  return (
    <div className="app-grid" data-collapsed={collapsed}>
      <AppSidebar role={user.role} collapsed={collapsed} mobileOpen={mobileOpen} onCollapse={toggleCollapsed} onMobileClose={() => setMobileOpen(false)} />
      <div className="app-content">
        <header className="topbar">
          <button className="icon-button mobile-menu-button" onClick={() => setMobileOpen(true)} aria-label="Buka menu" aria-expanded={mobileOpen} aria-controls="mobile-navigation"><Menu size={21} /></button>
          <div className="hidden text-sm font-semibold text-[#607067] sm:block">Operasional ISP</div>
          <div className="flex min-w-0 items-center gap-1 text-sm font-semibold"><UserRound size={18} /><span className="max-w-[180px] truncate px-1">{user.username}</span><button className="icon-button" onClick={logout} title="Keluar" aria-label="Keluar"><LogOut size={18} /></button></div>
        </header>
        {impersonating ? (
          <div className="flex items-center justify-between gap-3 border-b border-amber-300 bg-amber-100 px-4 py-2 text-sm font-semibold text-amber-900">
            <span>Mode Super Admin: mengelola data Mitra.</span>
            <button className="inline-flex items-center gap-1 rounded-md px-2 py-1 hover:bg-amber-200" onClick={stopImpersonation}><XCircle size={16} /> Selesai kelola mitra</button>
          </div>
        ) : null}
        <main>{children}</main>
      </div>
    </div>
  );
}

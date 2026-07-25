"use client";

import { useState } from "react";
import type { Principal } from "@/lib/api/types";
import { AppSidebar } from "./app-sidebar";
import { LogOut, Menu, UserRound } from "lucide-react";
import { clientAPI } from "@/lib/api/client";

export function AppShell({ user, initialCollapsed, children }: { user: Principal; initialCollapsed: boolean; children: React.ReactNode }) {
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

  return (
    <div className="app-grid" data-collapsed={collapsed}>
      <AppSidebar role={user.role} collapsed={collapsed} mobileOpen={mobileOpen} onCollapse={toggleCollapsed} onMobileClose={() => setMobileOpen(false)} />
      <div className="app-content">
        <header className="topbar">
          <button className="icon-button mobile-menu-button" onClick={() => setMobileOpen(true)} aria-label="Buka menu" aria-expanded={mobileOpen} aria-controls="mobile-navigation"><Menu size={21} /></button>
          <div className="hidden text-sm font-semibold text-[#607067] sm:block">Operasional ISP</div>
          <div className="flex min-w-0 items-center gap-1 text-sm font-semibold"><UserRound size={18} /><span className="max-w-[180px] truncate px-1">{user.username}</span><button className="icon-button" onClick={logout} title="Keluar" aria-label="Keluar"><LogOut size={18} /></button></div>
        </header>
        <main>{children}</main>
      </div>
    </div>
  );
}
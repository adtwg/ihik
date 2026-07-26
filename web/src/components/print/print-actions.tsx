"use client";

import { useEffect } from "react";
import { Printer, X } from "lucide-react";

export function PrintActions({ autoPrint = true }: { autoPrint?: boolean }) {
  useEffect(() => {
    if (!autoPrint) return;
    const timer = window.setTimeout(() => window.print(), 400);
    return () => window.clearTimeout(timer);
  }, [autoPrint]);
  return (
    <div className="print-actions no-print">
      <button className="primary-button" onClick={() => window.print()}><Printer size={17} /> Cetak</button>
      <button className="secondary-button" onClick={() => window.close()}><X size={17} /> Tutup</button>
    </div>
  );
}

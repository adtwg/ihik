"use client";

import { startTransition, type FormEvent } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Filter } from "lucide-react";

export function ReportFilter({ from, to }: { from: string; to: string }) {
  const router = useRouter();
  const pathname = usePathname();

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const next = new URLSearchParams();
    const fromValue = String(form.get("from") ?? "");
    const toValue = String(form.get("to") ?? "");
    if (fromValue) next.set("from", fromValue);
    if (toValue) next.set("to", toValue);
    startTransition(() => router.replace(`${pathname}?${next.toString()}`));
  }

  return (
    <form className="flex flex-wrap items-end gap-2" onSubmit={submit}>
      <label className="grid gap-1 text-xs font-semibold text-[#607067]">Dari<input className="secondary-button" type="month" name="from" defaultValue={from} required /></label>
      <label className="grid gap-1 text-xs font-semibold text-[#607067]">Sampai<input className="secondary-button" type="month" name="to" defaultValue={to} required /></label>
      <button className="primary-button" type="submit"><Filter size={16} /> Terapkan</button>
    </form>
  );
}

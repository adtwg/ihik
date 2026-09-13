"use server";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

export async function impersonateTenant(formData: FormData) {
  const tenantID = String(formData.get("tenant_id") ?? "").trim();
  if (!tenantID) return;
  const cookieStore = await cookies();
  cookieStore.set("impersonate_tenant", tenantID, {
    httpOnly: true,
    sameSite: "strict",
    secure: true,
    path: "/",
    maxAge: 60 * 60 * 8,
  });
  redirect("/dashboard");
}

export async function stopImpersonation() {
  const cookieStore = await cookies();
  cookieStore.delete("impersonate_tenant");
  redirect("/platform/mitra");
}

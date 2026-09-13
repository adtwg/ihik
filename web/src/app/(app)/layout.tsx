import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { AppShell } from "@/components/shell/app-shell";
import { getCurrentUser } from "@/lib/api/server";

export default async function ProtectedLayout({ children }: { children: React.ReactNode }) {
  let user;
  try {
    user = await getCurrentUser();
  } catch {
    redirect("/login");
  }
  const cookieStore = await cookies();
  const impersonating = user.role === "super_admin" && Boolean(cookieStore.get("impersonate_tenant")?.value);
  if (user.role !== "mitra" && !impersonating) redirect("/platform");
  return <AppShell user={user} impersonating={impersonating} initialCollapsed={cookieStore.get("sidebar")?.value === "collapsed"}>{children}</AppShell>;
}

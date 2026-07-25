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
  if (user.role !== "mitra") redirect("/platform");
  const cookieStore = await cookies();
  return <AppShell user={user} initialCollapsed={cookieStore.get("sidebar")?.value === "collapsed"}>{children}</AppShell>;
}
import { redirect } from "next/navigation";
import { getCurrentUser } from "@/lib/api/server";

export default async function PrintLayout({ children }: { children: React.ReactNode }) {
  let user;
  try {
    user = await getCurrentUser();
  } catch {
    redirect("/login");
  }
  if (user.role !== "mitra") redirect("/platform");
  return <main className="print-page">{children}</main>;
}

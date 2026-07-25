import { redirect } from "next/navigation";
import { getCurrentUser } from "@/lib/api/server";

export default async function HomePage() {
	let destination = "/login";
  try {
    const user = await getCurrentUser();
		destination = user.role === "super_admin" ? "/platform" : "/dashboard";
  } catch {}
	redirect(destination);
}
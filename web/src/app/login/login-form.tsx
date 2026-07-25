"use client";

import { useState, type FormEvent } from "react";
import { Eye, EyeOff, LogIn } from "lucide-react";
import { useRouter } from "next/navigation";
import { clientAPI } from "@/lib/api/client";
import { APIError, type Principal } from "@/lib/api/types";

export function LoginForm() {
  const router = useRouter();
  const [visible, setVisible] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setError("");
    const form = new FormData(event.currentTarget);
    try {
      const user = await clientAPI<Principal>("/api/v1/auth/login", {
        method: "POST",
        body: JSON.stringify({ username: form.get("username"), password: form.get("password") }),
      });
      router.replace(user.role === "super_admin" ? "/platform" : "/dashboard");
      router.refresh();
    } catch (cause) {
      setError(cause instanceof APIError ? cause.message : "Login belum dapat diproses.");
    } finally {
      setPending(false);
    }
  }

  return (
    <form className="grid gap-5" onSubmit={submit}>
      {error && <div className="error-box" role="alert">{error}</div>}
      <div className="field">
        <label htmlFor="username">Username</label>
        <input id="username" name="username" autoComplete="username" required maxLength={320} autoFocus />
      </div>
      <div className="field">
        <label htmlFor="password">Password</label>
        <div className="relative">
          <input id="password" name="password" type={visible ? "text" : "password"} autoComplete="current-password" required className="pr-12" />
          <button type="button" className="icon-button absolute right-0 top-0" onClick={() => setVisible((value) => !value)} aria-label={visible ? "Sembunyikan password" : "Tampilkan password"}>
            {visible ? <EyeOff size={18} /> : <Eye size={18} />}
          </button>
        </div>
      </div>
      <button className="primary-button w-full" disabled={pending}>
        <LogIn size={18} /> {pending ? "Memeriksa..." : "Masuk"}
      </button>
    </form>
  );
}
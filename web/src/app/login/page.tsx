import { LoginForm } from "./login-form";

export default function LoginPage() {
  return (
    <main className="min-h-screen bg-[#10251d] px-5 py-10 text-[#17201c] sm:grid sm:place-items-center">
      <section className="mx-auto w-full max-w-[420px] rounded-lg border border-white/10 bg-white p-6 shadow-2xl sm:p-8">
        <div className="mb-8">
          <div className="mb-5 flex h-11 w-11 items-center justify-center rounded-md bg-[#096b4c] text-lg font-bold text-white">IB</div>
          <h1 className="m-0 text-2xl font-bold">Masuk ke AWGRevBILL</h1>
          <p className="mt-2 text-sm text-[#607067]">Gunakan akun operasional Anda.</p>
        </div>
        <LoginForm />
      </section>
    </main>
  );
}
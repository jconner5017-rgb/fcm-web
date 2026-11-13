import { redirect } from "next/navigation";
import Link from "next/link";
import { apiGet, getMe } from "@/lib/api";
import type { AdminAccount } from "@/lib/types";
import AdminAccounts from "@/components/AdminAccounts";

export const metadata = { title: "Admin" };

export default async function AdminPage({ searchParams }: { searchParams: Promise<{ q?: string }> }) {
  const { q } = await searchParams;
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/admin");
  if (!me.account.is_admin) redirect("/");
  if (!me.mfa_enabled) {
    return (
      <div className="mt-8 max-w-[640px]">
        <h1 className="text-[26px] font-bold">Admin</h1>
        <p className="card mt-4">Turn on two-factor sign-in on your <Link href="/account" className="font-semibold text-fire">Account page</Link> first. Admin tools need it.</p>
      </div>
    );
  }
  const r = await apiGet<{ accounts: AdminAccount[] }>("/v1/admin/accounts?q=" + encodeURIComponent(q ?? ""));
  return (
    <div className="mt-8">
      <h1 className="text-[26px] font-bold">Accounts</h1>
      <p className="mt-1 text-muted">Turn on the one-time $20 web add-on after a customer pays, or give a trial a few more days.</p>
      <form className="mt-4 flex gap-2" action="/admin" method="get">
        <input name="q" defaultValue={q} placeholder="Search by email" className="field !mt-0 max-w-[320px]" />
        <button type="submit" className="btn-ghost">Search</button>
      </form>
      <AdminAccounts initial={r.data?.accounts ?? []} />
    </div>
  );
}

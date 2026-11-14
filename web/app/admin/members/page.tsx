import Link from "next/link";
import { apiGet, getMe } from "@/lib/api";
import type { AdminAccount } from "@/lib/types";
import AdminAccounts from "@/components/AdminAccounts";

const FILTERS: [string, string][] = [["", "All"], ["trial", "Free trial"], ["paid", "Paid"], ["off", "Sync off"], ["reused", "Trial reused"],
  ["suspended", "Suspended"], ["banned", "Banned"], ["unverified", "Email not confirmed"], ["closing", "Closing"], ["staff", "Staff"]];

export default async function Members({ searchParams }: { searchParams: Promise<{ q?: string; filter?: string }> }) {
  const { q = "", filter = "" } = await searchParams;
  const { me } = await getMe();
  const canBill = !!me && (me.account.is_admin || me.account.staff_role === "manager");
  const r = await apiGet<{ accounts: AdminAccount[] }>(`/v1/admin/accounts?q=${encodeURIComponent(q)}&filter=${encodeURIComponent(filter)}`);
  return (
    <div className="mt-6">
      <h1 className="text-[26px] font-bold">Members</h1>
      {canBill && (
        <p className="mt-1 text-[14px] text-muted">After a customer pays, add their months. FCM Web alone: $39.99 for 6 months, $59.99 for 12. Bundle (FCM desktop + FCM Web): $59.99 for 6 months, $89.99 for 12 (also issue their FCM license key). A renewal adds on to the current end date.</p>
      )}
      <form className="mt-4 flex flex-wrap gap-2" action="/admin/members" method="get">
        <input name="q" defaultValue={q} placeholder="Email or sign-up IP" className="field !mt-0 max-w-[320px]" />
        <input type="hidden" name="filter" value={filter} />
        <button type="submit" className="btn-ghost">Search</button>
      </form>
      <div className="mt-3 flex flex-wrap gap-1 text-[13px] font-semibold">
        {FILTERS.map(([v, label]) => (
          <Link key={v} href={`/admin/members?filter=${v}${q ? `&q=${encodeURIComponent(q)}` : ""}`}
            className={`rounded-full border px-3 py-1 ${v === filter ? "border-fire bg-fire text-white" : "border-line hover:border-fire"}`}>{label}</Link>
        ))}
      </div>
      <AdminAccounts key={q + filter} initial={r.data?.accounts ?? []} canBill={canBill} />
    </div>
  );
}

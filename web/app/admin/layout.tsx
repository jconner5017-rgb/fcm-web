import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";

export const metadata = { title: "Back office" };

/** The back office: only owners and the staff they grant a role, with 2FA on. */
export default async function AdminLayout({ children }: { children: React.ReactNode }) {
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/admin");
  const a = me.account;
  if (!a.is_admin && !a.staff_role) redirect("/");
  if (!me.mfa_enabled) {
    return (
      <div className="mt-8 max-w-[640px]">
        <h1 className="text-[26px] font-bold">Back office</h1>
        <p className="card mt-4">Turn on two-factor sign-in on your <Link href="/account" className="font-semibold text-fire">Account page</Link> first. The back office needs it.</p>
      </div>
    );
  }
  const access = await apiGet<{ ok: boolean }>("/v1/admin/access");
  if (access.code === "ip_not_allowed") {
    return (
      <div className="mt-8 max-w-[640px]">
        <h1 className="text-[26px] font-bold">Back office</h1>
        <p className="card mt-4">The back office can&apos;t be used from this network. An owner can add it on the Staff page from an allowed network.</p>
      </div>
    );
  }
  const tabs: [string, string][] = [["/admin", "Overview"], ["/admin/members", "Members"], ["/admin/alerts", "Alerts"], ["/admin/audit", "Audit log"]];
  if (a.is_admin) tabs.push(["/admin/staff", "Staff"]);
  return (
    <div className="mt-6">
      <div className="flex flex-wrap items-center gap-2 border-b border-line pb-3">
        <span className="mr-2 text-[13px] font-semibold uppercase tracking-wide text-muted">Back office</span>
        {tabs.map(([href, label]) => (
          <Link key={href} href={href} className="rounded-full border border-line px-3 py-1 text-[14px] font-semibold hover:border-fire hover:text-fire">{label}</Link>
        ))}
        <span className="ml-auto text-[12px] text-muted">{a.is_admin ? "Owner" : a.staff_role === "manager" ? "Manager" : "Support"} · {a.email}</span>
      </div>
      {children}
    </div>
  );
}

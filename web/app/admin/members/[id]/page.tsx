import Link from "next/link";
import { notFound } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import { shortDate, when } from "@/lib/client";
import { trialReuseText, type AdminAccount } from "@/lib/types";
import AdminAccounts from "@/components/AdminAccounts";
import MemberActions from "@/components/admin/MemberActions";
import { ACTIONS } from "@/lib/admin";

type Member = AdminAccount & {
  signup_ip?: string; closing_at?: string; mfa_enabled: boolean; sessions: number; sold: number;
  devices: { id: string; name: string; edition: string; app_version?: string; created_at: string; last_seen_at?: string; revoked_at?: string; shared_with: number }[];
  sign_ins: { at: string; ip: string; user_agent: string; success: boolean; reason?: string }[];
  notes: { id: number; author: string; body: string; at: string }[];
  audit: { id: number; actor_email: string; action: string; detail?: string; at: string }[];
  related: { id?: string; email: string; reason: string }[];
};

function agent(ua: string): string {
  const os = /Windows/.test(ua) ? "Windows" : /iPhone|iPad/.test(ua) ? "iPhone/iPad" : /Android/.test(ua) ? "Android" : /Mac OS/.test(ua) ? "Mac" : /Linux/.test(ua) ? "Linux" : "";
  const br = /Edg\//.test(ua) ? "Edge" : /Chrome\//.test(ua) ? "Chrome" : /Firefox\//.test(ua) ? "Firefox" : /Safari\//.test(ua) ? "Safari" : ua.slice(0, 30);
  return [br, os].filter(Boolean).join(" on ");
}

export default async function MemberPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const [{ me }, r] = await Promise.all([getMe(), apiGet<Member>(`/v1/admin/accounts/${id}`)]);
  if (!r.data) notFound();
  const m = r.data!;
  const canBill = !!me && (me.account.is_admin || me.account.staff_role === "manager");
  const protectedAcct = m.is_admin || (!!m.staff_role && !me?.account.is_admin);
  const suspended = m.status === "disabled" && !m.closing_at;
  return (
    <div className="mt-6 space-y-6">
      <div>
        <Link href="/admin/members" className="text-[14px] text-fire">← Members</Link>
        <h1 className="mt-1 text-[26px] font-bold">{m.email}</h1>
        <p className="text-[14px] text-muted">
          Joined {shortDate(m.created_at)}{m.signup_ip ? ` from ${m.signup_ip}` : ""} · email {m.email_verified ? "confirmed" : "not confirmed"} ·
          two-factor {m.mfa_enabled ? "on" : "off"} · {m.sessions} signed-in browser{m.sessions === 1 ? "" : "s"} · {m.cards.toLocaleString()} cards · {m.sold} sold
          {m.staff_role ? ` · staff: ${m.staff_role}` : ""}{m.is_admin ? " · owner" : ""}
        </p>
        {suspended && <p className="mt-2 rounded-lg border border-bad px-3 py-2 text-bad">Suspended: {m.suspended_reason || "no reason given"}</p>}
        {m.closing_at && <p className="mt-2 rounded-lg border border-bad px-3 py-2 text-bad">Closed {shortDate(m.closing_at)}; data is being deleted.</p>}
        {trialReuseText(m.trial_ended_reason) && <p className="mt-2 text-[14px]">Free trial ended early: {trialReuseText(m.trial_ended_reason)}.</p>}
      </div>

      <section>
        <h2 className="text-[18px] font-bold">Plan</h2>
        <AdminAccounts initial={[m]} canBill={canBill} />
      </section>

      <div className="grid gap-6 lg:grid-cols-2">
        <section className="card space-y-3">
          <h2 className="text-[18px] font-bold">Actions and notes</h2>
          <MemberActions id={m.id} email={m.email} suspended={suspended} protectedAcct={protectedAcct} />
          <ul className="space-y-2 text-[14px]">
            {m.notes.map((n) => (
              <li key={n.id} className="rounded-lg border border-line p-2"><p className="whitespace-pre-wrap">{n.body}</p>
                <p className="text-[12px] text-muted">{n.author} · {when(n.at)}</p></li>
            ))}
          </ul>
        </section>

        <section className="card space-y-2">
          <h2 className="text-[18px] font-bold">Related accounts</h2>
          {m.related.length === 0 ? <p className="text-[14px] text-muted">None found.</p> : (
            <ul className="space-y-1 text-[14px]">
              {m.related.map((x, i) => (
                <li key={i}>{x.id ? <Link href={`/admin/members/${x.id}`} className="font-semibold text-fire">{x.email}</Link> : <span className="text-muted">{x.email}</span>}
                  <span className="text-muted"> · {x.reason}</span></li>
              ))}
            </ul>
          )}
        </section>
      </div>

      <section>
        <h2 className="text-[18px] font-bold">Linked PCs</h2>
        {m.devices.length === 0 ? <p className="text-[14px] text-muted">None.</p> : (
          <table className="mt-2 w-full text-left text-[14px]">
            <thead className="border-b border-line text-muted"><tr><th className="py-2 pr-3">PC</th><th className="pr-3">App</th><th className="pr-3">Linked</th><th className="pr-3">Last sync</th><th>Also linked to</th></tr></thead>
            <tbody className="divide-y divide-line">
              {m.devices.map((d) => (
                <tr key={d.id} className={d.revoked_at ? "text-muted" : ""}>
                  <td className="py-2 pr-3 font-semibold">{d.name}{d.revoked_at ? " (unlinked)" : ""}</td>
                  <td className="pr-3">{d.edition === "tcg" ? "TCG" : "Sports"} {d.app_version}</td>
                  <td className="pr-3">{shortDate(d.created_at)}</td>
                  <td className="pr-3">{when(d.last_seen_at)}</td>
                  <td className={d.shared_with > 0 ? "font-semibold text-bad" : ""}>{d.shared_with > 0 ? `${d.shared_with} other account${d.shared_with === 1 ? "" : "s"}` : "-"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section>
        <h2 className="text-[18px] font-bold">Sign-in history</h2>
        <table className="mt-2 w-full text-left text-[14px]">
          <thead className="border-b border-line text-muted"><tr><th className="py-2 pr-3">When</th><th className="pr-3">Result</th><th className="pr-3">Network</th><th>Browser</th></tr></thead>
          <tbody className="divide-y divide-line">
            {m.sign_ins.map((e, i) => (
              <tr key={i}>
                <td className="whitespace-nowrap py-2 pr-3">{when(e.at)}</td>
                <td className={`pr-3 ${e.success ? "text-go" : "text-bad"}`}>{e.reason === "signup" ? "Signed up" : e.success ? "Signed in" : `Failed${e.reason ? ` (${e.reason})` : ""}`}</td>
                <td className="pr-3"><Link href={`/admin/members?q=${encodeURIComponent(e.ip)}`} className="hover:text-fire">{e.ip}</Link></td>
                <td>{agent(e.user_agent)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <section>
        <h2 className="text-[18px] font-bold">Staff history</h2>
        {m.audit.length === 0 ? <p className="text-[14px] text-muted">No staff actions yet.</p> : (
          <ul className="mt-2 space-y-1 text-[14px]">
            {m.audit.map((a) => <li key={a.id}><b>{ACTIONS[a.action] ?? a.action}</b>{a.detail ? `: ${a.detail}` : ""} <span className="text-muted">· {a.actor_email} · {when(a.at)}</span></li>)}
          </ul>
        )}
      </section>
    </div>
  );
}

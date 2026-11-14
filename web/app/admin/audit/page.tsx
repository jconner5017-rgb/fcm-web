import Link from "next/link";
import { apiGet } from "@/lib/api";
import { when } from "@/lib/client";
import { ACTIONS } from "@/lib/admin";

type Entry = { id: number; actor_email: string; action: string; account_id?: string; account_email?: string; detail?: string; at: string };


export default async function Audit() {
  const r = await apiGet<{ entries: Entry[] }>("/v1/admin/audit");
  const rows = r.data?.entries ?? [];
  return (
    <div className="mt-6">
      <h1 className="text-[26px] font-bold">Audit log</h1>
      <p className="mt-1 text-[14px] text-muted">Every back office action, newest first.</p>
      <div className="mt-4 overflow-x-auto">
        <table className="w-full min-w-[720px] text-left text-[14px]">
          <thead className="border-b border-line text-muted"><tr><th className="py-2 pr-3">When</th><th className="pr-3">Staff</th><th className="pr-3">Action</th><th className="pr-3">Account</th><th>Detail</th></tr></thead>
          <tbody className="divide-y divide-line">
            {rows.length === 0 && <tr><td colSpan={5} className="py-3 text-muted">Nothing yet.</td></tr>}
            {rows.map((e) => (
              <tr key={e.id}>
                <td className="whitespace-nowrap py-2 pr-3">{when(e.at)}</td>
                <td className="pr-3">{e.actor_email}</td>
                <td className="pr-3 font-semibold">{ACTIONS[e.action] ?? e.action}</td>
                <td className="pr-3">{e.account_id ? <Link href={`/admin/members/${e.account_id}`} className="text-fire">{e.account_email || "(closed)"}</Link> : ""}</td>
                <td className="break-all text-muted">{e.action === "dismiss_alert" ? "" : e.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

"use client";

import Link from "next/link";
import { useState } from "react";
import { addonActive, send, shortDate, when } from "@/lib/client";
import type { AdminAccount } from "@/lib/types";

const PRICES = {
  web: { 6: "$39.99", 12: "$59.99" } as Record<number, string>,
  bundle: { 6: "$59.99", 12: "$89.99" } as Record<number, string>,
};
const PLAN_NAMES: Record<string, string> = { web: "Web", bundle: "Bundle", addon: "Add-on", standalone: "Standalone" };

export default function AdminAccounts({ initial, canBill }: { initial: AdminAccount[]; canBill: boolean }) {
  const [rows, setRows] = useState(initial);
  const [msg, setMsg] = useState("");

  async function addon(a: AdminAccount, months: 0 | 6 | 12, plan: "web" | "bundle" = "web") {
    let note = "";
    if (months > 0) {
      const price = PRICES[plan][months];
      const what = plan === "bundle" ? "the FCM + FCM Web bundle" : "FireCardManager Web";
      const n = prompt(`Add ${months} months of ${what} (${price}) for ${a.email}?\nNote (order number, amount):`, `${price} - `);
      if (n === null) return;
      note = n;
    } else if (!confirm(`End the web add-on for ${a.email} now? Sync stops unless their trial is still running.`)) {
      return;
    }
    const r = await send<{ web_addon_until?: string | null }>("POST", `/v1/admin/accounts/${a.id}/addon`, { months, plan, note });
    if (!r.ok) return setMsg(r.message);
    const until = r.data.web_addon_until ?? undefined;
    setRows(rows.map((x) => (x.id === a.id ? { ...x, web_addon_until: until, web_plan: months > 0 ? plan : x.web_plan, web_addon_note: note || x.web_addon_note } : x)));
    setMsg(months > 0 ? `Web add-on for ${a.email} now runs until ${shortDate(until)}.` : `Web add-on ended for ${a.email}.`);
  }

  async function closeAccount(a: AdminAccount) {
    if (prompt(`Close ${a.email}? Everything online for this account is deleted within the hour (privacy policy). Type CLOSE to confirm:`) !== "CLOSE") return;
    const r = await send("POST", `/v1/admin/accounts/${a.id}/close`);
    if (!r.ok) return setMsg(r.message);
    setRows(rows.filter((x) => x.id !== a.id));
    setMsg(`${a.email} closed; its data is deleted at the next hourly cleanup.`);
  }

  async function trial(a: AdminAccount) {
    const d = new Date(Date.now() + 14 * 86400000).toISOString().slice(0, 10);
    const until = prompt(`Extend ${a.email}'s sync trial until (YYYY-MM-DD):`, d);
    if (!until) return;
    const r = await send("POST", `/v1/admin/accounts/${a.id}/trial`, { until });
    if (!r.ok) return setMsg(r.message);
    setRows(rows.map((x) => (x.id === a.id ? { ...x, sync_trial_ends_at: until + "T23:59:59Z" } : x)));
    setMsg(`Trial for ${a.email} now runs until ${until}.`);
  }

  if (rows.length === 0) return <p className="card mt-6">No accounts found.</p>;
  return (
    <>
      {msg && <p role="status" className="mt-4">{msg}</p>}
      <div className="mt-4 overflow-x-auto">
        <table className="w-full min-w-[1000px] text-left text-[14px]">
          <thead className="border-b border-line text-muted">
            <tr>
              <th className="py-2 pr-3">Email</th>
              <th className="py-2 pr-3">Joined</th>
              <th className="py-2 pr-3">Sync</th>
              <th className="py-2 pr-3 text-right">PCs</th>
              <th className="py-2 pr-3 text-right">Cards</th>
              <th className="py-2 pr-3">Last sync</th>
              <th className="py-2"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {rows.map((a) => {
              const trialOn = new Date(a.sync_trial_ends_at).getTime() > Date.now();
              return (
                <tr key={a.id}>
                  <td className="py-2 pr-3">
                    <Link href={`/admin/members/${a.id}`} className="font-bold hover:text-fire">{a.email}</Link>
                    {a.is_admin && <span className="ml-1 text-fire">owner</span>}
                    {a.staff_role && <span className="ml-1 text-fire">{a.staff_role}</span>}
                    {a.status === "disabled" && <span className="ml-1 rounded-full bg-bad px-2 py-0.5 text-[11px] font-bold text-white">suspended</span>}
                    {a.web_addon_note && <span className="block text-[12px] text-muted">{a.web_addon_note}</span>}
                  </td>
                  <td className="py-2 pr-3">{shortDate(a.created_at)}</td>
                  <td className="py-2 pr-3">
                    {addonActive(a) ? <span className="font-semibold text-go">{PLAN_NAMES[a.web_plan || "addon"] ?? "Paid"} to {shortDate(a.web_addon_until)}</span>
                      : trialOn ? `Trial to ${shortDate(a.sync_trial_ends_at)}` : <span className="text-bad">Off{a.trial_ended_reason && a.trial_ended_reason !== "allowed" ? ` (trial reused: ${a.trial_ended_reason === "pc" ? "same PC" : a.trial_ended_reason === "email" ? "same email" : "same inventory"})` : ""}</span>}
                  </td>
                  <td className="num py-2 pr-3 text-right">{a.devices}</td>
                  <td className="num py-2 pr-3 text-right">{a.cards.toLocaleString()}</td>
                  <td className="py-2 pr-3">{when(a.last_sync)}</td>
                  <td className="py-2 text-right whitespace-nowrap">
                    {canBill && <>
                    <span className="mr-1 text-[12px] text-muted">Web</span>
                    <button type="button" onClick={() => addon(a, 6)} className="btn !px-3 !py-1 text-[13px]">+6 mo</button>
                    <button type="button" onClick={() => addon(a, 12)} className="btn ml-1 !px-3 !py-1 text-[13px]">+12 mo</button>
                    <span className="ml-3 mr-1 text-[12px] text-muted">Bundle</span>
                    <button type="button" onClick={() => addon(a, 6, "bundle")} className="btn !px-3 !py-1 text-[13px]">+6 mo</button>
                    <button type="button" onClick={() => addon(a, 12, "bundle")} className="btn ml-1 !px-3 !py-1 text-[13px]">+12 mo</button>
                    {addonActive(a) && (
                      <button type="button" onClick={() => addon(a, 0)} className="btn-ghost ml-2 !px-3 !py-1 text-[13px]">End</button>
                    )}
                    <button type="button" onClick={() => trial(a)} className="btn-ghost ml-2 !px-3 !py-1 text-[13px]">Extend trial</button>
                    {!a.is_admin && <button type="button" onClick={() => closeAccount(a)} className="btn-ghost ml-2 !px-3 !py-1 text-[13px] text-bad">Close</button>}
                    </>}
                    {!canBill && <Link href={`/admin/members/${a.id}`} className="btn-ghost !px-3 !py-1 text-[13px]">Open</Link>}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </>
  );
}

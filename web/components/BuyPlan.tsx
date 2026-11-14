"use client";

import { useState } from "react";
import { send } from "@/lib/client";

type Plan = { key: string; name: string; plan: "web" | "bundle"; months: number; cents: number };
type Payment = { session_id: string; plan: string; months: number; amount_cents: number; at: string };

/** Buy or renew FireCardManager Web (alone or with the desktop license) on
 *  FireCardManager's own checkout (Stripe), the same one the desktop's
 *  Help > Register uses. A renewal adds on to the current end date. */
export default function BuyPlan({ plans, payments, paid, active }: { plans: Plan[]; payments: Payment[]; paid: boolean; active: boolean }) {
  const [busy, setBusy] = useState("");
  const [msg, setMsg] = useState("");
  async function buy(p: Plan) {
    setBusy(p.key);
    setMsg("");
    const r = await send<{ url: string }>("POST", "/v1/billing/checkout", { plan: p.key });
    if (!r.ok) {
      setBusy("");
      return setMsg(r.message);
    }
    window.location.href = r.data.url;
  }
  const money = (c: number) => `$${(c / 100).toFixed(2)}`;
  const group = (kind: "web" | "bundle") => plans.filter((p) => p.plan === kind);
  return (
    <div className="space-y-3 text-[14px]">
      {paid && <p className="rounded-lg border border-go px-3 py-2 text-go">Thanks! Your payment went through. Your plan updates here within a minute (refresh the page).</p>}
      <div className="grid gap-3 sm:grid-cols-2">
        {([["web", "FireCardManager Web", "Everything on this website, on any device."],
          ["bundle", "Bundle: desktop + Web", "FireCardManager on your PC plus this website. Your desktop license key is emailed to you."]] as const).map(([kind, title, about]) => (
          <div key={kind} className="rounded-xl border border-line p-3">
            <p className="font-bold">{title}</p>
            <p className="text-[13px] text-muted">{about}</p>
            <div className="mt-2 flex flex-wrap gap-2">
              {group(kind).map((p) => (
                <button key={p.key} type="button" className="btn !py-1.5" disabled={!!busy} onClick={() => buy(p)}>
                  {busy === p.key ? "Opening..." : `${p.months === 12 ? "1 year" : "6 months"} - ${money(p.cents)}`}
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>
      <p className="text-[13px] text-muted">
        {active ? "Buying again adds the time on to your current end date. " : ""}Paid securely with Stripe. One payment, no subscription.
        For the bundle, link your PC first (Linked PCs) so its key can be made for it.
      </p>
      {msg && <p className="text-bad">{msg}</p>}
      {payments.length > 0 && (
        <ul className="text-[13px] text-muted">
          {payments.map((p) => <li key={p.session_id}>{new Date(p.at).toLocaleDateString()}: {plans.find((x) => x.key === p.plan)?.name ?? p.plan} - {money(p.amount_cents)}</li>)}
        </ul>
      )}
    </div>
  );
}

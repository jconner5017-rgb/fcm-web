"use client";

import { useEffect, useState } from "react";
import { send } from "@/lib/client";

type Policy = { id: string; name: string };
type Location = { key: string; name: string; city?: string; postal_code?: string };
type Settings = { fulfillment_policy_id: string; payment_policy_id: string; return_policy_id: string; location_key: string; store_name: string };
type Setup = { settings: Settings; shipping: Policy[]; payment: Policy[]; returns: Policy[]; locations: Location[] };

/** The eBay choices the Bulk Lister uses: business policies, ship-from location, store name. */
export default function EbaySettings() {
  const [setup, setSetup] = useState<Setup | null>(null);
  const [st, setSt] = useState<Settings | null>(null);
  const [zip, setZip] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ text: string; bad?: boolean } | null>(null);

  async function load() {
    const r = await send<Setup>("GET", "/v1/ebay/setup");
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    setSetup(r.data);
    const s = r.data.settings;
    // One choice of a kind is picked for you.
    const only = (list: { id?: string; key?: string }[], cur: string) => cur || (list.length === 1 ? (list[0].id ?? list[0].key ?? "") : "");
    setSt({
      fulfillment_policy_id: only(r.data.shipping, s.fulfillment_policy_id ?? ""), payment_policy_id: only(r.data.payment, s.payment_policy_id ?? ""),
      return_policy_id: only(r.data.returns, s.return_policy_id ?? ""), location_key: only(r.data.locations, s.location_key ?? ""), store_name: s.store_name ?? "",
    });
  }
  useEffect(() => { load(); }, []);

  async function addLocation() {
    setBusy(true);
    const r = await send<{ key: string }>("POST", "/v1/ebay/location", { postal_code: zip });
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    await load();
    setSt((v) => (v ? { ...v, location_key: r.data.key } : v));
    setZip("");
    setMsg({ text: "Ship-from location added. Save to use it." });
  }

  async function save() {
    if (!st) return;
    setBusy(true);
    const r = await send("PUT", "/v1/ebay/settings", st);
    setBusy(false);
    setMsg(r.ok ? { text: "eBay settings saved. The Bulk Lister can list on eBay." } : { text: r.message, bad: true });
  }

  if (!setup || !st) return <p className="text-[13px] text-muted">{msg?.text ?? "Loading your eBay business policies..."}</p>;
  const pick = (label: string, list: Policy[], k: keyof Settings) => (
    <label className="block text-[13px] font-semibold">{label}
      <select className="field" value={st[k]} onChange={(e) => setSt({ ...st, [k]: e.target.value })}>
        <option value="">Choose...</option>
        {list.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
      </select>
    </label>
  );
  const none = setup.shipping.length === 0 || setup.payment.length === 0 || setup.returns.length === 0;
  return (
    <div className="space-y-3 border-t border-line pt-3">
      <p className="text-[13px] font-semibold">Listing settings</p>
      {none && (
        <p className="text-[13px] text-bad">
          Your eBay account is missing a shipping, payment or return policy. Create them in eBay (Account &gt; Business policies), or once in
          FireCardManager on your PC, then reload this page.
        </p>
      )}
      <div className="grid gap-3 sm:grid-cols-3">
        {pick("Shipping policy", setup.shipping, "fulfillment_policy_id")}
        {pick("Payment policy", setup.payment, "payment_policy_id")}
        {pick("Return policy", setup.returns, "return_policy_id")}
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="block text-[13px] font-semibold">Ships from
          <select className="field" value={st.location_key} onChange={(e) => setSt({ ...st, location_key: e.target.value })}>
            <option value="">Choose...</option>
            {setup.locations.map((l) => <option key={l.key} value={l.key}>{[l.name || l.key, l.city, l.postal_code].filter(Boolean).join(" · ")}</option>)}
          </select>
        </label>
        <div className="flex items-end gap-2">
          <label className="block flex-1 text-[13px] font-semibold">Or add a ZIP code
            <input className="field" inputMode="numeric" value={zip} onChange={(e) => setZip(e.target.value)} placeholder="33601" />
          </label>
          <button type="button" className="btn-ghost" disabled={busy || zip.trim().length < 5} onClick={addLocation}>Add</button>
        </div>
      </div>
      <label className="block text-[13px] font-semibold">Store name <span className="font-normal text-muted">(shown at the top of your listing description)</span>
        <input className="field" value={st.store_name} onChange={(e) => setSt({ ...st, store_name: e.target.value })} placeholder="Your store name" />
      </label>
      <div className="flex flex-wrap items-center gap-3">
        <button type="button" className="btn" disabled={busy} onClick={save}>Save eBay settings</button>
        {msg && <span role="status" className={`text-[13px] ${msg.bad ? "text-bad" : "text-go"}`}>{msg.text}</span>}
      </div>
    </div>
  );
}

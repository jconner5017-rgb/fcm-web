"use client";

import { useEffect, useState } from "react";
import { send } from "@/lib/client";

export type Profile = {
  id: string; name: string; package: string; length_in: number; width_in: number; height_in: number; weight_oz: number;
  carrier_code: string; service_code: string; handling_days: number; cost: string; flat_amount: number;
};
type Location = { key: string; name: string; city?: string; postal_code?: string };
type Settings = { profiles: Profile[]; returns_30: boolean; location_key: string; store_name: string };

// eBay's domestic services (ShippingProfile.Services). Standard Envelope is envelope-only.
export const SERVICES: [string, string, string][] = [
  ["eBay Standard Envelope", "USPS", "US_eBayStandardEnvelope"], ["USPS Ground Advantage", "USPS", "USPSParcel"],
  ["USPS Priority Mail", "USPS", "USPSPriority"], ["USPS Priority Mail Express", "USPS", "USPSPriorityMailExpress"],
  ["UPS Ground", "UPS", "UPSGround"], ["UPS 3 Day Select", "UPS", "UPS3rdDay"], ["UPS 2nd Day Air", "UPS", "UPS2ndDay"],
  ["UPS Next Day Air", "UPS", "UPSNextDay"], ["FedEx Ground / Home Delivery", "FedEx", "FedExHomeDelivery"],
  ["FedEx Express Saver", "FedEx", "FedExExpressSaver"], ["FedEx 2Day", "FedEx", "FedEx2Day"], ["FedEx Standard Overnight", "FedEx", "FedExStandardOvernight"],
];
const PACKAGES: [string, string][] = [["Envelope", "Envelope"], ["BubbleMailer", "Bubble mailer"], ["RigidMailer", "Rigid mailer"], ["Box", "Box"]];

export function envelopeRate(oz: number): number | null {
  return oz <= 0 ? null : oz <= 1 ? 0.78 : oz <= 2 ? 1.07 : oz <= 3 ? 1.36 : null;
}

export function profileSummary(p: Profile): string {
  const svc = SERVICES.find((s) => s[2] === p.service_code)?.[0] ?? p.service_code;
  const pkg = PACKAGES.find((x) => x[0] === p.package)?.[1] ?? p.package;
  const size = p.package === "Envelope" ? `${p.weight_oz} oz` : `${p.length_in}×${p.width_in}×${p.height_in} in, ${p.weight_oz} oz`;
  const r = envelopeRate(p.weight_oz);
  const cost = p.cost === "Free" ? "free shipping" : p.cost === "Flat" ? `buyer pays $${p.flat_amount.toFixed(2)}`
    : p.package === "Envelope" ? (r ? `buyer pays eBay rate $${r.toFixed(2)}` : "over 3 oz: not allowed") : "buyer pays eBay calculated rate";
  return `${p.name} - ${pkg} ${size} · ${svc} · ${cost}`;
}

/** FireCardManager's eBay setup on the web: shipping profiles (eBay shipping
 *  policies are made from them when listing), returns, ship-from and store name. */
export default function EbaySettings() {
  const [locs, setLocs] = useState<Location[] | null>(null);
  const [st, setSt] = useState<Settings | null>(null);
  const [editing, setEditing] = useState<number | null>(null);
  const [zip, setZip] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ text: string; bad?: boolean } | null>(null);

  async function load() {
    const r = await send<{ settings: Settings; locations: Location[] }>("GET", "/v1/ebay/setup");
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    setLocs(r.data.locations);
    const s = r.data.settings;
    setSt({ profiles: s.profiles ?? [], returns_30: !!s.returns_30, store_name: s.store_name ?? "",
      location_key: s.location_key || (r.data.locations.length === 1 ? r.data.locations[0].key : "") });
  }
  useEffect(() => { load(); }, []);

  async function addLocation() {
    setBusy(true);
    const r = await send<{ key: string }>("POST", "/v1/ebay/location", { postal_code: zip });
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    const l = await send<{ settings: Settings; locations: Location[] }>("GET", "/v1/ebay/setup");
    if (l.ok) setLocs(l.data.locations);
    setSt((v) => (v ? { ...v, location_key: r.data.key } : v));
    setZip("");
    setMsg({ text: "Ship-from location added. Save to use it." });
  }

  async function save() {
    if (!st) return;
    setBusy(true);
    const r = await send<{ settings: Settings }>("PUT", "/v1/ebay/settings", st);
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    setSt({ ...st, profiles: r.data.settings.profiles ?? st.profiles });
    setEditing(null);
    setMsg({ text: "eBay settings saved." });
  }

  function setProfile(i: number, change: Partial<Profile>) {
    if (!st) return;
    const list = st.profiles.map((p, j) => (j === i ? { ...p, ...change } : p));
    const p = list[i];
    // Envelopes only ship with eBay Standard Envelope; packages never do.
    if (change.package === "Envelope") p.service_code = "US_eBayStandardEnvelope";
    else if (change.package && p.service_code === "US_eBayStandardEnvelope") p.service_code = "USPSParcel";
    if (change.service_code) p.carrier_code = SERVICES.find((s) => s[2] === change.service_code)?.[1] ?? "USPS";
    setSt({ ...st, profiles: list });
  }

  if (!st || !locs) return <p className="text-[13px] text-muted">{msg?.text ?? "Loading your eBay settings..."}</p>;
  const num = (v: string) => (v === "" ? 0 : Number(v));
  return (
    <div className="space-y-4 border-t border-line pt-3 text-[13px]">
      <div>
        <p className="font-semibold">Shipping profiles</p>
        <p className="text-muted">Like FireCardManager: pick one in the Bulk Lister and the eBay shipping policy is made for you. Payment and returns are set up the same way.</p>
        <ul className="mt-2 space-y-2">
          {st.profiles.map((p, i) => (
            <li key={p.id || i} className="rounded-lg border border-line p-2">
              {editing === i ? (
                <div className="grid gap-2 sm:grid-cols-4">
                  <label className="block font-semibold">Name<input className="field" value={p.name} onChange={(e) => setProfile(i, { name: e.target.value })} /></label>
                  <label className="block font-semibold">Package
                    <select className="field" value={p.package} onChange={(e) => setProfile(i, { package: e.target.value })}>
                      {PACKAGES.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
                    </select></label>
                  <label className="block font-semibold">Packed weight (oz)<input className="field num" inputMode="decimal" value={p.weight_oz} onChange={(e) => setProfile(i, { weight_oz: num(e.target.value) })} /></label>
                  <label className="block font-semibold">Handling (days)
                    <select className="field" value={p.handling_days} onChange={(e) => setProfile(i, { handling_days: Number(e.target.value) })}>
                      {[1, 2, 3].map((d) => <option key={d} value={d}>{d}</option>)}
                    </select></label>
                  {p.package !== "Envelope" && (
                    <>
                      <label className="block font-semibold">Length (in)<input className="field num" inputMode="decimal" value={p.length_in} onChange={(e) => setProfile(i, { length_in: num(e.target.value) })} /></label>
                      <label className="block font-semibold">Width (in)<input className="field num" inputMode="decimal" value={p.width_in} onChange={(e) => setProfile(i, { width_in: num(e.target.value) })} /></label>
                      <label className="block font-semibold">Height (in)<input className="field num" inputMode="decimal" value={p.height_in} onChange={(e) => setProfile(i, { height_in: num(e.target.value) })} /></label>
                    </>
                  )}
                  <label className="block font-semibold">Service
                    <select className="field" value={p.service_code} onChange={(e) => setProfile(i, { service_code: e.target.value })}>
                      {SERVICES.filter((s) => (p.package === "Envelope") === (s[2] === "US_eBayStandardEnvelope")).map(([l, , c]) => <option key={c} value={c}>{l}</option>)}
                    </select></label>
                  <label className="block font-semibold">Buyer pays
                    <select className="field" value={p.cost} onChange={(e) => setProfile(i, { cost: e.target.value })}>
                      <option value="EbayRate">{p.package === "Envelope" ? "eBay's envelope rate" : "eBay calculated rate"}</option>
                      <option value="Flat">A flat amount</option>
                      <option value="Free">Nothing (free shipping)</option>
                    </select></label>
                  {p.cost === "Flat" && (
                    <label className="block font-semibold">Amount ($)<input className="field num" inputMode="decimal" value={p.flat_amount} onChange={(e) => setProfile(i, { flat_amount: num(e.target.value) })} /></label>
                  )}
                  <div className="flex items-end gap-2 sm:col-span-4">
                    <button type="button" className="btn-ghost !py-1" onClick={() => setEditing(null)}>Done</button>
                    <button type="button" className="btn-ghost !py-1 text-bad" onClick={() => { setSt({ ...st, profiles: st.profiles.filter((_, j) => j !== i) }); setEditing(null); }}>Remove</button>
                  </div>
                </div>
              ) : (
                <div className="flex items-center gap-2">
                  <span className="flex-1">{profileSummary(p)}</span>
                  <button type="button" className="font-semibold text-link" onClick={() => setEditing(i)}>Edit</button>
                </div>
              )}
            </li>
          ))}
        </ul>
        <button type="button" className="mt-2 font-semibold text-link" onClick={() => {
          setSt({ ...st, profiles: [...st.profiles, { id: "", name: "New profile", package: "BubbleMailer", length_in: 8, width_in: 6, height_in: 1, weight_oz: 3,
            carrier_code: "USPS", service_code: "USPSParcel", handling_days: 1, cost: "EbayRate", flat_amount: 0 }] });
          setEditing(st.profiles.length);
        }}>+ Add a shipping profile</button>
      </div>

      <label className="block font-semibold">Returns
        <select className="field" value={st.returns_30 ? "30" : "none"} onChange={(e) => setSt({ ...st, returns_30: e.target.value === "30" })}>
          <option value="none">No returns</option>
          <option value="30">30-day returns, buyer pays return shipping</option>
        </select>
      </label>

      <div className="grid gap-3 sm:grid-cols-2">
        <label className="block font-semibold">Ships from
          <select className="field" value={st.location_key} onChange={(e) => setSt({ ...st, location_key: e.target.value })}>
            <option value="">Choose...</option>
            {locs.map((l) => <option key={l.key} value={l.key}>{[l.name || l.key, l.city, l.postal_code].filter(Boolean).join(" · ")}</option>)}
          </select>
        </label>
        <div className="flex items-end gap-2">
          <label className="block flex-1 font-semibold">Or add a ZIP code
            <input className="field" inputMode="numeric" value={zip} onChange={(e) => setZip(e.target.value)} placeholder="33601" />
          </label>
          <button type="button" className="btn-ghost" disabled={busy || zip.trim().length < 5} onClick={addLocation}>Add</button>
        </div>
      </div>
      <label className="block font-semibold">Store name <span className="font-normal text-muted">(shown at the top of your listing description)</span>
        <input className="field" value={st.store_name} onChange={(e) => setSt({ ...st, store_name: e.target.value })} placeholder="Your store name" />
      </label>
      <div className="flex flex-wrap items-center gap-3">
        <button type="button" className="btn" disabled={busy} onClick={save}>Save eBay settings</button>
        {msg && <span role="status" className={msg.bad ? "text-bad" : "text-go"}>{msg.text}</span>}
      </div>
    </div>
  );
}

"use client";

import { useState } from "react";
import { send, shortDate } from "@/lib/client";

export type MarketKey = { market: string; last4: string; source: "pc" | "web"; device_name?: string; updated_at: string };

/** The seller's Slabs & Singles API key, for listing from the website's Bulk Lister. */
export default function MarketKeyForm({ initial }: { initial: MarketKey | null }) {
  const [key, setKey] = useState(initial);
  const [editing, setEditing] = useState(!initial);
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const r = await send<{ keys: MarketKey[] }>("PUT", "/v1/me/market-keys/slabs", { key: value });
    setBusy(false);
    if (!r.ok) return setError(r.fields.key || r.message);
    setKey(r.data.keys.find((k) => k.market === "slabs") ?? null);
    setValue("");
    setEditing(false);
  }

  async function remove() {
    if (!confirm("Remove the Slabs & Singles key? The Bulk Lister can't list until you add one again.")) return;
    const r = await send("DELETE", "/v1/me/market-keys/slabs");
    if (!r.ok) return setError(r.message);
    setKey(null);
    setEditing(true);
  }

  return (
    <div className="mt-2 space-y-3 text-[14px]">
      <p className="text-muted">
        Lets the Bulk Lister put cards on Slabs &amp; Singles. On slabsnsingles.com go to Seller hub &gt; Connect FireCardManager, choose
        <b> For: FireCardManager Web</b>, create the key and paste it here. Use a separate key from the desktop app&apos;s, so each can be revoked on its own.
        Stored encrypted and never shown again. Your package and weight settings come from FireCardManager on your PC.
      </p>
      {key && !editing && (
        <div className="rounded-lg border border-line px-3 py-2">
          <p><b>Slabs &amp; Singles</b> key ending <span className="font-mono">{key.last4}</span></p>
          <p className="text-[13px] text-muted">
            Entered here · {shortDate(key.updated_at)}
          </p>
          <div className="mt-2 flex gap-2">
            <button type="button" className="btn-ghost !py-1 text-[13px]" onClick={() => setEditing(true)}>Use a different key</button>
            <button type="button" className="btn-ghost !py-1 text-[13px] text-bad" onClick={remove}>Remove</button>
          </div>
        </div>
      )}
      {editing && (
        <form onSubmit={save} className="space-y-3">
          <label className="block font-semibold">
            API key
            <input className="field font-mono" value={value} onChange={(e) => setValue(e.target.value)} autoComplete="off" spellCheck={false} required />
          </label>
          {error && <p role="alert" className="text-bad">{error}</p>}
          <div className="flex gap-2">
            <button type="submit" className="btn" disabled={busy}>{busy ? "Saving..." : "Save key"}</button>
            {key && <button type="button" className="btn-ghost" onClick={() => setEditing(false)}>Cancel</button>}
          </div>
        </form>
      )}
    </div>
  );
}

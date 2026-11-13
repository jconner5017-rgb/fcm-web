"use client";

import { useState } from "react";
import { send, shortDate } from "@/lib/client";
import EbaySettings from "./EbaySettings";

export type EbayStatus = { configured: boolean; connected: boolean; user?: string; connected_at?: string; expires_at?: string };

/** Connect eBay: the seller signs in on eBay and approves FireCardManager Web. */
export default function EbayConnect({ initial }: { initial: EbayStatus }) {
  const [st, setSt] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function connect() {
    setBusy(true);
    setError("");
    const r = await send<{ url: string }>("POST", "/v1/ebay/connect");
    if (!r.ok) { setBusy(false); return setError(r.message); }
    window.location.href = r.data.url;
  }

  async function disconnect() {
    if (!confirm("Disconnect eBay from FireCardManager Web? Your eBay listings stay up; the web app just can't list, change or end them until you connect again. FireCardManager on your PC isn't affected.")) return;
    setBusy(true);
    const r = await send("DELETE", "/v1/ebay");
    setBusy(false);
    if (!r.ok) return setError(r.message);
    setSt({ ...st, connected: false, user: undefined });
  }

  if (!st.configured) return <p className="mt-2 text-[14px] text-muted">eBay on FireCardManager Web is coming soon. Until then, list on eBay from FireCardManager on your PC.</p>;
  return (
    <div className="mt-2 space-y-3 text-[14px]">
      {st.connected ? (
        <>
          <p>
            Connected{st.user ? <> as <b>{st.user}</b></> : ""}{st.connected_at ? ` on ${shortDate(st.connected_at)}` : ""}.
            {st.expires_at && <span className="text-muted"> eBay asks you to connect again by {shortDate(st.expires_at)}.</span>}
          </p>
          <div className="flex flex-wrap gap-2">
            <button type="button" className="btn-ghost" disabled={busy} onClick={connect}>Connect again</button>
            <button type="button" className="btn-ghost text-bad" disabled={busy} onClick={disconnect}>Disconnect</button>
          </div>
          <EbaySettings />
        </>
      ) : (
        <>
          <p className="text-muted">
            Lets FireCardManager Web list your cards on eBay, change or end listings, and record your eBay sales. You sign in on eBay itself;
            we never see your eBay password. This is separate from the eBay connection in FireCardManager on your PC.
          </p>
          <button type="button" className="btn" disabled={busy} onClick={connect}>{busy ? "Opening eBay..." : "Connect eBay"}</button>
        </>
      )}
      {error && <p role="alert" className="text-bad">{error}</p>}
    </div>
  );
}

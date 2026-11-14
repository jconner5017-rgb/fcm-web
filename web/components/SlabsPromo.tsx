"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

export const SLABS_CODE = "WELCOME25";

/** "List on Slabs & Singles, free" card with the new-seller code, like the
 *  desktop Dashboard's. Shown while nothing is live there; "Not now" hides
 *  it for 30 days in this browser. */
export default function SlabsPromo({ ready }: { ready: number }) {
  const [show, setShow] = useState(false);
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    try { setShow(Number(localStorage.getItem("fcm-slabs-promo-hide") ?? 0) < Date.now()); } catch { setShow(true); }
  }, []);
  if (!show) return null;
  const title = ready > 0 ? `List your ${ready.toLocaleString()} card${ready === 1 ? "" : "s"} on Slabs & Singles, free` : "List your cards on Slabs & Singles, free";
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-xl border border-link bg-wash px-4 py-3">
      <div className="min-w-[260px] flex-1">
        <p className="font-bold">{title}</p>
        <p className="text-[13px] text-muted">
          Our US card marketplace. Free to list, you pay only when a card sells, shipping is picked for you, and a card that sells on eBay or
          Slabs &amp; Singles comes off the other automatically. New sellers: use code{" "}
          <button type="button" title="Copy the code" className="rounded border border-dashed border-fire px-1.5 font-mono font-bold text-fire"
            onClick={() => { navigator.clipboard?.writeText(SLABS_CODE).then(() => setCopied(true)).catch(() => {}); }}>{SLABS_CODE}</button>
          {copied ? " (copied)" : ""} for your first <b>25 sales fee-free</b>.
        </p>
      </div>
      <div className="flex flex-wrap gap-2">
        <button type="button" className="btn-ghost !py-1.5" onClick={() => { setShow(false); try { localStorage.setItem("fcm-slabs-promo-hide", String(Date.now() + 30 * 86400000)); } catch {} }}>Not now</button>
        <a className="btn-ghost !py-1.5" href={`https://slabsnsingles.com/sell?promo=${SLABS_CODE}`} target="_blank" rel="noreferrer">Use the code</a>
        <Link className="btn !py-1.5" href="/editor">List on Slabs &amp; Singles</Link>
      </div>
    </div>
  );
}

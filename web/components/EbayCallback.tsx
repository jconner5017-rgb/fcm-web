"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { send } from "@/lib/client";

/** eBay sends the seller back here after they approve FireCardManager Web. */
export default function EbayCallback({ code, state }: { code: string; state: string }) {
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);
  const once = useRef(false);
  useEffect(() => {
    if (once.current) return;
    once.current = true;
    if (!code || !state) {
      setResult({ ok: false, text: "eBay didn't send the sign-in back. If you chose not to allow access, that's fine: nothing changed." });
      return;
    }
    send<{ user: string }>("POST", "/v1/ebay/callback", { code, state }).then((r) => {
      if (r.ok) {
        setResult({ ok: true, text: r.data.user ? `eBay is connected as ${r.data.user}.` : "eBay is connected." });
        setTimeout(() => { window.location.href = "/account#ebay"; }, 1500);
      } else if (r.status === 401) {
        setResult({ ok: false, text: "Sign in to FireCardManager Web first, then click Connect eBay on the Account page again." });
      } else setResult({ ok: false, text: r.message });
    });
  }, [code, state]);

  return (
    <div className="mx-auto mt-12 max-w-[520px]">
      <div className="card space-y-3">
        <h1 className="text-[22px] font-bold">eBay</h1>
        {!result && <p>Connecting your eBay account...</p>}
        {result && <p className={result.ok ? "text-go" : "text-bad"}>{result.text}</p>}
        <Link href="/account#ebay" className="btn-ghost inline-flex">Back to Account</Link>
      </div>
    </div>
  );
}

"use client";

import { useState } from "react";
import { send } from "@/lib/client";

/** Close the account: everything online is deleted (privacy policy). The desktop apps keep working. */
export default function CloseAccount() {
  const [open, setOpen] = useState(false);
  const [pw, setPw] = useState("");
  const [sure, setSure] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function close(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const r = await send("POST", "/v1/me/close", { password: pw });
    setBusy(false);
    if (!r.ok) return setError(r.fields.password || r.message);
    window.location.href = "/signin?closed=1";
  }

  if (!open) return (
    <div className="mt-2 space-y-2 text-[14px]">
      <p className="text-muted">Deletes your online inventory, photos, Sold Log, saved keys and eBay connection from our servers. FireCardManager on your PC keeps working with the cards on your PC.</p>
      <button type="button" className="btn-ghost text-bad" onClick={() => setOpen(true)}>Close my account...</button>
    </div>
  );
  return (
    <form onSubmit={close} className="mt-2 space-y-3 text-[14px]">
      <p>This can&apos;t be undone. You&apos;re signed out everywhere, linked PCs stop syncing, and everything stored online for this account is deleted (within a day; backups roll off within 30 days).</p>
      <label className="block font-semibold">Your password
        <input className="field" type="password" autoComplete="current-password" value={pw} onChange={(e) => setPw(e.target.value)} required />
      </label>
      <label className="flex items-center gap-2"><input type="checkbox" checked={sure} onChange={(e) => setSure(e.target.checked)} /> Yes, delete my FireCardManager Web account and its data</label>
      {error && <p role="alert" className="text-bad">{error}</p>}
      <div className="flex gap-2">
        <button type="submit" className="btn !bg-bad" disabled={busy || !sure || !pw}>{busy ? "Closing..." : "Close account"}</button>
        <button type="button" className="btn-ghost" onClick={() => { setOpen(false); setPw(""); setSure(false); }}>Keep my account</button>
      </div>
    </form>
  );
}

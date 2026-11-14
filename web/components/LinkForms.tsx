"use client";

import { useState } from "react";
import { editionName, send, shortDate } from "@/lib/client";
import type { PendingLink } from "@/lib/types";

const LICENSE: Record<string, string> = {
  valid: "Licensed",
  no_expiry: "Licensed (no expiry)",
  grace: "License just expired (grace period)",
  trial: "Desktop free trial",
};

export function ApproveLink({ link, email }: { link: PendingLink; email: string }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const [trialEnded, setTrialEnded] = useState("");

  async function approve() {
    setBusy(true);
    setError("");
    const r = await send<{ approved: boolean; trial_ended?: string }>("POST", "/v1/devices/link/approve", { user_code: link.user_code });
    setBusy(false);
    if (!r.ok) return setError(r.message);
    setTrialEnded(r.data.trial_ended ?? "");
    setDone(true);
  }

  if (done) {
    return (
      <div className="mt-6 space-y-3">
        <p className="text-[18px] font-bold text-go">Approved</p>
        <p>Go back to FireCardManager on <b>{link.device_name}</b>. It finishes linking by itself in a few seconds{trialEnded ? "" : " and starts syncing"}.</p>
        {trialEnded && <p className="text-bad">This PC already had a FireCardManager Web free trial on another account, so this account has no free trial. Syncing starts once FireCardManager Web is added to the account.</p>}
      </div>
    );
  }
  return (
    <div className="mt-6 space-y-4">
      <dl className="card grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
        <dt className="text-muted">Code</dt><dd className="num font-mono font-bold tracking-widest">{link.user_code}</dd>
        <dt className="text-muted">PC</dt><dd className="font-semibold">{link.device_name}</dd>
        <dt className="text-muted">App</dt><dd>{editionName(link.edition)}</dd>
        <dt className="text-muted">License</dt>
        <dd>{LICENSE[link.license_state] ?? link.license_state}{link.license_expires_at ? `, renews ${shortDate(link.license_expires_at)}` : ""}</dd>
        <dt className="text-muted">Account</dt><dd>{email}</dd>
      </dl>
      <p className="text-[14px] text-muted">Only approve a code you see on your own PC right now. Approving lets that PC read and change this account&apos;s inventory.</p>
      {error && <p role="alert" className="text-bad">{error}</p>}
      <div className="flex gap-3">
        <button type="button" onClick={approve} disabled={busy} className="btn">{busy ? "Approving…" : "Approve"}</button>
        <a href="/" className="btn-ghost">Cancel</a>
      </div>
    </div>
  );
}

export function EnterCode() {
  const [code, setCode] = useState("");
  return (
    <form action="/link" method="get" className="mt-6 space-y-3">
      <label htmlFor="code" className="block font-semibold">Code</label>
      <input id="code" name="code" value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} placeholder="ABCD-2345"
        maxLength={9} autoComplete="off" className="field num font-mono text-[20px] tracking-widest" />
      <button type="submit" disabled={code.replace("-", "").length < 8} className="btn w-full">Continue</button>
    </form>
  );
}

"use client";

import { useState } from "react";
import QRCode from "qrcode";
import { send } from "@/lib/client";

export function ChangePassword() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [msg, setMsg] = useState("");
  const [fields, setFields] = useState<Record<string, string>>({});
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setMsg("");
    setFields({});
    const r = await send("POST", "/v1/me/password", { current_password: current, password: next });
    if (!r.ok) {
      setFields(r.fields);
      return setMsg(r.message);
    }
    setCurrent("");
    setNext("");
    setMsg("Password changed. Other devices were signed out.");
  }
  return (
    <form onSubmit={submit} className="mt-3 space-y-3">
      <div>
        <label htmlFor="cur" className="block font-semibold">Current password</label>
        <input id="cur" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} className="field" />
        {fields.current_password && <p className="mt-1 text-[13px] text-bad">{fields.current_password}</p>}
      </div>
      <div>
        <label htmlFor="new" className="block font-semibold">New password</label>
        <input id="new" type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} className="field" />
        {fields.password && <p className="mt-1 text-[13px] text-bad">{fields.password}</p>}
      </div>
      {msg && <p role="status">{msg}</p>}
      <button type="submit" className="btn">Change password</button>
    </form>
  );
}

export function TwoFactor({ enabled }: { enabled: boolean }) {
  const [on, setOn] = useState(enabled);
  const [stage, setStage] = useState<"idle" | "scan" | "codes" | "off">("idle");
  const [secret, setSecret] = useState("");
  const [qr, setQr] = useState("");
  const [code, setCode] = useState("");
  const [backups, setBackups] = useState<string[]>([]);
  const [error, setError] = useState("");

  async function start() {
    setError("");
    const r = await send<{ secret: string; otpauth_url: string }>("POST", "/v1/me/mfa/setup");
    if (!r.ok) return setError(r.message);
    setSecret(r.data.secret);
    setQr(await QRCode.toDataURL(r.data.otpauth_url, { margin: 1, width: 220 }));
    setStage("scan");
  }
  async function confirmCode(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    const r = await send<{ backup_codes: string[] }>("POST", "/v1/me/mfa/confirm", { code });
    if (!r.ok) return setError(cap(r.message));
    setBackups(r.data.backup_codes);
    setOn(true);
    setCode("");
    setStage("codes");
  }
  async function turnOff(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    const r = await send("POST", "/v1/me/mfa/disable", { code });
    if (!r.ok) return setError(cap(r.message));
    setOn(false);
    setCode("");
    setStage("idle");
  }
  function download() {
    const text = `FireCardManager Web backup codes\nEach code works once.\n\n${backups.join("\n")}\n`;
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "firecardmanager-backup-codes.txt";
    a.click();
    URL.revokeObjectURL(url);
  }

  if (stage === "codes") {
    return (
      <div className="mt-3 space-y-3">
        <p className="font-semibold text-go">Two-factor sign-in is on.</p>
        <p className="text-muted">Save these backup codes. If you lose your phone, each one gets you in once. They won&apos;t be shown again.</p>
        <ul className="num grid grid-cols-2 gap-2 font-mono">
          {backups.map((b) => <li key={b} className="rounded bg-paper px-2 py-1 text-center">{b}</li>)}
        </ul>
        <div className="flex gap-2">
          <button type="button" onClick={download} className="btn-ghost">Download</button>
          <button type="button" onClick={() => setStage("idle")} className="btn">I saved them</button>
        </div>
      </div>
    );
  }
  if (stage === "scan") {
    return (
      <form onSubmit={confirmCode} className="mt-3 space-y-3">
        <ol className="list-decimal space-y-2 pl-5">
          <li>Open Google Authenticator, Authy, 1Password or another authenticator app.</li>
          <li>
            Scan this code, or type the key by hand.
            {/* eslint-disable-next-line @next/next/no-img-element */}
            {qr && <img src={qr} alt="QR code for your authenticator app" width={220} height={220} className="mt-2 rounded bg-white p-1" />}
            <code className="mt-2 block break-all text-[13px]">{secret.match(/.{1,4}/g)?.join(" ")}</code>
          </li>
          <li>Enter the 6-digit code it shows.</li>
        </ol>
        <input value={code} onChange={(e) => setCode(e.target.value)} inputMode="numeric" autoComplete="one-time-code" maxLength={6}
          className="field num text-[20px] tracking-[0.3em]" aria-label="Code" />
        {error && <p role="alert" className="text-bad">{error}</p>}
        <button type="submit" disabled={code.length < 6} className="btn">Turn on</button>
      </form>
    );
  }
  if (stage === "off") {
    return (
      <form onSubmit={turnOff} className="mt-3 space-y-3">
        <p>Enter a code from your authenticator app (or a backup code) to turn two-factor sign-in off.</p>
        <input value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" maxLength={12} className="field num" aria-label="Code" />
        {error && <p role="alert" className="text-bad">{error}</p>}
        <div className="flex gap-2">
          <button type="submit" className="btn">Turn off</button>
          <button type="button" onClick={() => setStage("idle")} className="btn-ghost">Cancel</button>
        </div>
      </form>
    );
  }
  return (
    <div className="mt-3 space-y-3">
      <p className="text-muted">
        {on ? "On. Signing in asks for a code from your authenticator app." : "Off. Turn it on so a stolen password alone can't get into your inventory."}
      </p>
      {error && <p role="alert" className="text-bad">{error}</p>}
      {on ? (
        <button type="button" onClick={() => setStage("off")} className="btn-ghost">Turn off</button>
      ) : (
        <button type="button" onClick={start} className="btn">Turn on two-factor sign-in</button>
      )}
    </div>
  );
}

function cap(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

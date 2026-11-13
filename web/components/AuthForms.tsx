"use client";

import Link from "next/link";
import { useState } from "react";
import { ADDON_PRICE, safeNext, send } from "@/lib/client";

function Field(props: { id: string; label: string; type?: string; value: string; onChange: (v: string) => void; error?: string; autoComplete?: string; hint?: string }) {
  return (
    <div>
      <label htmlFor={props.id} className="block font-semibold">{props.label}</label>
      <input id={props.id} type={props.type ?? "text"} value={props.value} autoComplete={props.autoComplete}
        onChange={(e) => props.onChange(e.target.value)} className="field" aria-invalid={!!props.error} />
      {props.hint && !props.error && <p className="mt-1 text-[13px] text-muted">{props.hint}</p>}
      {props.error && <p className="mt-1 text-[13px] text-bad">{props.error}</p>}
    </div>
  );
}

export function SignUpForm({ next }: { next?: string }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [fields, setFields] = useState<Record<string, string>>({});

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    setFields({});
    const r = await send("POST", "/v1/auth/signup", { email, password });
    setBusy(false);
    if (!r.ok) {
      setError(r.message);
      setFields(r.fields);
      return;
    }
    window.location.href = safeNext(next, "/");
  }

  return (
    <form onSubmit={submit} className="mt-6 space-y-4">
      <Field id="email" label="Email" type="email" autoComplete="email" value={email} onChange={setEmail} error={fields.email} />
      <Field id="password" label="Password" type="password" autoComplete="new-password" value={password} onChange={setPassword}
        error={fields.password} hint="At least 10 characters." />
      {error && <p role="alert" className="text-bad">{error}</p>}
      <button type="submit" disabled={busy} className="btn w-full">{busy ? "Creating…" : "Create account"}</button>
      <p className="text-[13px] text-muted">
        Your first 30 days of sync are free. After that, cloud sync is the web add-on: {ADDON_PRICE}. The desktop app keeps working either way.
      </p>
      <p className="text-[14px]">Already have an account? <Link href={next ? `/signin?next=${encodeURIComponent(next)}` : "/signin"} className="font-semibold text-fire">Sign in</Link></p>
    </form>
  );
}

export function SignInForm({ next, startWithCode }: { next?: string; startWithCode?: boolean }) {
  const [step, setStep] = useState<"password" | "code">(startWithCode ? "code" : "password");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const done = () => (window.location.href = safeNext(next, "/"));

  async function submitPassword(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const r = await send<{ mfa_required?: boolean }>("POST", "/v1/auth/login", { email, password });
    setBusy(false);
    if (!r.ok) return setError(r.message);
    if (r.data.mfa_required) return setStep("code");
    done();
  }

  async function submitCode(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const r = await send<{ used_backup: boolean; backups_left: number }>("POST", "/v1/auth/mfa", { code });
    setBusy(false);
    if (!r.ok) {
      if (r.code === "unauthorized") setStep("password");
      return setError(r.message.charAt(0).toUpperCase() + r.message.slice(1));
    }
    if (r.data.used_backup && r.data.backups_left <= 3) {
      alert(`You have ${r.data.backups_left} backup codes left. Make new ones on the Account page.`);
    }
    done();
  }

  if (step === "code") {
    return (
      <form onSubmit={submitCode} className="mt-6 space-y-4">
        <p className="text-muted">Enter the 6-digit code from your authenticator app, or one of your backup codes.</p>
        <div>
          <label htmlFor="code" className="block font-semibold">Code</label>
          <input id="code" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" autoFocus maxLength={12}
            className="field num text-[20px] tracking-[0.3em]" />
        </div>
        {error && <p role="alert" className="text-bad">{error}</p>}
        <button type="submit" disabled={busy || code.trim().length < 6} className="btn w-full">{busy ? "Checking…" : "Sign in"}</button>
      </form>
    );
  }

  return (
    <form onSubmit={submitPassword} className="mt-6 space-y-4">
      <Field id="email" label="Email" type="email" autoComplete="email" value={email} onChange={setEmail} />
      <Field id="password" label="Password" type="password" autoComplete="current-password" value={password} onChange={setPassword} />
      {error && <p role="alert" className="text-bad">{error}</p>}
      <button type="submit" disabled={busy} className="btn w-full">{busy ? "Signing in…" : "Sign in"}</button>
      <div className="flex flex-wrap justify-between gap-2 text-[14px]">
        <Link href="/forgot-password" className="text-fire">Forgot your password?</Link>
        <Link href={next ? `/signup?next=${encodeURIComponent(next)}` : "/signup"} className="font-semibold text-fire">Create an account</Link>
      </div>
    </form>
  );
}

export function ForgotForm() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    const r = await send("POST", "/v1/auth/forgot-password", { email });
    setBusy(false);
    if (!r.ok) return setError(r.message);
    setSent(true);
  }
  if (sent) return <p className="mt-6">If that email has an account, a reset link is on its way. It works for one hour.</p>;
  return (
    <form onSubmit={submit} className="mt-6 space-y-4">
      <Field id="email" label="Email" type="email" autoComplete="email" value={email} onChange={setEmail} />
      {error && <p role="alert" className="text-bad">{error}</p>}
      <button type="submit" disabled={busy} className="btn w-full">Email me a reset link</button>
    </form>
  );
}

export function ResetForm({ token }: { token: string }) {
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [field, setField] = useState("");
  const [done, setDone] = useState(false);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const r = await send("POST", "/v1/auth/reset-password", { token, password });
    setBusy(false);
    if (!r.ok) {
      setError(r.message);
      setField(r.fields.password ?? "");
      return;
    }
    setDone(true);
  }
  if (done) return <p className="mt-6">Password changed. <Link href="/signin" className="font-semibold text-fire">Sign in</Link> with your new password.</p>;
  return (
    <form onSubmit={submit} className="mt-6 space-y-4">
      <Field id="password" label="New password" type="password" autoComplete="new-password" value={password} onChange={setPassword}
        error={field} hint="At least 10 characters." />
      {error && <p role="alert" className="text-bad">{error}</p>}
      <button type="submit" disabled={busy || !token} className="btn w-full">Set new password</button>
    </form>
  );
}

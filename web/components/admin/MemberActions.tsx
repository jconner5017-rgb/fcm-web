"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { send } from "@/lib/client";

/** Support actions on a member: suspend / unsuspend, sign out everywhere, add a note. */
export default function MemberActions({ id, email, suspended, banned, canBan, protectedAcct }: { id: string; email: string; suspended: boolean; banned: boolean; canBan: boolean; protectedAcct: boolean }) {
  const router = useRouter();
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ text: string; bad?: boolean } | null>(null);

  async function act(path: string, body?: object, done?: string) {
    setBusy(true);
    const r = await send("POST", `/v1/admin/accounts/${id}/${path}`, body);
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    setMsg({ text: done ?? "Done." });
    router.refresh();
  }

  return (
    <div className="space-y-3">
      {!protectedAcct && (
        <div className="flex flex-wrap gap-2">
          {banned ? (
            canBan && <button type="button" className="btn" disabled={busy} onClick={() => confirm(`Lift the ban on ${email}? They can sign in again, and their email, networks and PCs are no longer blocked.`) && act("unban", undefined, "Ban lifted.")}>Lift ban</button>
          ) : suspended ? (
            <button type="button" className="btn" disabled={busy} onClick={() => act("unsuspend", undefined, `${email} can sign in and sync again.`)}>Unsuspend</button>
          ) : (
            <button type="button" className="btn-ghost text-bad" disabled={busy} onClick={() => {
              const reason = prompt(`Suspend ${email}? They're signed out, can't sign in, and their PCs stop syncing. Reason (staff only):`);
              if (reason) act("suspend", { reason }, `${email} is suspended.`);
            }}>Suspend</button>
          )}
          {!banned && canBan && (
            <button type="button" className="btn-ghost text-bad" disabled={busy} onClick={() => {
              const reason = prompt(`Ban ${email}? They're suspended, and their email can't sign up again, their PCs can't be linked to any account, and new accounts from their networks raise an alert. Reason (staff only):`);
              if (reason) act("ban", { reason }, `${email} is banned.`);
            }}>Ban</button>
          )}
          <button type="button" className="btn-ghost" disabled={busy} onClick={() => confirm(`Sign ${email} out of every browser?`) && act("signout", undefined, "Signed out everywhere.")}>Sign out everywhere</button>
        </div>
      )}
      <form className="space-y-2" onSubmit={(e) => { e.preventDefault(); if (note.trim()) act("notes", { body: note }, "Note added.").then(() => setNote("")); }}>
        <textarea className="field h-20" value={note} onChange={(e) => setNote(e.target.value)} placeholder="Staff note (the member never sees it)" />
        <button type="submit" className="btn-ghost" disabled={busy || !note.trim()}>Add note</button>
      </form>
      {msg && <p role="status" className={msg.bad ? "text-bad" : "text-go"}>{msg.text}</p>}
    </div>
  );
}

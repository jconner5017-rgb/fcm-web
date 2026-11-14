"use client";

import { useState } from "react";
import { send } from "@/lib/client";

type Staff = { id: string; email: string; role: "owner" | "manager" | "support"; mfa_enabled: boolean };

/** Owners grant back office access to existing accounts. */
export default function StaffManager({ initial }: { initial: Staff[] }) {
  const [rows, setRows] = useState(initial);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<"support" | "manager">("support");
  const [msg, setMsg] = useState<{ text: string; bad?: boolean } | null>(null);

  async function set(e: string, r: string) {
    const res = await send("PUT", "/v1/admin/staff", { email: e, role: r });
    if (!res.ok) return setMsg({ text: res.message, bad: true });
    const list = await send<{ staff: Staff[] }>("GET", "/v1/admin/staff");
    if (list.ok) setRows(list.data.staff);
    setMsg({ text: r ? `${e} now has ${r} access.` : `Back office access removed for ${e}.` });
    setEmail("");
  }

  return (
    <div className="mt-4 space-y-4">
      <div className="card space-y-2 text-[14px]">
        <p><b>Support:</b> sees members, alerts and the audit log; adds notes, suspends and signs members out.</p>
        <p><b>Manager:</b> all of that, plus adding plan months, setting trials and closing accounts.</p>
        <p><b>Owner:</b> set on the server (ADMIN_EMAILS); everything, including this page. Everyone needs two-factor sign-in on.</p>
      </div>
      <form className="flex flex-wrap items-end gap-2" onSubmit={(e) => { e.preventDefault(); set(email, role); }}>
        <label className="block font-semibold">Account email<input className="field" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required placeholder="They sign up first" /></label>
        <label className="block font-semibold">Role
          <select className="field" value={role} onChange={(e) => setRole(e.target.value as "support" | "manager")}>
            <option value="support">Support</option><option value="manager">Manager</option>
          </select></label>
        <button type="submit" className="btn">Give access</button>
      </form>
      {msg && <p role="status" className={msg.bad ? "text-bad" : "text-go"}>{msg.text}</p>}
      <table className="w-full text-left text-[14px]">
        <thead className="border-b border-line text-muted"><tr><th className="py-2 pr-3">Email</th><th className="pr-3">Role</th><th className="pr-3">2FA</th><th></th></tr></thead>
        <tbody className="divide-y divide-line">
          {rows.map((s) => (
            <tr key={s.id}>
              <td className="py-2 pr-3 font-semibold">{s.email}</td>
              <td className="pr-3 capitalize">{s.role}</td>
              <td className="pr-3">{s.mfa_enabled ? <span className="text-go">On</span> : <span className="text-bad">Off (no access until on)</span>}</td>
              <td className="text-right">
                {s.role !== "owner" && (
                  <>
                    <button type="button" className="btn-ghost !py-1 text-[13px]" onClick={() => set(s.email, s.role === "support" ? "manager" : "support")}>
                      Make {s.role === "support" ? "manager" : "support"}</button>
                    <button type="button" className="btn-ghost ml-1 !py-1 text-[13px] text-bad" onClick={() => confirm(`Remove back office access for ${s.email}?`) && set(s.email, "")}>Remove</button>
                  </>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

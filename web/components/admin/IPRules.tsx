"use client";

import { useState } from "react";
import { send, shortDate } from "@/lib/client";

type Rule = { id: number; cidr: string; label: string; created_by: string; created_at: string };
type Data = { rules: Rule[]; your_ip: string; enforced: boolean };

/** Networks the back office may be used from (like Slabs & Singles). */
export default function IPRules({ initial }: { initial: Data }) {
  const [d, setD] = useState(initial);
  const [cidr, setCidr] = useState("");
  const [label, setLabel] = useState("");
  const [msg, setMsg] = useState<{ text: string; bad?: boolean } | null>(null);

  async function reload() {
    const r = await send<Data>("GET", "/v1/admin/ip-rules");
    if (r.ok) setD(r.data);
  }
  async function add(c: string, l: string) {
    const r = await send("POST", "/v1/admin/ip-rules", { cidr: c, label: l });
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    setCidr(""); setLabel("");
    setMsg({ text: "Network added." });
    reload();
  }
  async function remove(x: Rule) {
    if (!confirm(`Remove ${x.cidr}? Staff on that network lose back office access.`)) return;
    const r = await send("DELETE", `/v1/admin/ip-rules/${x.id}`);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    reload();
  }

  return (
    <section className="mt-8 space-y-3">
      <h2 className="text-[20px] font-bold">Back office networks</h2>
      <p className="text-[14px] text-muted">
        With none listed, the back office works from any network (two-step sign-in is still required). Once you add one, it only works from
        the networks listed. You&apos;re on <b>{d.your_ip}</b>.{!d.enforced && " The list is switched off on the server right now (BACKOFFICE_IP_ALLOWLIST=off)."}
      </p>
      <form className="flex flex-wrap items-end gap-2" onSubmit={(e) => { e.preventDefault(); add(cidr, label); }}>
        <label className="block font-semibold">Address or range<input className="field" value={cidr} onChange={(e) => setCidr(e.target.value)} placeholder="203.0.113.7 or 203.0.113.0/24" required /></label>
        <label className="block font-semibold">Label<input className="field" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Home" /></label>
        <button type="submit" className="btn">Add</button>
        {!d.rules.some((r) => r.cidr.startsWith(d.your_ip + "/")) && (
          <button type="button" className="btn-ghost" onClick={() => add(d.your_ip, "My network")}>Add my current network</button>
        )}
      </form>
      {msg && <p role="status" className={msg.bad ? "text-bad" : "text-go"}>{msg.text}</p>}
      {d.rules.length > 0 && (
        <table className="w-full text-left text-[14px]">
          <thead className="border-b border-line text-muted"><tr><th className="py-2 pr-3">Network</th><th className="pr-3">Label</th><th className="pr-3">Added</th><th></th></tr></thead>
          <tbody className="divide-y divide-line">
            {d.rules.map((x) => (
              <tr key={x.id}>
                <td className="py-2 pr-3 font-mono">{x.cidr}</td><td className="pr-3">{x.label}</td>
                <td className="pr-3 text-muted">{shortDate(x.created_at)} · {x.created_by}</td>
                <td className="text-right"><button type="button" className="btn-ghost !py-1 text-[13px] text-bad" onClick={() => remove(x)}>Remove</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

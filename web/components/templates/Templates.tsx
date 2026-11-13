"use client";

import { useState } from "react";
import { send } from "@/lib/client";

export type Template = { id: string; edition: string; source: "pc" | "web"; name: string; title_template: string; description_html: string; is_default: boolean };

const SAMPLE: Record<string, string> = {
  "{StoreName}": "", "{PlayerName}": "Victor Wembanyama", "{Team}": "San Antonio Spurs", "{Year}": "2023-24", "{Brand}": "Panini", "{SetName}": "Prizm",
  "{CardNumber}": "136", "{Variety}": "Silver", "{Condition}": "Raw - NM", "{Sport}": "Basketball", "{EstimatedValue}": "$40.00", "{BoxLabel}": "",
  "{FrontImageUrl}": "/icon.svg", "{BackImageUrl}": "/icon.svg",
};
const TOKENS = Object.keys(SAMPLE).filter((k) => k !== "{BoxLabel}");

function fill(t: string, store: string): string {
  let out = t;
  for (const [k, v] of Object.entries(SAMPLE)) out = out.split(k).join(k === "{StoreName}" ? store || "Your Store Name" : v);
  return out;
}

/** eBay listing templates: the ones from FireCardManager on your PC (read-only here) and your own. */
export default function Templates({ edition, initial, defaults, store }: {
  edition: string; initial: Template[]; defaults: { title: string; description: string }; store: string;
}) {
  const [list, setList] = useState(initial);
  const [sel, setSel] = useState<string>(initial[0]?.id ?? "builtin");
  const [draft, setDraft] = useState<Template | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ text: string; bad?: boolean } | null>(null);

  const builtin: Template = { id: "builtin", edition, source: "pc", name: "Store Branded (built in)", title_template: defaults.title, description_html: defaults.description, is_default: false };
  const all = [...list, ...(list.length ? [] : [builtin])];
  const cur = draft ?? all.find((t) => t.id === sel) ?? builtin;

  function copy(t: Template) {
    setDraft({ ...t, id: "", source: "web", name: t.name.replace(" (built in)", "") + " (copy)", is_default: false });
    setMsg(null);
  }

  async function save() {
    if (!draft) return;
    setBusy(true);
    const r = draft.id
      ? await send<Template>("PUT", `/v1/templates/${draft.id}`, draft)
      : await send<Template>("POST", "/v1/templates", { ...draft, edition });
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    setList(draft.id ? list.map((t) => (t.id === r.data.id ? r.data : t)) : [...list, r.data]);
    setSel(r.data.id);
    setDraft(null);
    setMsg({ text: "Template saved. Pick it in the Bulk Lister when listing on eBay." });
  }

  async function remove(t: Template) {
    if (!confirm(`Delete the template "${t.name}"?`)) return;
    const r = await send("DELETE", `/v1/templates/${t.id}`);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    setList(list.filter((x) => x.id !== t.id));
    setSel(list.find((x) => x.id !== t.id)?.id ?? "builtin");
  }

  return (
    <div className="grid gap-5 lg:grid-cols-[300px_minmax(0,1fr)]">
      <aside className="space-y-2">
        <ul className="space-y-1">
          {all.map((t) => (
            <li key={t.id}>
              <button type="button" onClick={() => { setSel(t.id); setDraft(null); setMsg(null); }}
                className={`w-full rounded-lg border px-3 py-2 text-left text-[14px] ${!draft && sel === t.id ? "border-fire" : "border-line hover:bg-wash"}`}>
                <span className="font-semibold">{t.is_default ? "★ " : ""}{t.name}</span>
                <span className="block text-[11px] text-muted">{t.id === "builtin" ? "FireCardManager's default" : t.source === "pc" ? "From FireCardManager on your PC" : "Made here"}</span>
              </button>
            </li>
          ))}
        </ul>
        <p className="text-[12px] text-muted">
          Templates from your PC come over when FireCardManager syncs; change them there (Listing Templates). Make a copy to change one here.
          The ★ default is used unless you pick another in the Bulk Lister.
        </p>
      </aside>

      <section className="min-w-0 space-y-3">
        {draft ? (
          <div className="card space-y-3">
            <label className="block text-[13px] font-semibold">Name<input className="field" value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></label>
            <label className="block text-[13px] font-semibold">Title pattern
              <input className="field" value={draft.title_template} onChange={(e) => setDraft({ ...draft, title_template: e.target.value })} />
            </label>
            <label className="block text-[13px] font-semibold">Description (HTML)
              <textarea className="field h-72 font-mono text-[12px]" value={draft.description_html} onChange={(e) => setDraft({ ...draft, description_html: e.target.value })} />
            </label>
            <p className="text-[12px] text-muted">Fields you can use: {TOKENS.join(" ")}</p>
            <div className="flex flex-wrap items-center gap-2">
              <button type="button" className="btn" disabled={busy} onClick={save}>{busy ? "Saving..." : "Save template"}</button>
              <button type="button" className="btn-ghost" onClick={() => setDraft(null)}>Cancel</button>
            </div>
          </div>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="mr-auto text-[18px] font-bold">{cur.name}</h2>
            {cur.source === "web" && cur.id !== "builtin" && <button type="button" className="btn-ghost !py-1.5" onClick={() => setDraft({ ...cur })}>Edit</button>}
            <button type="button" className="btn-ghost !py-1.5" onClick={() => copy(cur)}>Make a copy</button>
            {cur.source === "web" && cur.id !== "builtin" && <button type="button" className="btn-ghost !py-1.5 text-bad" onClick={() => remove(cur)}>Delete</button>}
          </div>
        )}
        {msg && <p role="status" className={`text-[13px] ${msg.bad ? "text-bad" : "text-go"}`}>{msg.text}</p>}
        <div className="card space-y-2">
          <p className="text-[12px] font-semibold uppercase text-muted">Preview with a sample card</p>
          <p className="font-semibold">{fill(cur.title_template, store).replace(/\s{2,}/g, " ").trim()}</p>
          <iframe title="Description preview" sandbox="" className="h-[640px] w-full rounded-lg border border-line bg-white"
            srcDoc={fill(cur.description_html, store) + '<p style="margin-top:16px;color:#666;font-size:12px;">Provided by FireCardManager.com</p>'} />
        </div>
      </section>
    </div>
  );
}

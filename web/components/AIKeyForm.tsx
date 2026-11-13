"use client";

import { useState } from "react";
import { send, shortDate } from "@/lib/client";

export type AIKey = {
  provider: "claude" | "gemini";
  model?: string;
  last4: string;
  source: "pc" | "web";
  device_name?: string;
  updated_at: string;
};

const NAMES = { claude: "Claude (Anthropic)", gemini: "Gemini (Google)" };

/** The seller's own AI key, for identifying cards from a phone. */
export default function AIKeyForm({ initial }: { initial: AIKey | null }) {
  const [key, setKey] = useState<AIKey | null>(initial);
  const [editing, setEditing] = useState(!initial);
  const [provider, setProvider] = useState<"claude" | "gemini">(initial?.provider ?? "claude");
  const [value, setValue] = useState("");
  const [model, setModel] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const r = await send<{ ai_key: AIKey }>("PUT", "/v1/me/ai-key", { provider, key: value, model });
    setBusy(false);
    if (!r.ok) return setError(r.fields.key || r.fields.provider || r.message);
    setKey(r.data.ai_key);
    setValue("");
    setEditing(false);
  }

  async function remove() {
    if (!confirm("Remove the AI key you entered here? A key from a linked PC is used again on its next sync.")) return;
    const r = await send("DELETE", "/v1/me/ai-key");
    if (!r.ok) return setError(r.message);
    setKey(null);
    setEditing(true);
  }

  return (
    <div className="mt-2 space-y-3 text-[14px]">
      <p className="text-muted">
        Used to identify cards you photograph with your phone (Inventory &gt; Add card). It&apos;s your own key, so the AI service bills you directly,
        usually a cent or two per card. It&apos;s stored encrypted and never shown again.
      </p>
      {key && !editing && (
        <div className="rounded-lg border border-line px-3 py-2">
          <p>
            <b>{NAMES[key.provider]}</b> key ending <span className="font-mono">{key.last4}</span>
            {key.model ? ` · ${key.model}` : ""}
          </p>
          <p className="text-[13px] text-muted">
            {key.source === "pc" ? `From FireCardManager on ${key.device_name || "your PC"}` : "Entered here"} · {shortDate(key.updated_at)}
          </p>
          <div className="mt-2 flex gap-2">
            <button type="button" className="btn-ghost !py-1 text-[13px]" onClick={() => setEditing(true)}>Use a different key</button>
            {key.source === "web" && <button type="button" className="btn-ghost !py-1 text-[13px] text-bad" onClick={remove}>Remove</button>}
          </div>
        </div>
      )}
      {!key && !editing && <p>No key yet.</p>}
      {editing && (
        <form onSubmit={save} className="space-y-3">
          {!key && (
            <p>
              Have FireCardManager on a PC with a Claude or Gemini key in Configuration? Link that PC and its key comes over by itself. Or enter one here:
            </p>
          )}
          <div className="flex flex-wrap gap-3">
            {(["claude", "gemini"] as const).map((p) => (
              <label key={p} className="flex items-center gap-2">
                <input type="radio" name="provider" checked={provider === p} onChange={() => setProvider(p)} /> {NAMES[p]}
              </label>
            ))}
          </div>
          <label className="block font-semibold">
            API key
            <input className="field font-mono" value={value} onChange={(e) => setValue(e.target.value)} autoComplete="off" spellCheck={false} required
              placeholder={provider === "claude" ? "sk-ant-..." : "AIza..."} />
          </label>
          <label className="block font-semibold">
            Model <span className="font-normal text-muted">(optional)</span>
            <input className="field" value={model} onChange={(e) => setModel(e.target.value)}
              placeholder={provider === "claude" ? "claude-haiku-5-5" : "gemini-flash-lite-latest"} />
          </label>
          {error && <p role="alert" className="text-bad">{error}</p>}
          <div className="flex gap-2">
            <button type="submit" className="btn" disabled={busy}>{busy ? "Saving..." : "Save key"}</button>
            {key && <button type="button" className="btn-ghost" onClick={() => setEditing(false)}>Cancel</button>}
          </div>
        </form>
      )}
    </div>
  );
}

"use client";

import { useState } from "react";
import { editionName, send, shortDate, when } from "@/lib/client";
import type { Device } from "@/lib/types";

export default function DeviceList({ initial }: { initial: Device[] }) {
  const [list, setList] = useState(initial);
  const [error, setError] = useState("");

  async function remove(d: Device) {
    if (!confirm(`Unlink ${d.name} (${editionName(d.edition)})? It stops syncing until you link it again.`)) return;
    const r = await send("DELETE", "/v1/devices/" + d.id);
    if (!r.ok) return setError(r.message);
    setList(list.filter((x) => x.id !== d.id));
  }

  if (list.length === 0) {
    return <p className="card mt-6">No PCs linked yet. In FireCardManager, open Settings &gt; Cloud Sync and click Link this PC.</p>;
  }
  return (
    <>
      {error && <p role="alert" className="mt-4 text-bad">{error}</p>}
      <ul className="mt-6 space-y-3">
        {list.map((d) => (
          <li key={d.id} className="card flex flex-wrap items-center justify-between gap-3">
            <div>
              <p className="font-bold">{d.name}</p>
              <p className="text-[14px] text-muted">
                {editionName(d.edition)}{d.app_version ? ` ${d.app_version}` : ""} · linked {shortDate(d.created_at)} · last sync {when(d.last_seen_at)}
              </p>
            </div>
            <button type="button" onClick={() => remove(d)} className="btn-ghost text-[14px] text-bad">Unlink</button>
          </li>
        ))}
      </ul>
    </>
  );
}

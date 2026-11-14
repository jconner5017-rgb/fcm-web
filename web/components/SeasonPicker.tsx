"use client";

import { useEffect, useState } from "react";
import { SEASONS, chosenSeason } from "@/lib/season";

/** Account page: seasonal looks automatic by date, off, or one picked to try. */
export default function SeasonPicker() {
  const [pref, setPref] = useState("off");
  useEffect(() => { try { setPref(localStorage.getItem("fcm-season") ?? "off"); } catch {} }, []);
  function choose(v: string) {
    setPref(v);
    try { localStorage.setItem("fcm-season", v); localStorage.removeItem("fcm-season-hide"); } catch {}
    const s = chosenSeason(v, new Date());
    if (s) document.documentElement.dataset.season = s;
    else delete document.documentElement.dataset.season;
    window.dispatchEvent(new Event("fcm-season"));
  }
  return (
    <div className="space-y-2 text-[14px]">
      <p className="text-muted">Optional: holiday colors and a short greeting. Off unless you turn it on. Automatic switches with the calendar (New Year&apos;s, Fourth of July, Halloween, Veterans Day, Thanksgiving, Christmas), or pick one to use now.</p>
      <div className="flex flex-wrap gap-2">
        {[["off", "Off"], ["auto", "Automatic"], ...SEASONS.map((s) => [s.key, `${s.emoji} ${s.name}`])].map(([v, label]) => (
          <button key={v} type="button" onClick={() => choose(v)}
            className={`rounded-full border px-3 py-1 font-semibold ${pref === v ? "border-fire bg-fire text-white" : "border-line hover:border-fire"}`}>{label}</button>
        ))}
      </div>
    </div>
  );
}

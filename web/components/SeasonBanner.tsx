"use client";

import { useEffect, useState } from "react";
import { SEASONS } from "@/lib/season";

/** A one-line holiday greeting under the header while a seasonal look is on.
 *  Closing it hides it for the rest of that holiday (in this browser). */
export default function SeasonBanner() {
  const [season, setSeason] = useState("");
  const [hidden, setHidden] = useState(true);
  useEffect(() => {
    const read = () => {
      const s = document.documentElement.dataset.season ?? "";
      setSeason(s);
      try { setHidden(!s || localStorage.getItem("fcm-season-hide") === s + new Date().getFullYear()); } catch { setHidden(!s); }
    };
    read();
    window.addEventListener("fcm-season", read);
    return () => window.removeEventListener("fcm-season", read);
  }, []);
  const info = SEASONS.find((x) => x.key === season);
  if (!info || hidden) return null;
  return (
    <div className="season-banner">
      <div className="mx-auto flex max-w-[1100px] items-center gap-3 px-4 py-2 text-[14px]">
        <span aria-hidden className="text-[18px]">{info.emoji}</span>
        <span className="flex-1 font-semibold">{info.greeting}</span>
        <button type="button" aria-label="Hide this greeting" className="rounded-full px-2 text-[16px] opacity-70 hover:opacity-100"
          onClick={() => { setHidden(true); try { localStorage.setItem("fcm-season-hide", season + new Date().getFullYear()); } catch {} }}>×</button>
      </div>
    </div>
  );
}

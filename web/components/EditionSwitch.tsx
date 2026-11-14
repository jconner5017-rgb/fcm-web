"use client";

import { useRouter } from "next/navigation";

/** Home page Sports | TCG switch: the whole site follows it (fcm_edition cookie). */
export default function EditionSwitch({ editions, current }: { editions: string[]; current: string }) {
  const router = useRouter();
  function pick(e: string) {
    document.cookie = `fcm_edition=${e}; path=/; max-age=31536000; samesite=lax; secure`;
    router.replace("/");
    router.refresh();
  }
  return (
    <div className="flex gap-1 rounded-full border-2 border-go p-1 text-[13px] font-semibold" role="group" aria-label="Sports or TCG">
      {editions.map((e) => (
        <button key={e} type="button" onClick={() => pick(e)} aria-pressed={e === current}
          className={`rounded-full px-3 py-1 ${e === current ? "bg-go text-white shadow-sm" : "text-go hover:bg-go/10"}`}>
          {e === "tcg" ? "TCG" : "Sports"}
        </button>
      ))}
    </div>
  );
}

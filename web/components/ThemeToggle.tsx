"use client";

/** Light/dark switch; the choice is remembered in this browser. */
export default function ThemeToggle() {
  function flip() {
    const next = document.documentElement.dataset.theme === "light" ? "dark" : "light";
    document.documentElement.dataset.theme = next;
    try {
      localStorage.setItem("fcm-theme", next);
    } catch {}
  }
  return (
    <button type="button" onClick={flip} aria-label="Switch light or dark" title="Switch light or dark"
      className="rounded-full border border-line px-2 py-1 text-[13px] hover:border-ink">
      ◐
    </button>
  );
}

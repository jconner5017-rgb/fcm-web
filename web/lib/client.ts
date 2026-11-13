// Browser-side calls to the API through the same-origin /api proxy.
import type { ApiError } from "./types";

export type Result<T> =
  | { ok: true; data: T }
  | { ok: false; status: number; code: string; message: string; fields: Record<string, string> };

export async function send<T>(method: string, path: string, body?: unknown): Promise<Result<T>> {
  let res: Response;
  try {
    res = await fetch("/api" + path, {
      method,
      headers: { "content-type": "application/json" },
      body: method === "GET" ? undefined : JSON.stringify(body ?? {}),
    });
  } catch {
    return { ok: false, status: 0, code: "offline", message: "Can't reach FireCardManager Web. Check your connection and try again.", fields: {} };
  }
  if (res.status === 204) return { ok: true, data: undefined as T };
  const json = await res.json().catch(() => null);
  if (res.ok) return { ok: true, data: json as T };
  const err = (json as ApiError | null)?.error;
  return {
    ok: false,
    status: res.status,
    code: err?.code ?? "",
    message: err?.message ?? `Something went wrong (${res.status}).`,
    fields: err?.fields ?? {},
  };
}

/** Only allow redirects back into this site. */
export function safeNext(next: string | null | undefined, fallback = "/"): string {
  if (!next || !next.startsWith("/") || next.startsWith("//")) return fallback;
  return next;
}

export function editionName(e: string): string {
  return e === "tcg" ? "FireCardManager TCG" : "FireCardManager (Sports)";
}

export function shortDate(iso?: string): string {
  if (!iso) return "never";
  return new Date(iso).toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });
}

export function when(iso?: string): string {
  if (!iso) return "never";
  const d = new Date(iso);
  const mins = Math.round((Date.now() - d.getTime()) / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} min ago`;
  if (mins < 60 * 24) return `${Math.round(mins / 60)} h ago`;
  return shortDate(iso);
}

/** The web add-on is paid up: it runs until web_addon_until. */
export function addonActive(a: { web_addon_until?: string }): boolean {
  return !!a.web_addon_until && new Date(a.web_addon_until).getTime() > Date.now();
}

export const ADDON_PRICE = "$39.99 for 6 months or $59.99 for 12 ($59.99 / $89.99 together with FCM desktop)";

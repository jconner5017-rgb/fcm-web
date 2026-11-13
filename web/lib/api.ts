import "server-only";
import { cookies } from "next/headers";
import type { Me } from "./types";

// The Go API, reached over Render's private network in production.
export const API_URL = withScheme(process.env.API_URL || "http://localhost:8080");

/** Server-side GET to the API, forwarding the visitor's session cookie. */
export async function apiGet<T>(path: string): Promise<{ status: number; data: T | null; code?: string }> {
  const jar = await cookies();
  const session = jar.get("fcm_session");
  const once = async () => {
    try {
      const res = await fetch(API_URL + path, {
        headers: session ? { cookie: `fcm_session=${session.value}` } : {},
        cache: "no-store",
      });
      const json = await res.json().catch(() => null);
      if (!res.ok) return { status: res.status, data: null, code: json?.error?.code as string | undefined };
      return { status: res.status, data: json as T };
    } catch {
      return { status: 503, data: null };
    }
  };
  // Reads are safe to repeat: retrying rides out a deploy.
  let r = await once();
  for (const wait of [1000, 2500]) {
    if (r.status < 502) return r;
    await new Promise((ok) => setTimeout(ok, wait));
    r = await once();
  }
  return r;
}

/** The signed-in seller, or null. `mfaPending` when only the code step is left. */
export async function getMe(): Promise<{ me: Me | null; mfaPending: boolean }> {
  const r = await apiGet<Me>("/v1/me");
  return { me: r.data, mfaPending: r.code === "mfa_required" };
}

/** Render's private-network address comes as host:port; add the scheme. */
function withScheme(u: string): string {
  u = u.trim().replace(/\/$/, "");
  return /^https?:\/\//.test(u) ? u : "http://" + u;
}

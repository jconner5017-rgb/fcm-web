import Link from "next/link";
import { API_URL } from "@/lib/api";
import Panel from "@/components/Panel";

export const metadata = { title: "Confirm your email" };

export default async function VerifyPage({ searchParams }: { searchParams: Promise<{ token?: string }> }) {
  const { token } = await searchParams;
  let ok = false;
  if (token) {
    try {
      const res = await fetch(API_URL + "/v1/auth/verify-email", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ token }),
        cache: "no-store",
      });
      ok = res.ok;
    } catch {}
  }
  return (
    <Panel title={ok ? "Email confirmed" : "That link didn't work"}>
      <p className="mt-4">
        {ok ? "Thanks, your email address is confirmed." : "The link has expired or was already used. You can send a new one from your Account page."}
      </p>
      <Link href="/" className="btn mt-6">Go to FireCardManager Web</Link>
    </Panel>
  );
}

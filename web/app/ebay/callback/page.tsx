import EbayCallback from "@/components/EbayCallback";

export const metadata = { title: "Connecting eBay" };

export default async function EbayCallbackPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const sp = await searchParams;
  const one = (v: string | string[] | undefined) => (Array.isArray(v) ? v[0] : v) ?? "";
  return <EbayCallback code={one(sp.code)} state={one(sp.state)} />;
}

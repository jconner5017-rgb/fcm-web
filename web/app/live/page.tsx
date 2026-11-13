import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import { href, one, type InvSummary } from "@/lib/inventory";
import LiveListings from "@/components/live/LiveListings";

export const metadata = { title: "Live Listings" };

export default async function LivePage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const edition = one((await searchParams).edition);
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/live");
  const [sum, keys] = await Promise.all([
    apiGet<InvSummary>(href("/v1/inventory/summary", { edition })),
    apiGet<{ keys: { market: string }[] }>("/v1/me/market-keys"),
  ]);
  const eb = (await apiGet<{ connected: boolean }>("/v1/ebay")).data;
  if (!sum.data) return <p className="card mt-8">Your listings couldn&apos;t be loaded just now. Refresh the page in a minute.</p>;
  const ed = sum.data.edition;
  return (
    <div className="relative left-1/2 mt-6 w-screen max-w-[1400px] -translate-x-1/2 px-4 lg:px-8">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px] font-bold">Live Listings</h1>
          <p className="text-[13px] text-muted">Everything you have up for sale on eBay and Slabs &amp; Singles.</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {sum.data.editions.length > 1 && (
            <div className="flex gap-1 rounded-full border border-line p-1 text-[13px] font-semibold">
              {sum.data.editions.map((e) => (
                <Link key={e.edition} href={href("/live", { edition: e.edition })}
                  className={`rounded-full px-3 py-1 ${e.edition === ed ? "bg-fire text-white" : "hover:text-fire"}`}>{e.edition === "tcg" ? "TCG" : "Sports"}</Link>
              ))}
            </div>
          )}
          <Link href="/lister" className="btn !py-1.5">Bulk Lister</Link>
        </div>
      </div>
      <LiveListings key={ed} edition={ed} hasKey={!!keys.data?.keys.some((k) => k.market === "slabs")} ebayOn={!!eb?.connected} />
    </div>
  );
}

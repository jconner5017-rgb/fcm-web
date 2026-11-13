import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import { href, one, type InvSummary } from "@/lib/inventory";
import SoldLog from "@/components/sold/SoldLog";

export const metadata = { title: "Sold Log" };

export default async function SoldPageView({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const sp = await searchParams;
  const edition = one(sp.edition);
  const { me } = await getMe();
  if (!me) redirect("/signin?next=" + encodeURIComponent(href("/sold", { edition })));
  const sum = (await apiGet<InvSummary>(href("/v1/inventory/summary", { edition }))).data;
  if (!sum) return <p className="card mt-8">The Sold Log couldn&apos;t be loaded just now. Refresh the page in a minute.</p>;
  const ed = sum.edition;
  return (
    <div className="relative left-1/2 mt-6 w-screen max-w-[1500px] -translate-x-1/2 px-4 lg:px-8">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px] font-bold">Sold Log</h1>
          <p className="text-[13px] text-muted">Tap a sale to edit it. Changes reach FireCardManager on your PC at its next sync.</p>
        </div>
        {sum.editions.length > 1 && (
          <div className="flex gap-1 rounded-full border border-line p-1 text-[13px] font-semibold">
            {sum.editions.map((e) => (
              <Link key={e.edition} href={href("/sold", { edition: e.edition })}
                className={`rounded-full px-3 py-1 ${e.edition === ed ? "bg-fire text-white" : "hover:text-fire"}`}>
                {e.edition === "tcg" ? "TCG" : "Sports"}
              </Link>
            ))}
          </div>
        )}
      </div>
      <SoldLog key={ed} edition={ed} />
    </div>
  );
}

import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import { href, one, type InvSummary, type TreeSport } from "@/lib/inventory";
import ExportForm from "@/components/data/ExportForm";

export const metadata = { title: "Export" };

export default async function ExportPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const sp = await searchParams;
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/export");
  const [sum, tree] = await Promise.all([
    apiGet<InvSummary>(href("/v1/inventory/summary", { edition: one(sp.edition) })),
    apiGet<{ edition: string; sports: TreeSport[] }>(href("/v1/inventory/tree", { edition: one(sp.edition) })),
  ]);
  if (!sum.data || !tree.data) return <p className="card mt-8">Your inventory couldn&apos;t be loaded just now. Refresh the page in a minute.</p>;
  const ed = tree.data.edition;
  return (
    <div className="mt-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px] font-bold">Export</h1>
          <p className="mt-1 text-[14px] text-muted">The same files FireCardManager makes, with your photos linked.</p>
        </div>
        <Link href="/import" className="btn !py-1.5 ml-auto">Import cards</Link>
        {sum.data.editions.length > 1 && (
          <div className="flex gap-1 rounded-full border border-line p-1 text-[13px] font-semibold">
            {sum.data.editions.map((e) => (
              <Link key={e.edition} href={href("/export", { edition: e.edition })}
                className={`rounded-full px-3 py-1 ${e.edition === ed ? "bg-fire text-white" : "hover:text-fire"}`}>{e.edition === "tcg" ? "TCG" : "Sports"}</Link>
            ))}
          </div>
        )}
      </div>
      <p className="card mt-4 text-[14px] lg:hidden">Exporting works best on a computer, where you can open the file in Excel or upload it to Shopify or Whatnot.</p>
      <div className="mt-5"><ExportForm key={ed} edition={ed} sports={tree.data.sports} /></div>
    </div>
  );
}

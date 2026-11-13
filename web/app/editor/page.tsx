import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import { href, one, type InvSummary, type TreeSport } from "@/lib/inventory";
import CardEditor from "@/components/editor/CardEditor";

export const metadata = { title: "Card Editor" };

export default async function EditorPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const sp = await searchParams;
  const start = { sport: one(sp.sport) ?? "", box: one(sp.box) ?? "", row: one(sp.row) ?? "", q: one(sp.q) ?? "", id: one(sp.id) };
  const { me } = await getMe();
  if (!me) redirect("/signin?next=" + encodeURIComponent(href("/editor", { ...start, edition: one(sp.edition) })));
  const [sum, tree] = await Promise.all([
    apiGet<InvSummary>(href("/v1/inventory/summary", { edition: one(sp.edition) })),
    apiGet<{ edition: string; sports: TreeSport[] }>(href("/v1/inventory/tree", { edition: one(sp.edition) })),
  ]);
  if (!sum.data || !tree.data) return <p className="card mt-8">Your inventory couldn&apos;t be loaded just now. Refresh the page in a minute.</p>;
  const ed = tree.data.edition;
  return (
    // As wide as the screen allows, like the desktop window.
    <div className="relative left-1/2 mt-4 w-screen max-w-[1800px] -translate-x-1/2 px-4 lg:px-6">
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <h1 className="text-[22px] font-bold">Card Editor</h1>
        {sum.data.editions.length > 1 && (
          <div className="flex gap-1 rounded-full border border-line p-1 text-[13px] font-semibold">
            {sum.data.editions.map((e) => (
              <Link key={e.edition} href={href("/editor", { edition: e.edition })}
                className={`rounded-full px-3 py-1 ${e.edition === ed ? "bg-fire text-white" : "hover:text-fire"}`}>{e.edition === "tcg" ? "TCG" : "Sports"}</Link>
            ))}
          </div>
        )}
        <Link href="/inventory/add" className="btn-ghost ml-auto !py-1.5">+ Add card</Link>
      </div>
      <div className="card lg:hidden">
        <p className="font-semibold">The full Card Editor is made for a computer screen.</p>
        <p className="mt-1 text-[14px] text-muted">On a phone, open a card from <Link href="/inventory" className="font-semibold text-fire">Inventory</Link> and tap Edit.</p>
      </div>
      <div className="hidden lg:block">
        {!me.sync_allowed && <p className="card mb-3 text-[14px]">Your free trial has ended: you can look, but saving changes needs FireCardManager Web.</p>}
        <CardEditor key={ed} edition={ed} sports={tree.data.sports} start={start} />
      </div>
    </div>
  );
}

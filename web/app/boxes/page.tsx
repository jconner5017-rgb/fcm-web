import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import { href, one, type CardPage, type InvSummary, type Params, type TreeSport } from "@/lib/inventory";
import BoxTree from "@/components/inventory/BoxTree";
import BoxTile from "@/components/inventory/BoxTile";

export const metadata = { title: "Boxes" };

const PER_PAGE = 48;

export default async function BoxesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const sp = await searchParams;
  const cur: Params = { edition: one(sp.edition), sport: one(sp.sport), box: one(sp.box), row: one(sp.row), page: one(sp.page) };
  const self = href("/boxes", cur);
  const { me } = await getMe();
  if (!me) redirect("/signin?next=" + encodeURIComponent(self));

  const [sumRes, treeRes, pageRes] = await Promise.all([
    apiGet<InvSummary>(href("/v1/inventory/summary", { edition: cur.edition })),
    apiGet<{ edition: string; sports: TreeSport[] }>(href("/v1/inventory/tree", { edition: cur.edition })),
    apiGet<CardPage>(href("/v1/inventory/cards", { ...cur, sort: "slot", per: String(PER_PAGE), backs: "1" })),
  ]);
  const sum = sumRes.data, tree = treeRes.data, page = pageRes.data;
  if (!sum || !tree || !page) return <p className="card mt-8">Your boxes couldn&apos;t be loaded just now. Refresh the page in a minute.</p>;
  cur.edition = tree.edition;

  const sport = tree.sports.find((s) => s.sport === cur.sport);
  const box = sport?.boxes.find((b) => b.id === cur.box);
  const crumbs: { label: string; href?: string }[] = [{ label: "All", href: href("/boxes", { edition: cur.edition }) }];
  if (sport) crumbs.push({ label: sport.sport, href: href("/boxes", { edition: cur.edition, sport: sport.sport }) });
  if (sport && box) crumbs.push({ label: box.label, href: href("/boxes", { edition: cur.edition, sport: sport.sport, box: box.id }) });
  if (box && cur.row) crumbs.push({ label: `Row ${cur.row}` });
  const pages = Math.max(1, Math.ceil(page.total / PER_PAGE));
  const total = tree.sports.reduce((n, s) => n + s.cards, 0);

  return (
    <div className="mt-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <h1 className="text-[26px] font-bold">Boxes</h1>
        <div className="flex flex-wrap items-center gap-2">
          {sum.editions.length > 1 && (
            <div className="flex gap-1 rounded-full border border-line p-1 text-[13px] font-semibold">
              {sum.editions.map((e) => (
                <Link key={e.edition} href={href("/boxes", { edition: e.edition })}
                  className={`rounded-full px-3 py-1 ${e.edition === tree.edition ? "bg-fire text-white" : "hover:text-fire"}`}>
                  {e.edition === "tcg" ? "TCG" : "Sports"}
                </Link>
              ))}
            </div>
          )}
          <Link href="/inventory/add" className="btn !py-1.5">+ Add card</Link>
        </div>
      </div>

      <div className="mt-5 grid gap-6 lg:grid-cols-[270px_minmax(0,1fr)]">
        <aside>
          {/* Phone and tablet: the tree folds away above the cards. */}
          <details className="card !p-2 lg:hidden">
            <summary className="cursor-pointer px-2 py-1 font-semibold">
              {crumbs.map((c) => c.label).join(" › ")}
            </summary>
            <div className="mt-2"><BoxTree sports={tree.sports} total={total} cur={cur} /></div>
          </details>
          <div className="sticky top-4 hidden max-h-[calc(100vh-2rem)] overflow-y-auto rounded-xl border border-line bg-paper p-2 lg:block">
            <BoxTree sports={tree.sports} total={total} cur={cur} />
          </div>
        </aside>

        <section className="min-w-0">
          <nav aria-label="Where you are" className="flex flex-wrap items-center gap-1 text-[15px] font-semibold">
            {crumbs.map((c, i) => (
              <span key={i} className="flex items-center gap-1">
                {i > 0 && <span className="text-muted">›</span>}
                {c.href && i < crumbs.length - 1 ? <Link href={c.href} className="text-link hover:underline">{c.label}</Link> : <span>{c.label}</span>}
              </span>
            ))}
          </nav>
          <p className="num mb-3 mt-1 text-[12px] text-muted">
            {page.total.toLocaleString()} card{page.total === 1 ? "" : "s"} · in box, row and slot order · moving cards is done in FireCardManager
          </p>

          {page.cards.length === 0 ? (
            <p className="card text-muted">No cards here.</p>
          ) : (
            <ul className="grid auto-rows-fr grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
              {page.cards.map((c) => <li key={c.id}><BoxTile c={c} from={self} /></li>)}
            </ul>
          )}

          {pages > 1 && (
            <nav aria-label="Pages" className="mt-5 flex items-center justify-center gap-3 text-[14px]">
              {page.page > 1 ? <Link href={href("/boxes", cur, { page: page.page === 2 ? undefined : String(page.page - 1) })} className="btn-ghost">Previous</Link>
                : <span className="btn-ghost opacity-40">Previous</span>}
              <span className="num text-muted">Page {page.page} of {pages}</span>
              {page.page < pages ? <Link href={href("/boxes", cur, { page: String(page.page + 1) })} className="btn-ghost">Next</Link>
                : <span className="btn-ghost opacity-40">Next</span>}
            </nav>
          )}
        </section>
      </div>
    </div>
  );
}

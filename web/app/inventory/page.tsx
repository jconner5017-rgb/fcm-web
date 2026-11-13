import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import { editionName, when } from "@/lib/client";
import { href, money, one, type CardPage, type InvSummary, type Params } from "@/lib/inventory";
import StorageTree from "@/components/inventory/StorageTree";
import AutoSubmit from "@/components/inventory/AutoSubmit";
import { CardTable, CardTiles } from "@/components/inventory/CardList";

export const metadata = { title: "Inventory" };

const PER_PAGE = 60;

export default async function InventoryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const sp = await searchParams;
  const cur: Params = {
    edition: one(sp.edition),
    box: one(sp.box),
    row: one(sp.row),
    status: one(sp.status),
    q: one(sp.q)?.trim(),
    sort: one(sp.sort),
    dir: one(sp.dir),
    view: one(sp.view),
    page: one(sp.page),
  };
  const self = href("/inventory", cur);
  const { me } = await getMe();
  if (!me) redirect("/signin?next=" + encodeURIComponent(self));

  const api = (path: string) => href(path, { ...cur, view: undefined, per: String(PER_PAGE) });
  const [sumRes, pageRes] = await Promise.all([
    apiGet<InvSummary>(href("/v1/inventory/summary", { edition: cur.edition })),
    apiGet<CardPage>(api("/v1/inventory/cards")),
  ]);
  const sum = sumRes.data;
  const page = pageRes.data;
  if (!sum || !page) {
    return <p className="card mt-8">Your inventory couldn&apos;t be loaded just now. Refresh the page in a minute.</p>;
  }
  cur.edition = sum.edition;

  if (sum.editions.length === 0) {
    return (
      <div className="mt-8 max-w-[680px]">
        <h1 className="text-[26px] font-bold">Inventory</h1>
        <p className="card mt-4">
          Nothing has synced yet. In FireCardManager on your PC, open <b>Settings &gt; FireCardManager Web</b>, click <b>Open Cloud Sync</b> and link
          the PC. Your boxes, cards and photos appear here after the first sync.
        </p>
      </div>
    );
  }

  const box = sum.boxes.find((b) => b.id === cur.box);
  const title = cur.box === "none" ? "Not in a box" : box ? (cur.row ? `${box.name} · Row ${cur.row}` : box.name) : "All cards";
  const pages = Math.max(1, Math.ceil(page.total / PER_PAGE));
  const tiles = cur.view !== "table";
  const listed = (sum.by_status.EBA ?? 0) + (sum.by_status.EBF ?? 0);

  return (
    <div className="mt-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px] font-bold">Inventory</h1>
          <p className="text-[13px] text-muted">
            {sum.cards.toLocaleString()} cards · {money(sum.value)} estimated · {listed.toLocaleString()} live on eBay
            {sum.last_sync && <> · last sync {when(sum.last_sync)}</>}
          </p>
        </div>
        {sum.editions.length > 1 && (
          <div className="flex gap-1 rounded-full border border-line p-1 text-[13px] font-semibold">
            {sum.editions.map((e) => (
              <Link
                key={e.edition}
                href={href("/inventory", { edition: e.edition, view: cur.view })}
                className={`rounded-full px-3 py-1 ${e.edition === sum.edition ? "bg-fire text-white" : "hover:text-fire"}`}
              >
                {e.edition === "tcg" ? "TCG" : "Sports"} <span className="num opacity-80">{e.cards.toLocaleString()}</span>
              </Link>
            ))}
          </div>
        )}
      </div>

      {sum.conflicts > 0 && (
        <p className="mt-4 rounded-lg border border-fire px-3 py-2 text-[14px]">
          {sum.conflicts} edit{sum.conflicts === 1 ? "" : "s"} clashed with a change made somewhere else.{" "}
          <Link href="/conflicts" className="font-semibold text-fire">Review conflicts</Link>
        </p>
      )}

      <div className="mt-5 grid gap-6 md:grid-cols-[240px_1fr]">
        <aside>
          {/* On a phone the box list folds away above the cards. */}
          <details className="card !p-2 md:hidden">
            <summary className="cursor-pointer px-2 py-1 font-semibold">Boxes · {title}</summary>
            <div className="mt-2"><StorageTree sum={sum} cur={cur} /></div>
          </details>
          <div className="sticky top-4 hidden max-h-[calc(100vh-2rem)] overflow-y-auto md:block">
            <StorageTree sum={sum} cur={cur} />
          </div>
        </aside>

        <section className="min-w-0">
          <form action="/inventory" className="flex flex-wrap items-center gap-2">
            {(["edition", "box", "row", "view"] as const).map((k) => cur[k] && <input key={k} type="hidden" name={k} value={cur[k]} />)}
            <label className="min-w-[200px] flex-1">
              <span className="sr-only">Search</span>
              <input
                name="q"
                type="search"
                defaultValue={cur.q}
                placeholder="Search player, set, number, SKU..."
                className="field !mt-0"
              />
            </label>
            <label>
              <span className="sr-only">Status</span>
              <AutoSubmit name="status" defaultValue={cur.status ?? ""} className="field !mt-0 !w-auto">
                <option value="">Any status</option>
                <option value="listed">Live on eBay</option>
                <option value="Ready">Ready to list</option>
                <option value="NotForSale">Not for sale</option>
                <option value="EBF">eBay Buy It Now</option>
                <option value="EBA">eBay auction</option>
              </AutoSubmit>
            </label>
            <label>
              <span className="sr-only">Sort</span>
              <AutoSubmit name="sort" defaultValue={cur.sort ?? ""} className="field !mt-0 !w-auto">
                <option value="">{cur.q ? "Player" : "Box, row, slot"}</option>
                <option value="slot">Box, row, slot</option>
                <option value="player">Player</option>
                <option value="set">Set</option>
                <option value="year">Year</option>
                <option value="value">Value</option>
                <option value="updated">Recently changed</option>
              </AutoSubmit>
            </label>
            <label>
              <span className="sr-only">Order</span>
              <AutoSubmit name="dir" defaultValue={cur.dir ?? ""} className="field !mt-0 !w-auto">
                <option value="">Low to high, A-Z</option>
                <option value="desc">High to low, Z-A</option>
              </AutoSubmit>
            </label>
            <button type="submit" className="btn !py-2">Search</button>
          </form>

          <div className="mt-4 flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-[18px] font-bold">
              {title}{" "}
              <span className="num text-[14px] font-normal text-muted">
                {page.total.toLocaleString()} card{page.total === 1 ? "" : "s"}
                {cur.q && <> matching &ldquo;{cur.q}&rdquo;</>}
              </span>
            </h2>
            <div className="flex items-center gap-3 text-[13px] font-semibold">
              {(cur.q || cur.status) && (
                <Link href={href("/inventory", { ...cur, q: undefined, status: undefined, page: undefined })} className="text-muted hover:text-fire">
                  Clear search
                </Link>
              )}
              <div className="flex gap-1 rounded-full border border-line p-1">
                <Link href={href("/inventory", cur, { view: undefined })} className={`rounded-full px-3 py-0.5 ${tiles ? "bg-fire text-white" : ""}`}>Photos</Link>
                <Link href={href("/inventory", cur, { view: "table" })} className={`rounded-full px-3 py-0.5 ${!tiles ? "bg-fire text-white" : ""}`}>List</Link>
              </div>
            </div>
          </div>

          <div className="mt-3">
            {page.cards.length === 0 ? (
              <p className="card text-muted">
                {page.total > 0 ? (
                  <>That page is past the end. <Link href={href("/inventory", cur, { page: undefined })} className="font-semibold text-fire">Go to page 1</Link>.</>
                ) : cur.q || cur.status ? "No cards match. Try fewer words or another status." : "No cards here."}
              </p>
            ) : tiles ? (
              <CardTiles cards={page.cards} from={self} />
            ) : (
              <CardTable cards={page.cards} from={self} />
            )}
          </div>

          {pages > 1 && (
            <nav aria-label="Pages" className="mt-5 flex items-center justify-center gap-3 text-[14px]">
              {page.page > 1 ? (
                <Link href={href("/inventory", cur, { page: page.page === 2 ? undefined : String(page.page - 1) })} className="btn-ghost">Previous</Link>
              ) : <span className="btn-ghost opacity-40">Previous</span>}
              <span className="num text-muted">Page {page.page} of {pages}</span>
              {page.page < pages ? (
                <Link href={href("/inventory", cur, { page: String(page.page + 1) })} className="btn-ghost">Next</Link>
              ) : <span className="btn-ghost opacity-40">Next</span>}
            </nav>
          )}
          <p className="mt-6 text-center text-[12px] text-muted">
            {editionName(sum.edition)} · open a card to edit it or mark it sold. Changes on your PC show here after its next sync.
          </p>
        </section>
      </div>
    </div>
  );
}

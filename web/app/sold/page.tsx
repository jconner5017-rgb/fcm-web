import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import { shortDate } from "@/lib/client";
import { href, money, one, setLine, type InvSummary, type Params, type SoldPage } from "@/lib/inventory";

export const metadata = { title: "Sold Log" };

function where(d: SoldPage["sold"][number]["data"]): string {
  if (d.marketplace_order_id) return "Slabs & Singles";
  return d.sold_outside_ebay ? "Outside eBay" : "eBay";
}

export default async function SoldPageView({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const sp = await searchParams;
  const cur: Params = { edition: one(sp.edition), q: one(sp.q)?.trim(), page: one(sp.page) };
  const { me } = await getMe();
  if (!me) redirect("/signin?next=" + encodeURIComponent(href("/sold", cur)));

  const [sumRes, res] = await Promise.all([
    apiGet<InvSummary>(href("/v1/inventory/summary", { edition: cur.edition })),
    apiGet<SoldPage>(href("/v1/inventory/sold", cur)),
  ]);
  const sum = sumRes.data;
  const page = res.data;
  if (!sum || !page) return <p className="card mt-8">The Sold Log couldn&apos;t be loaded just now. Refresh the page in a minute.</p>;
  cur.edition = page.edition;
  const pages = Math.max(1, Math.ceil(page.total / page.per_page));

  return (
    <div className="mt-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px] font-bold">Sold Log</h1>
          <p className="num text-[13px] text-muted">
            {page.total.toLocaleString()} sale{page.total === 1 ? "" : "s"}{cur.q && <> matching &ldquo;{cur.q}&rdquo;</>} · {money(page.revenue)} in sales
          </p>
        </div>
        {sum.editions.length > 1 && (
          <div className="flex gap-1 rounded-full border border-line p-1 text-[13px] font-semibold">
            {sum.editions.map((e) => (
              <Link key={e.edition} href={href("/sold", { edition: e.edition })}
                className={`rounded-full px-3 py-1 ${e.edition === page.edition ? "bg-fire text-white" : "hover:text-fire"}`}>
                {e.edition === "tcg" ? "TCG" : "Sports"}
              </Link>
            ))}
          </div>
        )}
      </div>

      <form action="/sold" className="mt-4 flex flex-wrap gap-2">
        {cur.edition && <input type="hidden" name="edition" value={cur.edition} />}
        <label className="min-w-[200px] flex-1">
          <span className="sr-only">Search</span>
          <input name="q" type="search" defaultValue={cur.q} placeholder="Search player, set, buyer, tracking..." className="field !mt-0" />
        </label>
        <button type="submit" className="btn !py-2">Search</button>
        {cur.q && <Link href={href("/sold", { edition: cur.edition })} className="btn-ghost !py-2">Clear</Link>}
      </form>

      {page.sold.length === 0 ? (
        <p className="card mt-4 text-muted">{cur.q ? "No sales match." : "No sales synced yet."}</p>
      ) : (
        <>
          <div className="mt-4 hidden overflow-x-auto rounded-xl border border-line md:block">
            <table className="w-full text-left text-[14px]">
              <thead className="bg-wash text-[12px] uppercase tracking-wide text-muted">
                <tr>
                  <th className="px-3 py-2">Date</th>
                  <th className="px-3 py-2">Card</th>
                  <th className="px-3 py-2">Where</th>
                  <th className="px-3 py-2">Buyer</th>
                  <th className="px-3 py-2">Shipping</th>
                  <th className="px-3 py-2 text-right">Sold for</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {page.sold.map(({ id, data: d }) => (
                  <tr key={id} className="hover:bg-wash">
                    <td className="whitespace-nowrap px-3 py-2">{d.sale_date ? shortDate(d.sale_date) : ""}</td>
                    <td className="px-3 py-2">
                      <p className="font-semibold">{d.player_name}</p>
                      <p className="text-[12px] text-muted">{setLine(d)}</p>
                    </td>
                    <td className="px-3 py-2 text-[13px]">{where(d)}</td>
                    <td className="px-3 py-2 text-[13px]">{d.buyer}</td>
                    <td className="px-3 py-2 text-[13px]">
                      {d.tracking_number ? `${d.carrier ? d.carrier + " " : ""}${d.tracking_number}` : d.shipped_date_utc ? `Shipped ${shortDate(d.shipped_date_utc)}` : ""}
                    </td>
                    <td className="num px-3 py-2 text-right font-semibold">{money(d.sale_price)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <ul className="mt-4 divide-y divide-line rounded-xl border border-line md:hidden">
            {page.sold.map(({ id, data: d }) => (
              <li key={id} className="p-3">
                <div className="flex justify-between gap-3">
                  <p className="font-semibold">{d.player_name}</p>
                  <p className="num font-bold">{money(d.sale_price)}</p>
                </div>
                <p className="text-[12px] text-muted">{setLine(d)}</p>
                <p className="text-[12px] text-muted">{[d.sale_date ? shortDate(d.sale_date) : "", where(d), d.buyer].filter(Boolean).join(" · ")}</p>
              </li>
            ))}
          </ul>
        </>
      )}

      {pages > 1 && (
        <nav aria-label="Pages" className="mt-5 flex items-center justify-center gap-3 text-[14px]">
          {page.page > 1 ? <Link href={href("/sold", cur, { page: page.page === 2 ? undefined : String(page.page - 1) })} className="btn-ghost">Previous</Link>
            : <span className="btn-ghost opacity-40">Previous</span>}
          <span className="num text-muted">Page {page.page} of {pages}</span>
          {page.page < pages ? <Link href={href("/sold", cur, { page: String(page.page + 1) })} className="btn-ghost">Next</Link>
            : <span className="btn-ghost opacity-40">Next</span>}
        </nav>
      )}
    </div>
  );
}

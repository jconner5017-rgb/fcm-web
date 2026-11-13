import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import { when } from "@/lib/client";
import { href, one, type InvSummary, type TreeSport } from "@/lib/inventory";
import BulkLister from "@/components/lister/BulkLister";
import type { Run } from "@/components/lister/RunProgress";

export const metadata = { title: "Bulk Lister" };

export default async function ListerPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const edition = one((await searchParams).edition);
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/lister");
  const [sum, tree, keys, runs] = await Promise.all([
    apiGet<InvSummary>(href("/v1/inventory/summary", { edition })),
    apiGet<{ edition: string; sports: TreeSport[] }>(href("/v1/inventory/tree", { edition })),
    apiGet<{ keys: { market: string }[] }>("/v1/me/market-keys"),
    apiGet<{ jobs: Run[] }>("/v1/lister/jobs"),
  ]);
  const ed = tree.data?.edition ?? "sports";
  const hasKey = !!keys.data?.keys.some((k) => k.market === "slabs");
  return (
    <div className="mt-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px] font-bold">Bulk Lister</h1>
          <p className="text-[14px] text-muted">Put many cards up for sale at once. Exports (Shopify, Whatnot, CSV) stay in FireCardManager on your PC.</p>
        </div>
        {sum.data && sum.data.editions.length > 1 && (
          <div className="flex gap-1 rounded-full border border-line p-1 text-[13px] font-semibold">
            {sum.data.editions.map((e) => (
              <Link key={e.edition} href={href("/lister", { edition: e.edition })}
                className={`rounded-full px-3 py-1 ${e.edition === ed ? "bg-fire text-white" : "hover:text-fire"}`}>
                {e.edition === "tcg" ? "TCG" : "Sports"}
              </Link>
            ))}
          </div>
        )}
      </div>
      {!me.sync_allowed ? (
        <p className="card mt-5">Listing from the website needs the FireCardManager Web add-on (your free trial has ended).</p>
      ) : (
        <div className="mt-5">
          <BulkLister key={ed} edition={ed} sports={tree.data?.sports ?? []} hasKey={hasKey} />
        </div>
      )}
      {(runs.data?.jobs.length ?? 0) > 0 && (
        <section className="mt-8">
          <h2 className="text-[16px] font-bold">Recent runs</h2>
          <ul className="mt-2 divide-y divide-line rounded-xl border border-line text-[14px]">
            {runs.data!.jobs.map((j) => (
              <li key={j.id}>
                <Link href={`/lister/runs/${j.id}`} className="flex flex-wrap justify-between gap-2 px-3 py-2 hover:bg-wash">
                  <span>Slabs &amp; Singles · {when(j.created_at)}</span>
                  <span className="num text-muted">
                    {j.counts.done} listed · {j.counts.problems} not listed{j.counts.pending ? ` · ${j.counts.pending} waiting` : ""}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

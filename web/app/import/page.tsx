import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import type { InvSummary } from "@/lib/inventory";
import ImportForm from "@/components/data/ImportForm";

export const metadata = { title: "Import cards from CSV" };

export default async function ImportPage() {
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/import");
  const [sum, key] = await Promise.all([
    apiGet<InvSummary>("/v1/inventory/summary"),
    apiGet<{ ai_key: unknown }>("/v1/me/ai-key"),
  ]);
  const synced = sum.data?.editions.map((e) => e.edition) ?? [];
  const editions = synced.length ? [sum.data!.edition, ...synced.filter((e) => e !== sum.data!.edition)] : ["sports", "tcg"];
  return (
    <div className="mt-6">
      <div className="mb-5 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px] font-bold">Import cards from CSV</h1>
          <p className="mt-1 text-[14px] text-muted">Bring in cards from a spreadsheet, another lister or a FireCardManager export.</p>
        </div>
        <Link href="/export" className="btn !py-1.5">Export cards</Link>
      </div>
      <p className="card mb-4 text-[14px] lg:hidden">Importing works best on a computer, where your CSV and photo folder are.</p>
      {!me.sync_allowed ? (
        <p className="card">Importing cards needs FireCardManager Web (your free trial has ended).</p>
      ) : (
        <ImportForm editions={editions} hasKey={!!key.data?.ai_key} />
      )}
    </div>
  );
}

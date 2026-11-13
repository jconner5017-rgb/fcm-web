import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import { href, one, type InvSummary } from "@/lib/inventory";
import Templates, { type Template } from "@/components/templates/Templates";

export const metadata = { title: "Listing Templates" };

export default async function TemplatesPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const edition = one((await searchParams).edition);
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/templates");
  const [sum, res, eb] = await Promise.all([
    apiGet<InvSummary>(href("/v1/inventory/summary", { edition })),
    apiGet<{ edition: string; templates: Template[]; default_title: string; default_description: string }>(href("/v1/templates", { edition })),
    apiGet<{ settings: { store_name?: string } }>("/v1/ebay/setup").catch(() => ({ data: null })),
  ]);
  if (!res.data) return <p className="card mt-8">Your templates couldn&apos;t be loaded just now. Refresh the page in a minute.</p>;
  const ed = res.data.edition;
  return (
    <div className="relative left-1/2 mt-6 w-screen max-w-[1400px] -translate-x-1/2 px-4 lg:px-8">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px] font-bold">Listing Templates</h1>
          <p className="text-[13px] text-muted">How your eBay listings look: the title and the description.</p>
        </div>
        {sum.data && sum.data.editions.length > 1 && (
          <div className="flex gap-1 rounded-full border border-line p-1 text-[13px] font-semibold">
            {sum.data.editions.map((e) => (
              <Link key={e.edition} href={href("/templates", { edition: e.edition })}
                className={`rounded-full px-3 py-1 ${e.edition === ed ? "bg-fire text-white" : "hover:text-fire"}`}>{e.edition === "tcg" ? "TCG" : "Sports"}</Link>
            ))}
          </div>
        )}
      </div>
      <Templates key={ed} edition={ed} initial={res.data.templates} store={eb.data?.settings?.store_name ?? ""}
        defaults={{ title: res.data.default_title, description: res.data.default_description }} />
    </div>
  );
}

import Link from "next/link";
import { notFound, redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import RunProgress, { type Run } from "@/components/lister/RunProgress";

export const metadata = { title: "Listing run" };

export default async function RunPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/lister");
  const r = await apiGet<Run>(`/v1/lister/jobs/${encodeURIComponent(id)}`);
  if (r.status === 404) notFound();
  if (!r.data) return <p className="card mt-8">This run couldn&apos;t be loaded just now. Refresh the page in a minute.</p>;
  return (
    <div className="mx-auto mt-6 max-w-[760px]">
      <Link href="/lister" className="text-[14px] font-semibold text-muted hover:text-fire">&larr; Bulk Lister</Link>
      <h1 className="mb-4 mt-2 text-[26px] font-bold">Listing on Slabs &amp; Singles</h1>
      <RunProgress initial={r.data} />
    </div>
  );
}

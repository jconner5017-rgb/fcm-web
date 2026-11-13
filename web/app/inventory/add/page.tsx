import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import type { InvSummary } from "@/lib/inventory";
import AddCard from "@/components/inventory/AddCard";

export const metadata = { title: "Add card" };

export default async function AddCardPage() {
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/inventory/add");
  const [sum, key] = await Promise.all([
    apiGet<InvSummary>("/v1/inventory/summary"),
    apiGet<{ ai_key: unknown }>("/v1/me/ai-key"),
  ]);
  // The app(s) the seller uses; a seller with nothing synced yet can pick either.
  const synced = sum.data?.editions.map((e) => e.edition) ?? [];
  const editions = synced.length ? [sum.data!.edition, ...synced.filter((e) => e !== sum.data!.edition)] : ["sports", "tcg"];
  return (
    <div className="mx-auto mt-6 max-w-[640px]">
      <Link href="/inventory" className="text-[14px] font-semibold text-muted hover:text-fire">&larr; Inventory</Link>
      <h1 className="mt-2 text-[26px] font-bold">Add a card</h1>
      <p className="mb-5 mt-1 text-[14px] text-muted">Photograph the front and back. AI fills in the details for you to check, and the card syncs to your PC. Got a stack? <Link href="/inventory/upload" className="font-semibold text-fire">Upload many photos at once</Link>.</p>
      {!me.sync_allowed ? (
        <p className="card">Adding cards needs the FireCardManager Web add-on (your free trial has ended).</p>
      ) : (
        <AddCard editions={editions} hasKey={!!key.data?.ai_key} />
      )}
    </div>
  );
}

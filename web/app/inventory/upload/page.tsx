import Link from "next/link";
import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import type { InvSummary } from "@/lib/inventory";
import BatchUpload from "@/components/inventory/BatchUpload";

export const metadata = { title: "Upload card photos" };

export default async function UploadPage() {
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/inventory/upload");
  const [sum, key] = await Promise.all([
    apiGet<InvSummary>("/v1/inventory/summary"),
    apiGet<{ ai_key: unknown }>("/v1/me/ai-key"),
  ]);
  const synced = sum.data?.editions.map((e) => e.edition) ?? [];
  const editions = synced.length ? [sum.data!.edition, ...synced.filter((e) => e !== sum.data!.edition)] : ["sports", "tcg"];
  return (
    <div className="mx-auto mt-6 max-w-[1100px]">
      <Link href="/inventory" className="text-[14px] font-semibold text-muted hover:text-fire">&larr; Inventory</Link>
      <h1 className="mt-2 text-[26px] font-bold">Upload card photos</h1>
      <p className="mb-5 mt-1 text-[14px] text-muted">
        Add a stack of cards at once from your phone&apos;s photos or a folder on your computer. AI identifies each card, you check the details,
        and they&apos;re filed into your boxes. One card at a time with the camera? <Link href="/inventory/add" className="font-semibold text-fire">Add a card</Link>.
      </p>
      {!me.sync_allowed ? (
        <p className="card">Adding cards needs FireCardManager Web (your free trial has ended).</p>
      ) : (
        <BatchUpload editions={editions} hasKey={!!key.data?.ai_key} />
      )}
    </div>
  );
}

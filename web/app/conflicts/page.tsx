import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import ConflictList, { type Conflict } from "@/components/inventory/ConflictList";

export const metadata = { title: "Conflicts" };

export default async function ConflictsPage() {
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/conflicts");
  const list = (await apiGet<{ conflicts: Conflict[] }>("/v1/inventory/conflicts")).data?.conflicts ?? [];
  return (
    <div className="mt-6 max-w-[860px]">
      <h1 className="text-[26px] font-bold">Conflicts</h1>
      <p className="mt-2 text-muted">
        When the same field of a card is changed in two places before they sync, the newer change wins. The other value is kept here so you can
        put it back if it was the right one.
      </p>
      <ConflictList initial={list} />
    </div>
  );
}

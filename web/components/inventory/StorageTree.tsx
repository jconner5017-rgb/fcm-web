import Link from "next/link";
import { href, money, type InvSummary, type Params } from "@/lib/inventory";

/** Boxes and their rows, with card counts. The open box is expanded. */
export default function StorageTree({ sum, cur }: { sum: InvSummary; cur: Params }) {
  const base = { edition: cur.edition, view: cur.view, sort: cur.sort, dir: cur.dir };
  const item = (active: boolean) =>
    `flex items-center justify-between gap-2 rounded-md px-2 py-1.5 text-[14px] ${active ? "bg-fire text-white" : "hover:bg-paper"}`;
  return (
    <nav aria-label="Storage boxes" className="space-y-0.5">
      <Link href={href("/inventory", base)} className={item(!cur.box)}>
        <span className="font-semibold">All cards</span>
        <span className="num text-[13px] opacity-80">{sum.cards.toLocaleString()}</span>
      </Link>
      {sum.boxes.map((b) => {
        const open = cur.box === b.id;
        return (
          <div key={b.id}>
            <Link href={href("/inventory", base, { box: b.id })} className={item(open && !cur.row)} title={b.description || undefined}>
              <span className="truncate font-semibold">{b.name || "Unnamed box"}</span>
              <span className="num shrink-0 text-[13px] opacity-80">{b.cards.toLocaleString()}</span>
            </Link>
            {open && (
              <div className="mb-1 ml-3 border-l border-line pl-2">
                {b.value > 0 && <p className="px-2 py-1 text-[12px] text-muted">Value {money(b.value)}</p>}
                {b.rows.map((r) => (
                  <Link key={r.row} href={href("/inventory", base, { box: b.id, row: r.row })} className={item(cur.row === r.row)}>
                    <span>Row {r.row}</span>
                    <span className="num text-[13px] opacity-80">{r.cards}</span>
                  </Link>
                ))}
              </div>
            )}
          </div>
        );
      })}
      {sum.unboxed > 0 && (
        <Link href={href("/inventory", base, { box: "none" })} className={item(cur.box === "none")}>
          <span className="italic">Not in a box</span>
          <span className="num text-[13px] opacity-80">{sum.unboxed.toLocaleString()}</span>
        </Link>
      )}
    </nav>
  );
}

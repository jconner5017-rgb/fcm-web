import Link from "next/link";
import { href, type Params, type TreeSport } from "@/lib/inventory";

/**
 * Sport > Box > Row, like the desktop's Boxes window. The path down to what's
 * on screen is marked with a green dot and stays open.
 */
export default function BoxTree({ sports, total, cur }: { sports: TreeSport[]; total: number; cur: Params }) {
  const base = { edition: cur.edition };
  const row = (active: boolean, onPath: boolean, depth: number) =>
    `flex items-center gap-2 rounded-md py-1.5 pr-2 text-[14px] ${depth === 0 ? "pl-2" : depth === 1 ? "pl-5" : "pl-9"} ${
      active ? "bg-fire text-white" : onPath ? "font-semibold" : "hover:bg-wash"
    }`;
  const dot = (on: boolean) => <span className={`h-2 w-2 shrink-0 rounded-full ${on ? "bg-go" : "bg-transparent"}`} aria-hidden />;
  const count = (n: number) => <span className="num ml-auto shrink-0 text-[12px] opacity-75">{n.toLocaleString()}</span>;

  return (
    <nav aria-label="Categories and boxes" className="space-y-0.5">
      <p className="mb-2 px-2 text-[11px] font-bold uppercase tracking-wide text-muted">Categories / location</p>
      <Link href={href("/boxes", base)} className={row(!cur.sport, !cur.sport, 0)}>
        {dot(!cur.sport)} <span>All</span> {count(total)}
      </Link>
      {sports.map((s) => {
        const sportOn = cur.sport === s.sport;
        return (
          <div key={s.sport}>
            <Link href={href("/boxes", base, { sport: s.sport })} className={row(sportOn && !cur.box, sportOn, 0)}>
              {dot(sportOn)} <span className="text-muted" aria-hidden>{sportOn ? "▾" : "▸"}</span> <span className="truncate">{s.sport}</span> {count(s.cards)}
            </Link>
            {sportOn &&
              s.boxes.map((b) => {
                const boxOn = cur.box === b.id;
                return (
                  <div key={b.id}>
                    <Link href={href("/boxes", base, { sport: s.sport, box: b.id })} className={row(boxOn && !cur.row, boxOn, 1)}>
                      {dot(boxOn)} {b.rows.length > 0 && <span className="text-muted" aria-hidden>{boxOn ? "▾" : "▸"}</span>}
                      <span className="truncate">{b.label}</span> {count(b.cards)}
                    </Link>
                    {boxOn &&
                      b.rows.map((r) => {
                        const rowOn = cur.row === r.row;
                        return (
                          <Link key={r.row} href={href("/boxes", base, { sport: s.sport, box: b.id, row: r.row })} className={row(rowOn, rowOn, 2)}>
                            {dot(rowOn)} <span>Row {r.row}</span> {count(r.cards)}
                          </Link>
                        );
                      })}
                  </div>
                );
              })}
          </div>
        );
      })}
    </nav>
  );
}

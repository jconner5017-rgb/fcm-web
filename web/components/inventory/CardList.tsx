import Link from "next/link";
import { flags, gradeText, locationText, money, setLine, statusText, thumbUrl, type CardRow } from "@/lib/inventory";

function cardHref(c: CardRow, from: string) {
  return `/inventory/cards/${c.id}?from=${encodeURIComponent(from)}`;
}

function Status({ c }: { c: CardRow }) {
  const t = statusText(c.data);
  const live = t.startsWith("Live");
  return (
    <span className={`inline-block rounded-full px-2 py-0.5 text-[11px] font-bold uppercase tracking-wide ${live ? "bg-go text-white" : "border border-line text-muted"}`}>
      {t}
    </span>
  );
}

function Thumb({ c, className }: { c: CardRow; className: string }) {
  const src = thumbUrl(c.data);
  return src ? (
    // eslint-disable-next-line @next/next/no-img-element
    <img src={src} alt="" loading="lazy" decoding="async" className={`${className} bg-paper object-contain`} />
  ) : (
    <div className={`${className} flex items-center justify-center bg-paper text-[11px] text-muted`}>No photo</div>
  );
}

/** Photo tiles: two across on a phone, up to five on a wide screen. */
export function CardTiles({ cards, from }: { cards: CardRow[]; from: string }) {
  return (
    <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
      {cards.map((c) => {
        const tags = [gradeText(c.data), ...flags(c.data)].filter(Boolean);
        return (
          <li key={c.id}>
            <Link href={cardHref(c, from)} className="card group flex h-full flex-col !p-2 hover:border-fire">
              <Thumb c={c} className="aspect-[5/7] w-full rounded-md" />
              <div className="mt-2 flex flex-1 flex-col px-1">
                <p className="line-clamp-2 font-bold leading-snug group-hover:text-fire">{c.data.player_name || "Unnamed card"}</p>
                <p className="mt-0.5 line-clamp-2 text-[12px] text-muted">{setLine(c.data)}</p>
                {tags.length > 0 && <p className="mt-1 text-[11px] font-semibold text-fire">{tags.join(" · ")}</p>}
                <div className="mt-auto pt-2">
                  <div className="flex items-center justify-between gap-2">
                    <span className="num font-bold">{money(c.data.sale_price ?? c.data.estimated_value)}</span>
                    <Status c={c} />
                  </div>
                  <p className="mt-1 truncate text-[11px] text-muted">{locationText(c)}</p>
                </div>
              </div>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}

/** A dense table for scanning many cards; becomes a list on a phone. */
export function CardTable({ cards, from }: { cards: CardRow[]; from: string }) {
  return (
    <>
      <div className="hidden overflow-x-auto rounded-xl border border-line md:block">
        <table className="w-full text-left text-[14px]">
          <thead className="bg-wash text-[12px] uppercase tracking-wide text-muted">
            <tr>
              <th className="w-12 px-2 py-2"><span className="sr-only">Photo</span></th>
              <th className="px-2 py-2">Card</th>
              <th className="px-2 py-2">Location</th>
              <th className="px-2 py-2">Status</th>
              <th className="px-2 py-2 text-right">Value</th>
              <th className="px-2 py-2 text-right">Price</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {cards.map((c) => (
              <tr key={c.id} className="hover:bg-wash">
                <td className="px-2 py-1.5"><Thumb c={c} className="h-14 w-10 rounded" /></td>
                <td className="px-2 py-1.5">
                  <Link href={cardHref(c, from)} className="font-semibold hover:text-fire">{c.data.player_name || "Unnamed card"}</Link>
                  <p className="text-[12px] text-muted">
                    {setLine(c.data)}
                    {[gradeText(c.data), ...flags(c.data)].filter(Boolean).map((t) => ` · ${t}`).join("")}
                  </p>
                </td>
                <td className="px-2 py-1.5 text-[13px]">
                  {locationText(c)}
                  {c.data.sku && <p className="text-[12px] text-muted">{c.data.sku}</p>}
                </td>
                <td className="px-2 py-1.5"><Status c={c} /></td>
                <td className="num px-2 py-1.5 text-right">{money(c.data.estimated_value)}</td>
                <td className="num px-2 py-1.5 text-right font-semibold">{money(c.data.sale_price)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <ul className="divide-y divide-line rounded-xl border border-line md:hidden">
        {cards.map((c) => (
          <li key={c.id}>
            <Link href={cardHref(c, from)} className="flex gap-3 p-2">
              <Thumb c={c} className="h-20 w-14 shrink-0 rounded" />
              <div className="min-w-0 flex-1">
                <p className="truncate font-semibold">{c.data.player_name || "Unnamed card"}</p>
                <p className="truncate text-[12px] text-muted">{setLine(c.data)}</p>
                <p className="truncate text-[12px] text-muted">{locationText(c)}</p>
                <div className="mt-1 flex items-center justify-between gap-2">
                  <span className="num font-bold">{money(c.data.sale_price ?? c.data.estimated_value)}</span>
                  <Status c={c} />
                </div>
              </div>
            </Link>
          </li>
        ))}
      </ul>
    </>
  );
}

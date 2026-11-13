import { catName } from "@/lib/cats";
import Link from "next/link";
import { flags, locationCode, money, seasonText, statusText, thumbUrl, photoUrl, type CardRow } from "@/lib/inventory";

/**
 * A card as the desktop's Boxes window shows it: front and back side by
 * side, then its details, with the price on its own line at the bottom.
 * Every tile is the same height (the text is clamped), so the grid lines up.
 */
export default function BoxTile({ c, from }: { c: CardRow; from: string }) {
  const d = c.data;
  const front = c.thumb_url || thumbUrl(d);
  const back = c.back_thumb_url || photoUrl(d.back_thumb_photo_id || d.back_photo_id);
  const status = statusText(d);
  const live = status.startsWith("Live");
  const price = d.sale_price ?? d.estimated_value;
  const yearSet = [seasonText(d), d.brand && d.set_name?.toLowerCase().includes(d.brand.toLowerCase()) ? "" : d.brand, d.set_name,
    d.card_number ? `#${d.card_number}` : ""].filter(Boolean).join(" ") + (d.variety ? ` · ${d.variety}` : "");
  const tags = flags(d);
  // Phones get the small thumbnail; a computer screen (1024 px and wider) the full-size photo.
  const img = (src: string | null, side: string, full?: string) =>
    src ? (
      <picture className="block h-full w-full">
        {full && <source media="(min-width: 1024px)" srcSet={full} />}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src={src} alt={`${side} of ${d.player_name ?? "card"}`} loading="lazy" decoding="async" className="h-full w-full rounded-md object-contain" />
      </picture>
    ) : (
      <div className="flex h-full w-full items-center justify-center rounded-md text-[11px] text-muted">No {side.toLowerCase()}</div>
    );

  return (
    <Link href={`/inventory/cards/${c.id}?from=${encodeURIComponent(from)}`} className="card group flex h-full flex-col !p-3 hover:border-fire">
      <div className="grid h-60 grid-cols-2 gap-2 rounded-lg bg-paper p-1.5 lg:h-80">
        {img(front, "Front", c.front_url)}
        {img(back, "Back", c.back_url)}
      </div>

      <div className="mt-3 flex items-center justify-between gap-2">
        <span className="truncate text-[11px] font-bold uppercase tracking-wide text-link">{catName(d.sport)}</span>
        <span className={`shrink-0 whitespace-nowrap rounded-full px-2 py-0.5 text-[10px] font-bold uppercase ${live ? "bg-go text-white" : "border border-line text-muted"}`}>
          {status}
        </span>
      </div>
      <p className="mt-1 truncate text-[16px] font-bold group-hover:text-fire">{d.player_name || "(unnamed)"}</p>
      <p className="line-clamp-2 min-h-[2.6em] text-[12.5px] leading-[1.3] text-muted">{yearSet || "—"}</p>
      <p className="mt-1 h-4 truncate text-[11px] font-semibold text-fire">{tags.join(" · ")}</p>

      <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-2 text-[11.5px]">
        <dt className="text-muted">SKU</dt><dd className="truncate">{d.sku ?? "—"}</dd>
        <dt className="text-muted">Location</dt><dd className="truncate font-semibold">{locationCode(c)}</dd>
        <dt className="text-muted">Quality</dt><dd className="truncate uppercase">{d.condition || "—"}</dd>
      </dl>

      <p className="num mt-auto border-t border-line pt-2 text-right text-[21px] font-bold">{money(price) || "—"}</p>
    </Link>
  );
}

import Link from "next/link";
import { flags, locationCode, money, seasonText, statusText, thumbUrl, photoUrl, type CardRow } from "@/lib/inventory";

/** A card as the desktop's Boxes window shows it: front and back, then its details and price. */
export default function BoxTile({ c, from }: { c: CardRow; from: string }) {
  const d = c.data;
  const front = c.thumb_url || thumbUrl(d);
  const back = c.back_thumb_url || photoUrl(d.back_thumb_photo_id || d.back_photo_id);
  const status = statusText(d);
  const live = status.startsWith("Live");
  const price = d.sale_price ?? d.estimated_value;
  const yearSet = [seasonText(d), d.brand && d.set_name?.toLowerCase().includes(d.brand.toLowerCase()) ? "" : d.brand, d.set_name,
    d.card_number ? `#${d.card_number}` : ""].filter(Boolean).join(" ") + (d.variety ? ` · ${d.variety}` : "");
  const img = (src: string | null, side: string) =>
    src ? (
      // eslint-disable-next-line @next/next/no-img-element
      <img src={src} alt={`${side} of ${d.player_name ?? "card"}`} loading="lazy" decoding="async" className="aspect-[5/7] w-full rounded-md bg-wash object-contain" />
    ) : (
      <div className="flex aspect-[5/7] w-full items-center justify-center rounded-md bg-wash text-[11px] text-muted">No {side.toLowerCase()}</div>
    );

  return (
    <Link href={`/inventory/cards/${c.id}?from=${encodeURIComponent(from)}`}
      className="card group grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-3 !p-3 hover:border-fire sm:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)]">
      <div className="grid grid-cols-2 gap-1.5">
        {img(front, "Front")}
        {img(back, "Back")}
      </div>
      <div className="flex min-w-0 flex-col">
        <div className="flex items-start justify-between gap-2">
          <span className="truncate text-[11px] font-bold uppercase tracking-wide text-link">{d.sport ?? ""}</span>
          <span className={`shrink-0 whitespace-nowrap text-[11px] font-bold uppercase ${live ? "text-go" : "text-muted"}`}>{status}</span>
        </div>
        <p className="mt-1 line-clamp-2 text-[16px] font-bold leading-snug group-hover:text-fire">{d.player_name || "(unnamed)"}</p>
        <p className="mt-0.5 line-clamp-3 text-[12.5px] text-muted">{yearSet || "—"}</p>
        {flags(d).length > 0 && <p className="mt-1 text-[11px] font-semibold text-fire">{flags(d).join(" · ")}</p>}
        <p className="mt-1.5 truncate text-[11px] text-muted">SKU: {d.sku ?? "—"}</p>
        <p className="truncate text-[11px] text-muted">LOCATION: <span className="font-semibold text-ink">{locationCode(c)}</span></p>
        <div className="mt-auto flex items-end justify-between gap-2 pt-2">
          <div className="min-w-0">
            <p className="text-[10.5px] font-bold uppercase text-muted">Quality</p>
            <p className="truncate text-[12px] uppercase">{d.condition || "—"}</p>
          </div>
          <p className="num shrink-0 text-[21px] font-bold">{money(price) || "—"}</p>
        </div>
      </div>
    </Link>
  );
}

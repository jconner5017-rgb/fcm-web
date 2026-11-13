import Link from "next/link";
import { notFound, redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import CardActions from "@/components/inventory/CardActions";
import { editionName, shortDate, when } from "@/lib/client";
import {
  STATUS_LABELS, flags, gradeText, liveText, locationText, money, one, photoUrl, seasonText, setLine, type CardDetail,
} from "@/lib/inventory";

export const metadata = { title: "Card" };

function Photo({ id, url, label }: { id?: string | null; url?: string; label: string }) {
  const src = url || photoUrl(id);
  return (
    <figure className="min-w-0 flex-1">
      {src ? (
        <a href={src} target="_blank" rel="noopener" title={`Open the ${label.toLowerCase()} full size`}>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={src} alt={`${label} of the card`} className="aspect-[5/7] w-full rounded-lg border border-line bg-wash object-contain" />
        </a>
      ) : (
        <div className="flex aspect-[5/7] w-full items-center justify-center rounded-lg border border-dashed border-line text-[13px] text-muted">
          No {label.toLowerCase()} photo yet
        </div>
      )}
      <figcaption className="mt-1 text-center text-[12px] text-muted">{label}</figcaption>
    </figure>
  );
}

function Rows({ items }: { items: [string, React.ReactNode][] }) {
  const shown = items.filter(([, v]) => v !== "" && v !== null && v !== undefined && v !== false);
  if (shown.length === 0) return null;
  return (
    <dl className="grid grid-cols-[minmax(110px,auto)_1fr] gap-x-4 gap-y-1.5 text-[14px]">
      {shown.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-muted">{k}</dt>
          <dd className="min-w-0 break-words">{v}</dd>
        </div>
      ))}
    </dl>
  );
}

export default async function CardPage({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { id } = await params;
  const from = one((await searchParams).from);
  const back = from && from.startsWith("/inventory") && !from.startsWith("//") ? from : "/inventory";
  const { me } = await getMe();
  if (!me) redirect("/signin?next=" + encodeURIComponent(`/inventory/cards/${id}`));
  const res = await apiGet<CardDetail>(`/v1/inventory/cards/${encodeURIComponent(id)}`);
  if (res.status === 404) notFound();
  const c = res.data;
  if (!c) return <p className="card mt-8">This card couldn&apos;t be loaded just now. Refresh the page in a minute.</p>;
  const d = c.data;
  const live = liveText(d);
  const tags = [gradeText(d), ...flags(d)].filter(Boolean);

  return (
    <div className="mt-6">
      <Link href={back} className="text-[14px] font-semibold text-muted hover:text-fire">&larr; Back to inventory</Link>
      <div className="mt-4 grid gap-8 md:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
        <div className="flex gap-3">
          <Photo id={d.front_photo_id} url={c.front_url} label="Front" />
          <Photo id={d.back_photo_id} url={c.back_url} label="Back" />
        </div>

        <div className="min-w-0 space-y-5">
          <div>
            <h1 className="text-[28px] font-bold leading-tight">{d.player_name || "Unnamed card"}</h1>
            <p className="mt-1 text-muted">{setLine(d)}</p>
            {tags.length > 0 && <p className="mt-1 text-[13px] font-semibold text-fire">{tags.join(" · ")}</p>}
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <span className={`whitespace-nowrap rounded-full px-3 py-1 text-[12px] font-bold uppercase tracking-wide ${live ? "bg-go text-white" : "border border-line text-muted"}`}>
                {live || STATUS_LABELS[d.status ?? ""] || d.status}
              </span>
              {d.ebay_listing_url && (
                <a href={d.ebay_listing_url} target="_blank" rel="noopener noreferrer" className="text-[14px] font-semibold text-link">
                  View on eBay
                </a>
              )}
            </div>
          </div>

          <CardActions card={c} />

          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            <div className="card !p-3">
              <p className="text-[12px] text-muted">Estimated value</p>
              <p className="num text-[20px] font-bold">{money(d.estimated_value) || "-"}</p>
            </div>
            <div className="card !p-3">
              <p className="text-[12px] text-muted">{d.status === "EBA" ? "Start bid" : "Price"}</p>
              <p className="num text-[20px] font-bold">{money(d.status === "EBA" ? d.start_bid : d.sale_price) || "-"}</p>
            </div>
            {typeof d.card_sight_price === "number" && (
              <div className="card !p-3">
                <p className="text-[12px] text-muted">CardSight price</p>
                <p className="num text-[20px] font-bold">{money(d.card_sight_price)}</p>
              </div>
            )}
          </div>

          <section>
            <h2 className="mb-2 text-[13px] font-semibold uppercase tracking-wide text-muted">Where it is</h2>
            <Rows items={[["Location", locationText(c)], ["SKU", d.sku]]} />
          </section>

          <section>
            <h2 className="mb-2 text-[13px] font-semibold uppercase tracking-wide text-muted">Card</h2>
            <Rows
              items={[
                ["Player", d.player_name],
                ["Team", d.team],
                ["Sport", d.sport],
                ["Year", seasonText(d)],
                ["Brand", d.brand],
                ["Set", d.set_name],
                ["Number", d.card_number],
                ["Parallel / variety", d.variety],
                ["Condition", d.condition],
                ["Grading", gradeText(d)],
                ["Cert number", d.grading_cert_number],
                ["Serial number", d.serial_number],
              ]}
            />
          </section>

          <section>
            <h2 className="mb-2 text-[13px] font-semibold uppercase tracking-wide text-muted">Selling</h2>
            <Rows
              items={[
                ["Status", live ? live.replace("Live · ", "Live on ").replace("S&S", "Slabs & Singles").replace("eBay + Slabs & Singles", "eBay and Slabs & Singles") : STATUS_LABELS[d.status ?? ""] || d.status],
                ["Price", money(d.sale_price)],
                ["Start bid", d.status === "EBA" ? money(d.start_bid) : ""],
                ["Reserve", money(d.reserve_price)],
                ["Offers", d.allow_offers ? "Allowed" : ""],
                ["Paid", money(d.paid_price)],
                ["eBay item", d.ebay_listing_id],
                ["Slabs & Singles", d.marketplace_listing_id ? `Listed ${d.marketplace_listed_at ? shortDate(d.marketplace_listed_at) : ""}` : ""],
                ["Last priced", d.last_priced_at ? shortDate(d.last_priced_at) : ""],
                ["Pricing note", d.card_sight_note],
              ]}
            />
          </section>

          <p className="text-[12px] text-muted">
            {editionName(c.edition)} · added {shortDate(d.created_at)} · changed {when(c.updated_at)}. Changes made on your PC show here after its next sync.
          </p>
        </div>
      </div>
    </div>
  );
}

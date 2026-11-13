import Link from "next/link";
import { money, statusText, thumbUrl, type CardRow } from "@/lib/inventory";

export type DashboardData = {
  edition: string;
  priced_inventory_value: number;
  priced_count: number;
  total_active_cards: number;
  ready_to_list_count: number;
  ready_to_list_potential_value: number;
  live_count: number;
  ebay_live_count: number;
  slabs_live_count: number;
  both_live_count: number;
  ebay_sold_7d: number;
  slabs_sold_7d: number;
  revenue_7d: number;
  revenue_previous_7d: number;
  units_sold_7d: number;
  units_sold_previous_7d: number;
  ai_documented_count: number;
  needs_identification_count: number;
  missing_pricing_count: number;
  listing_errors_count: number;
  sell_through_rate: number;
  sales_31d: { day: string; revenue: number; units: number }[];
  recent: CardRow[];
};

const n = (v: number) => v.toLocaleString();
const pct = (part: number, whole: number) => (whole === 0 ? 0 : Math.round((part / whole) * 100));

function Kpi({ label, value, sub, href }: { label: string; value: string; sub?: React.ReactNode; href?: string }) {
  const body = (
    <>
      <p className="text-[12px] font-semibold uppercase tracking-wide text-muted">{label}</p>
      <p className="num mt-1 text-[26px] font-bold leading-tight">{value}</p>
      {sub && <p className="mt-1 text-[12px] text-muted">{sub}</p>}
    </>
  );
  return href ? <Link href={href} className="card block hover:border-fire">{body}</Link> : <div className="card">{body}</div>;
}

function Change({ now, before, isMoney }: { now: number; before: number; isMoney?: boolean }) {
  const diff = now - before;
  if (before === 0 && now === 0) return <span>no sales the week before either</span>;
  const up = diff >= 0;
  return (
    <span className={up ? "text-go" : "text-bad"}>
      {up ? "▲" : "▼"} {isMoney ? money(Math.abs(diff)) : n(Math.abs(diff))} vs the 7 days before
    </span>
  );
}

/** Daily revenue, last 31 days: one series, thin bars, hover for the day's numbers. */
function SalesChart({ days }: { days: DashboardData["sales_31d"] }) {
  const max = Math.max(...days.map((d) => d.revenue), 0);
  const total = days.reduce((s, d) => s + d.revenue, 0);
  const label = (d: string) => new Date(d + "T12:00:00Z").toLocaleDateString("en-US", { month: "short", day: "numeric", timeZone: "UTC" });
  return (
    <section className="card">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-[16px] font-bold">Sales, last 31 days</h2>
        <p className="num text-[13px] text-muted">{money(total)} · {n(days.reduce((s, d) => s + d.units, 0))} sold</p>
      </div>
      {max === 0 ? (
        <p className="mt-6 py-8 text-center text-[13px] text-muted">No sales in this period yet</p>
      ) : (
        <>
          <div className="relative mt-4 h-40" role="img" aria-label={`Daily sales for the last 31 days, ${money(total)} in total`}>
            <div className="absolute inset-x-0 top-0 border-t border-dashed border-line" aria-hidden />
            <span className="num absolute -top-2.5 right-0 bg-wash pl-1 text-[11px] text-muted">{money(max)}</span>
            <div className="absolute inset-0 flex items-end gap-[2px] border-b border-line">
              {days.map((d) => (
                <div key={d.day} className="group relative flex h-full flex-1 items-end">
                  <div className="w-full rounded-t-[4px] bg-fire" style={{ height: d.revenue > 0 ? `${Math.max(2, (d.revenue / max) * 100)}%` : 0 }} />
                  <div className="pointer-events-none absolute bottom-full left-1/2 z-10 mb-1 hidden -translate-x-1/2 whitespace-nowrap rounded-md border border-line bg-paper px-2 py-1 text-[12px] shadow group-hover:block">
                    <b>{label(d.day)}</b>: <span className="num">{money(d.revenue)}</span> · {d.units} sold
                  </div>
                </div>
              ))}
            </div>
          </div>
          <div className="mt-1 flex justify-between text-[11px] text-muted">
            <span>{label(days[0].day)}</span>
            <span>{label(days[days.length - 1].day)}</span>
          </div>
          <details className="mt-2 text-[12px]">
            <summary className="cursor-pointer text-muted">Show as a table</summary>
            <table className="num mt-2 w-full text-left">
              <thead className="text-muted"><tr><th className="py-0.5">Day</th><th className="text-right">Sales</th><th className="text-right">Cards</th></tr></thead>
              <tbody>
                {days.filter((d) => d.units > 0).map((d) => (
                  <tr key={d.day}><td className="py-0.5">{label(d.day)}</td><td className="text-right">{money(d.revenue)}</td><td className="text-right">{d.units}</td></tr>
                ))}
              </tbody>
            </table>
          </details>
        </>
      )}
    </section>
  );
}

function Meter({ label, part, whole, note }: { label: string; part: number; whole: number; note: string }) {
  const p = pct(part, whole);
  return (
    <div>
      <div className="flex justify-between text-[14px]">
        <span>{label}</span>
        <span className="num font-semibold">{p}%</span>
      </div>
      <div className="mt-1 h-2 overflow-hidden rounded-full bg-paper" aria-hidden>
        <div className="h-full rounded-full bg-go" style={{ width: `${p}%` }} />
      </div>
      <p className="num mt-0.5 text-[12px] text-muted">{note}</p>
    </div>
  );
}

/** The desktop app's Dashboard, from the synced inventory and Sold Log. */
export default function Dashboard({ d }: { d: DashboardData }) {
  const flow = [
    { label: "Captured", value: d.total_active_cards, note: "all active cards" },
    { label: "AI documented", value: d.ai_documented_count, note: `${pct(d.ai_documented_count, d.total_active_cards)}% of captured` },
    { label: "Ready", value: d.ready_to_list_count, note: "waiting to list" },
    { label: "Live", value: d.live_count, note: "eBay or Slabs & Singles" },
    { label: "Sold", value: d.units_sold_7d, note: "last 7 days" },
  ];
  const actions = [
    { label: "Cards to identify", note: "Scanned, not identified yet", value: d.needs_identification_count, href: "/inventory?q=pending" },
    { label: "Missing prices", note: "Identified, no price yet", value: d.missing_pricing_count, href: "/inventory?sort=value" },
    { label: "Listing errors", note: "Ready or live without a price", value: d.listing_errors_count, href: "/inventory?status=Ready&sort=value" },
  ];
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Kpi label="Priced inventory value" value={money(d.priced_inventory_value)} sub={`${n(d.priced_count)} of ${n(d.total_active_cards)} cards priced`} href="/inventory" />
        <Kpi label="Ready to list" value={n(d.ready_to_list_count)} sub={`${money(d.ready_to_list_potential_value)} potential`} href="/inventory?status=Ready" />
        <Kpi label="Live" value={n(d.live_count)} sub={`eBay ${n(d.ebay_live_count)} · S&S ${n(d.slabs_live_count)}`} href="/inventory?status=listed" />
        <Kpi label="Revenue, last 7 days" value={money(d.revenue_7d)} sub={<Change now={d.revenue_7d} before={d.revenue_previous_7d} isMoney />} href="/sold" />
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <SalesChart days={d.sales_31d} />
        <section className="card">
          <h2 className="text-[16px] font-bold">Action queue</h2>
          <p className="text-[13px] text-muted">The next work that moves inventory forward.</p>
          <ul className="mt-3 divide-y divide-line">
            {actions.map((a) => (
              <li key={a.label} className="flex items-center justify-between gap-3 py-2.5">
                <div>
                  <p className="text-[14px] font-semibold">{a.label}</p>
                  <p className="text-[12px] text-muted">{a.note}</p>
                </div>
                <span className={`num rounded-full px-2.5 py-0.5 text-[13px] font-bold ${a.value > 0 ? "bg-fire text-white" : "border border-line text-muted"}`}>
                  {n(a.value)}
                </span>
              </li>
            ))}
          </ul>
          <div className="mt-2 grid grid-cols-3 gap-2 border-t border-line pt-3 text-center">
            <div><p className="num text-[18px] font-bold">{money(d.revenue_7d)}</p><p className="text-[11px] text-muted">Revenue, 7 days</p></div>
            <div><p className="num text-[18px] font-bold">{n(d.units_sold_7d)}</p><p className="text-[11px] text-muted">Units sold</p></div>
            <div><p className="num text-[18px] font-bold">{d.sell_through_rate.toFixed(1)}%</p><p className="text-[11px] text-muted">Sell-through</p></div>
          </div>
        </section>
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <section className="card">
          <h2 className="text-[16px] font-bold">Inventory flow</h2>
          <p className="text-[13px] text-muted">Where every card sits right now, from first scan to sold.</p>
          <ol className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-5">
            {flow.map((f, i) => (
              <li key={f.label} className="rounded-lg border border-line bg-paper p-3">
                <p className="text-[12px] text-muted">{i + 1}. {f.label}</p>
                <p className="num text-[22px] font-bold">{n(f.value)}</p>
                <p className="text-[11px] text-muted">{f.note}</p>
              </li>
            ))}
          </ol>
          <h3 className="mt-4 text-[14px] font-bold">By marketplace</h3>
          <table className="num mt-1 w-full text-left text-[14px]">
            <thead className="text-[12px] text-muted"><tr><th className="py-1 font-normal">Marketplace</th><th className="text-right font-normal">Live</th><th className="text-right font-normal">Sold, 7 days</th></tr></thead>
            <tbody className="divide-y divide-line">
              <tr><td className="py-1.5 font-semibold">eBay</td><td className="text-right">{n(d.ebay_live_count)}</td><td className="text-right">{n(d.ebay_sold_7d)}</td></tr>
              <tr><td className="py-1.5 font-semibold">Slabs &amp; Singles</td><td className="text-right">{n(d.slabs_live_count)}</td><td className="text-right">{n(d.slabs_sold_7d)}</td></tr>
            </tbody>
          </table>
          {d.both_live_count > 0 && <p className="mt-1 text-[12px] text-muted">{n(d.both_live_count)} live on both.</p>}
        </section>
        <section className="card space-y-4">
          <div>
            <h2 className="text-[16px] font-bold">Inventory health</h2>
            <p className="text-[13px] text-muted">Coverage and AI completion.</p>
          </div>
          <Meter label="AI identified" part={d.ai_documented_count} whole={d.total_active_cards} note={`${n(d.needs_identification_count)} still to identify`} />
          <Meter label="Pricing coverage" part={d.priced_count} whole={d.total_active_cards} note={`${n(d.total_active_cards - d.priced_count)} without a price`} />
          <Meter label="Listing coverage" part={d.live_count} whole={d.total_active_cards} note={`${n(d.total_active_cards - d.live_count)} not listed`} />
        </section>
      </div>

      {d.recent.length > 0 && (
        <section className="card">
          <div className="flex items-baseline justify-between gap-2">
            <h2 className="text-[16px] font-bold">Recently added</h2>
            <Link href="/inventory?sort=added&dir=desc" className="text-[13px] font-semibold text-fire">See all</Link>
          </div>
          <ul className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-8">
            {d.recent.map((c) => {
              const src = c.thumb_url || thumbUrl(c.data);
              return (
                <li key={c.id}>
                  <Link href={`/inventory/cards/${c.id}`} className="group block">
                    {src ? (
                      // eslint-disable-next-line @next/next/no-img-element
                      <img src={src} alt="" loading="lazy" className="aspect-[5/7] w-full rounded-md bg-paper object-contain" />
                    ) : <div className="aspect-[5/7] w-full rounded-md bg-paper" />}
                    <p className="mt-1 truncate text-[12px] font-semibold group-hover:text-fire">{c.data.player_name}</p>
                    <p className="truncate text-[11px] text-muted">{money(c.data.sale_price ?? c.data.estimated_value)} · {statusText(c.data)}</p>
                  </Link>
                </li>
              );
            })}
          </ul>
        </section>
      )}
    </div>
  );
}

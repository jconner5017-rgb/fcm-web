import { apiGet } from "@/lib/api";
import { when } from "@/lib/client";

export const metadata = { title: "Resources · Back office" };
export const dynamic = "force-dynamic";

type Gauge = {
  key: string; group: string; label: string; used: number; limit?: number; unit: string; percent?: number;
  level: "ok" | "warn" | "high" | "info"; note?: string; upgrade?: string; peak?: boolean;
  peak_used?: number; peak_percent?: number;
};
type Snapshot = { at: string; db_bytes: number; photo_bytes: number; photos: number; cards: number; accounts: number };
type Report = {
  at: string; gauges: Gauge[]; counts: Record<string, number>; tables: { name: string; bytes: number; rows: number }[];
  growth?: { days: number; db_bytes_per_day: number; photo_bytes_per_day: number; cards_per_day: number; accounts_per_day: number; db_full_in_days?: number };
  render: string; uptime: string; history: Snapshot[];
};

function bytes(n: number) {
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (Math.abs(n) >= 1000 && i < u.length - 1) { n /= 1000; i++; }
  return `${n.toFixed(n >= 100 || i === 0 ? 0 : 1)} ${u[i]}`;
}
const amount = (g: Gauge, v: number) => (g.unit === "bytes" ? bytes(v) : g.unit === "cpu" ? `${v.toFixed(2)} CPU` : v.toLocaleString());

const COLORS = { ok: "bg-go", warn: "bg-[#d97706]", high: "bg-bad", info: "bg-muted" };
const WORDS = { ok: "Fine", warn: "Getting full", high: "Upgrade soon", info: "" };

const COUNTS: [string, string][] = [
  ["accounts", "Accounts"], ["active_30_days", "Active, 30 days"], ["linked_pcs", "Linked PCs"], ["cards", "Cards online"],
  ["sold", "Sold records"], ["photos", "Photos"], ["email_waiting", "Emails waiting"], ["listing_waiting", "Listings waiting"],
];

/** Back office > Resources: is anything close to its limit, and what to upgrade. */
export default async function ResourcesPage() {
  const res = await apiGet<Report>("/v1/admin/resources");
  if (!res.data) return <p className="card mt-6">The resource monitor couldn&apos;t be loaded just now. Refresh in a minute.</p>;
  const r = res.data;
  const groups = [...new Set(r.gauges.map((g) => g.group))];
  const high = r.gauges.filter((g) => g.level === "high");
  const warn = r.gauges.filter((g) => g.level === "warn");

  return (
    <div className="mt-6 space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <h1 className="text-[26px] font-bold">Resources</h1>
        <p className="text-[12px] text-muted">Checked {when(r.at)} · refreshes every minute{r.uptime ? ` · API up ${r.uptime}` : ""}</p>
      </div>

      <div className={`card border-2 ${high.length ? "border-bad" : warn.length ? "border-[#d97706]" : "border-go"}`}>
        <p className="text-[18px] font-bold">
          {high.length ? `Upgrade soon: ${high.map((g) => `${g.group} ${g.label.toLowerCase()}`).join(", ")}`
            : warn.length ? `Keep an eye on: ${warn.map((g) => `${g.group} ${g.label.toLowerCase()}`).join(", ")}`
            : "Everything has plenty of room."}
        </p>
        <p className="mt-1 text-[13px] text-muted">
          Yellow at 70% of a limit, red at 85%. Server memory and CPU go by the 24-hour average; a busiest hour over 95% also shows yellow. At red the owners get an email once a day, and it shows first on the Alerts page.
        </p>
      </div>

      {r.render !== "on" && (
        <p className="card text-[13px]">
          {r.render === "off" ? (
            <>CPU and memory of each server come from Render. To see them: in Render, Account settings &gt; API Keys &gt; Create API key,
              then add it to <b>fcm-api</b> &gt; Environment as <code>RENDER_API_KEY</code>. Until then this page shows the database, photos and the API server&apos;s own memory.</>
          ) : r.render}
        </p>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        {groups.map((group) => (
          <section key={group} className="card space-y-4">
            <h2 className="text-[16px] font-bold">{group}</h2>
            {r.gauges.filter((g) => g.group === group).map((g) => (
              <div key={g.key}>
                <div className="flex flex-wrap items-baseline justify-between gap-2 text-[14px]">
                  <span className="font-semibold">{g.label}</span>
                  <span className="num">
                    {amount(g, g.used)}{g.limit ? ` of ${amount(g, g.limit)}` : ""}
                    {g.percent != null && g.limit ? <b className="ml-2">{g.percent.toFixed(0)}%</b> : null}
                  </span>
                </div>
                {g.limit ? (
                  <div className="mt-1 h-2.5 overflow-hidden rounded-full bg-wash" aria-hidden>
                    <div className={`h-full rounded-full ${COLORS[g.level]}`} style={{ width: `${Math.min(100, Math.max(2, g.percent ?? 0))}%` }} />
                  </div>
                ) : null}
                {g.level !== "info" && g.level !== "ok" && <p className={`mt-1 text-[12px] font-semibold ${g.level === "high" ? "text-bad" : "text-[#d97706]"}`}>{WORDS[g.level]}</p>}
                {g.peak && g.peak_used ? (
                  <p className="mt-1 text-[12px] text-muted num">Busiest hour: {amount(g, g.peak_used)}{g.peak_percent != null ? ` (${g.peak_percent.toFixed(0)}%)` : ""}</p>
                ) : null}
                {g.note && <p className="mt-1 text-[12px] text-muted">{g.note}</p>}
                {(g.level === "warn" || g.level === "high") && g.upgrade && <p className="mt-1 text-[12px]"><b>What to do:</b> {g.upgrade}</p>}
              </div>
            ))}
          </section>
        ))}
      </div>

      <section className="card">
        <h2 className="text-[16px] font-bold">Growth</h2>
        {r.growth ? (
          <div className="mt-2 grid gap-3 text-[14px] sm:grid-cols-2 lg:grid-cols-4">
            <p><span className="block text-[12px] text-muted">Database a day</span><b className="num">{bytes(r.growth.db_bytes_per_day)}</b></p>
            <p><span className="block text-[12px] text-muted">Photos a day</span><b className="num">{bytes(r.growth.photo_bytes_per_day)}</b></p>
            <p><span className="block text-[12px] text-muted">Cards a day</span><b className="num">{Math.round(r.growth.cards_per_day).toLocaleString()}</b></p>
            <p><span className="block text-[12px] text-muted">New accounts a day</span><b className="num">{r.growth.accounts_per_day.toFixed(1)}</b></p>
            <p className="sm:col-span-2 lg:col-span-4 text-[13px]">
              {r.growth.db_full_in_days != null
                ? <>At this rate the database disk is full in about <b>{Math.max(0, Math.round(r.growth.db_full_in_days)).toLocaleString()} days</b> (measured over {Math.round(r.growth.days)} days).</>
                : "The database isn't growing."}
            </p>
          </div>
        ) : (
          <p className="mt-1 text-[13px] text-muted">A snapshot is taken once a day; growth shows after the second one.</p>
        )}
      </section>

      <section className="card">
        <h2 className="text-[16px] font-bold">Usage</h2>
        <div className="mt-2 grid grid-cols-2 gap-3 sm:grid-cols-4">
          {COUNTS.map(([k, label]) => (
            <p key={k}><span className="block text-[12px] text-muted">{label}</span><b className="num text-[18px]">{(r.counts[k] ?? 0).toLocaleString()}</b></p>
          ))}
        </div>
      </section>

      <div className="grid gap-4 lg:grid-cols-2">
        <section className="card">
          <h2 className="text-[16px] font-bold">Biggest tables</h2>
          <table className="mt-2 w-full text-[13px]">
            <thead><tr className="text-left text-muted"><th className="py-1">Table</th><th className="text-right">Rows</th><th className="text-right">Size</th></tr></thead>
            <tbody>
              {r.tables.map((t) => (
                <tr key={t.name} className="border-t border-line"><td className="py-1">{t.name}</td><td className="num text-right">{t.rows.toLocaleString()}</td><td className="num text-right">{bytes(t.bytes)}</td></tr>
              ))}
            </tbody>
          </table>
        </section>
        <section className="card">
          <h2 className="text-[16px] font-bold">Daily snapshots</h2>
          {r.history.length === 0 ? <p className="mt-1 text-[13px] text-muted">The first one is taken within an hour.</p> : (
            <table className="mt-2 w-full text-[13px]">
              <thead><tr className="text-left text-muted"><th className="py-1">Day</th><th className="text-right">Database</th><th className="text-right">Photos</th><th className="text-right">Cards</th><th className="text-right">Accounts</th></tr></thead>
              <tbody>
                {r.history.slice(-14).reverse().map((h) => (
                  <tr key={h.at} className="border-t border-line">
                    <td className="py-1">{new Date(h.at).toLocaleDateString()}</td><td className="num text-right">{bytes(h.db_bytes)}</td>
                    <td className="num text-right">{bytes(h.photo_bytes)}</td><td className="num text-right">{h.cards.toLocaleString()}</td><td className="num text-right">{h.accounts.toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      </div>
    </div>
  );
}

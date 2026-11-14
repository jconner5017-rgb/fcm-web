import Link from "next/link";
import { apiGet } from "@/lib/api";
import AlertList, { type Alert } from "@/components/admin/AlertList";

type Overview = Record<string, number>;

const TILES: [string, string, string?][] = [
  ["accounts", "Accounts"], ["on_trial", "On free trial", "trial"], ["paid", "Paid", "paid"], ["bundle", "Bundle"],
  ["off", "Sync off", "off"], ["suspended", "Suspended", "suspended"], ["closing", "Closing", "closing"], ["expiring_in_14", "Plans ending in 14 days"],
  ["signups_1", "Sign-ups today"], ["signups_7", "Sign-ups, 7 days"], ["signups_30", "Sign-ups, 30 days"], ["trials_reused_30", "Trials reused, 30 days", "reused"],
  ["blocked_signups_7", "Bot sign-ups stopped, 7 days"], ["failed_sign_ins_1", "Wrong passwords today"], ["linked_pcs", "Linked PCs"], ["cards", "Cards online"],
];

export default async function BackOfficeHome() {
  const [ov, al] = await Promise.all([apiGet<Overview>("/v1/admin/overview"), apiGet<{ alerts: Alert[] }>("/v1/admin/alerts")]);
  const o = ov.data ?? {};
  const alerts = al.data?.alerts ?? [];
  return (
    <div className="mt-6 space-y-6">
      <h1 className="text-[26px] font-bold">Overview</h1>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {TILES.map(([k, label, filter]) => {
          const body = (
            <>
              <p className="text-[12px] font-semibold uppercase tracking-wide text-muted">{label}</p>
              <p className="num mt-1 text-[24px] font-bold">{(o[k] ?? 0).toLocaleString()}</p>
            </>
          );
          return filter
            ? <Link key={k} href={`/admin/members?filter=${filter}`} className="card block hover:border-fire">{body}</Link>
            : <div key={k} className="card">{body}</div>;
        })}
      </div>
      <section>
        <div className="flex items-end justify-between">
          <h2 className="text-[18px] font-bold">Suspicious activity ({alerts.length})</h2>
          <Link href="/admin/alerts" className="text-[14px] font-semibold text-fire">All alerts</Link>
        </div>
        <AlertList initial={alerts.slice(0, 5)} />
      </section>
    </div>
  );
}

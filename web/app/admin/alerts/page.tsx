import { apiGet } from "@/lib/api";
import AlertList, { type Alert } from "@/components/admin/AlertList";

export default async function Alerts() {
  const r = await apiGet<{ alerts: Alert[] }>("/v1/admin/alerts");
  return (
    <div className="mt-6">
      <h1 className="text-[26px] font-bold">Suspicious activity</h1>
      <p className="mt-1 text-[14px] text-muted">
        Free trials reused (ended automatically), several accounts from one network, sign-ups stopped as bots, password guessing,
        one account signed in from many networks, and one PC linked to several accounts.
      </p>
      <AlertList initial={r.data?.alerts ?? []} />
    </div>
  );
}

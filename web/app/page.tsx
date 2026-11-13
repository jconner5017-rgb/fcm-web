import Link from "next/link";
import { apiGet, getMe } from "@/lib/api";
import { ADDON_PRICE, addonActive, editionName, shortDate, when } from "@/lib/client";
import type { Device } from "@/lib/types";
import ResendVerification from "@/components/ResendVerification";

export default async function Home() {
  const { me } = await getMe();
  if (!me) {
    return (
      <section className="mx-auto mt-14 max-w-[720px] text-center">
        <h1 className="text-[34px] font-bold leading-tight">Your FireCardManager inventory, from any device</h1>
        <p className="mt-4 text-[17px] text-muted">
          Keep scanning on your PC. Check boxes, rows, prices and what&apos;s sold from a Mac, a phone or any other computer.
        </p>
        <div className="mt-8 flex flex-wrap justify-center gap-3">
          <Link href="/signup" className="btn">Create account</Link>
          <Link href="/signin" className="btn-ghost">Sign in</Link>
        </div>
        <p className="mt-6 text-[13px] text-muted">30 days of sync free. Then the web add-on: {ADDON_PRICE}. The desktop app keeps working either way.</p>
      </section>
    );
  }

  const devices = (await apiGet<{ devices: Device[] }>("/v1/devices")).data?.devices ?? [];
  const a = me.account;
  const trialEnds = new Date(a.sync_trial_ends_at);
  const trialLeft = Math.ceil((trialEnds.getTime() - Date.now()) / 86400000);

  return (
    <div className="mt-8 space-y-6">
      <h1 className="text-[26px] font-bold">Welcome back</h1>
      {!a.email_verified && (
        <div className="card flex flex-wrap items-center justify-between gap-3 !border-fire">
          <p>Confirm your email address ({a.email}) so you can reset your password if you ever need to.</p>
          <ResendVerification />
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-3">
        <div className="card">
          <p className="text-[13px] font-semibold uppercase tracking-wide text-muted">Cloud sync</p>
          {addonActive(a) ? (
            <>
              <p className="mt-2 text-[18px] font-bold text-go">Web add-on active</p>
              <p className="mt-1 text-[13px] text-muted">Runs until {shortDate(a.web_addon_until)}. Renew along with your FCM license.</p>
            </>
          ) : me.sync_allowed ? (
            <>
              <p className="mt-2 text-[18px] font-bold">Free trial: {trialLeft} day{trialLeft === 1 ? "" : "s"} left</p>
              <p className="mt-1 text-[13px] text-muted">Ends {shortDate(a.sync_trial_ends_at)}. Then the web add-on: {ADDON_PRICE}.</p>
            </>
          ) : (
            <>
              <p className="mt-2 text-[18px] font-bold text-bad">Sync is off</p>
              <p className="mt-1 text-[13px] text-muted">The trial or add-on has ended. The web add-on is {ADDON_PRICE}. Contact FireCardManager to add it.</p>
            </>
          )}
        </div>
        <div className="card">
          <p className="text-[13px] font-semibold uppercase tracking-wide text-muted">Linked PCs</p>
          <p className="mt-2 text-[18px] font-bold">{devices.length}</p>
          <Link href="/devices" className="mt-1 inline-block text-[14px] font-semibold text-fire">Manage</Link>
        </div>
        <div className="card">
          <p className="text-[13px] font-semibold uppercase tracking-wide text-muted">Card limit</p>
          <p className="mt-2 text-[18px] font-bold num">{me.card_limit.toLocaleString()} cards</p>
          <p className="mt-1 text-[13px] text-muted">Cards in inventory. Sold cards don&apos;t count.</p>
        </div>
      </div>

      {devices.length === 0 ? (
        <section className="card">
          <h2 className="text-[18px] font-bold">Link your PC</h2>
          <ol className="mt-3 list-decimal space-y-2 pl-5">
            <li>In FireCardManager on your PC, open <b>Settings &gt; Cloud Sync</b> and click <b>Link this PC</b>.</li>
            <li>The app opens this website with a code. Check the PC name and click <b>Approve</b>.</li>
            <li>Your boxes, cards and photos start uploading in the background.</li>
          </ol>
          <p className="mt-3 text-[13px] text-muted">Cloud Sync arrives in the next FireCardManager desktop update.</p>
        </section>
      ) : (
        <section className="card">
          <h2 className="text-[18px] font-bold">Your PCs</h2>
          <ul className="mt-3 divide-y divide-line">
            {devices.map((d) => (
              <li key={d.id} className="flex flex-wrap justify-between gap-2 py-2">
                <span><b>{d.name}</b> · {editionName(d.edition)}</span>
                <span className="text-muted">Last sync {when(d.last_seen_at)}</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="card">
        <h2 className="text-[18px] font-bold">Coming next</h2>
        <p className="mt-2 text-muted">
          Browsing your boxes and cards, search, price edits and marking cards sold from the web arrive in the next updates.
        </p>
      </section>
    </div>
  );
}

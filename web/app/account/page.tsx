import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import AIKeyForm, { type AIKey } from "@/components/AIKeyForm";
import MarketKeyForm, { type MarketKey } from "@/components/MarketKeyForm";
import EbayConnect, { type EbayStatus } from "@/components/EbayConnect";
import { addonActive, shortDate } from "@/lib/client";
import { trialReuseText } from "@/lib/types";
import { ChangePassword, TwoFactor } from "@/components/AccountForms";
import ResendVerification from "@/components/ResendVerification";
import CloseAccount from "@/components/CloseAccount";

export const metadata = { title: "Account" };

export default async function AccountPage() {
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/account");
  const a = me.account;
  const [aiRes, mkRes, ebRes] = await Promise.all([apiGet<{ ai_key: AIKey | null }>("/v1/me/ai-key"), apiGet<{ keys: MarketKey[] }>("/v1/me/market-keys"),
    apiGet<EbayStatus>("/v1/ebay")]);
  const aiKey = aiRes.data?.ai_key ?? null;
  const slabsKey = mkRes.data?.keys.find((k) => k.market === "slabs") ?? null;
  return (
    <div className="mt-8 max-w-[640px] space-y-6">
      <h1 className="text-[26px] font-bold">Account</h1>
      <section className="card space-y-1">
        <p><span className="text-muted">Email:</span> <b>{a.email}</b> {a.email_verified ? <span className="text-go">(confirmed)</span> : <span className="text-bad">(not confirmed)</span>}</p>
        {!a.email_verified && <ResendVerification />}
        <p><span className="text-muted">Member since:</span> {shortDate(a.created_at)}</p>
        <p>
          <span className="text-muted">Cloud sync:</span>{" "}
          {addonActive(a) ? `paid until ${shortDate(a.web_addon_until)}` : me.sync_allowed ? `free trial until ${shortDate(a.sync_trial_ends_at)}` : "off (trial or add-on ended)"}
        </p>
        {!addonActive(a) && trialReuseText(a.trial_ended_reason) && (
          <p className="text-[13px] text-muted">No free trial on this account: {trialReuseText(a.trial_ended_reason)}. One free trial per seller.</p>
        )}
      </section>
      <section className="card" id="ai-key">
        <h2 className="text-[18px] font-bold">AI key for phone identification</h2>
        <AIKeyForm initial={aiKey} />
      </section>
      <section className="card" id="ebay">
        <h2 className="text-[18px] font-bold">eBay</h2>
        <EbayConnect initial={ebRes.data ?? { configured: false, connected: false }} />
      </section>
      <section className="card" id="market-keys">
        <h2 className="text-[18px] font-bold">Slabs &amp; Singles key for the Bulk Lister</h2>
        <MarketKeyForm initial={slabsKey} />
      </section>
      <section className="card">
        <h2 className="text-[18px] font-bold">Two-factor sign-in</h2>
        <TwoFactor enabled={me.mfa_enabled} />
      </section>
      <section className="card">
        <h2 className="text-[18px] font-bold">Change password</h2>
        <ChangePassword />
      </section>
      <section className="card" id="close">
        <h2 className="text-[18px] font-bold">Close account</h2>
        <CloseAccount />
      </section>
    </div>
  );
}

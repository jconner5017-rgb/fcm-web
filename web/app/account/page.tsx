import { redirect } from "next/navigation";
import { getMe } from "@/lib/api";
import { shortDate } from "@/lib/client";
import { ChangePassword, TwoFactor } from "@/components/AccountForms";
import ResendVerification from "@/components/ResendVerification";

export const metadata = { title: "Account" };

export default async function AccountPage() {
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/account");
  const a = me.account;
  return (
    <div className="mt-8 max-w-[640px] space-y-6">
      <h1 className="text-[26px] font-bold">Account</h1>
      <section className="card space-y-1">
        <p><span className="text-muted">Email:</span> <b>{a.email}</b> {a.email_verified ? <span className="text-go">(confirmed)</span> : <span className="text-bad">(not confirmed)</span>}</p>
        {!a.email_verified && <ResendVerification />}
        <p><span className="text-muted">Member since:</span> {shortDate(a.created_at)}</p>
        <p>
          <span className="text-muted">Cloud sync:</span>{" "}
          {a.web_addon_at ? `web add-on (since ${shortDate(a.web_addon_at)})` : me.sync_allowed ? `free trial until ${shortDate(a.sync_trial_ends_at)}` : "trial ended"}
        </p>
      </section>
      <section className="card">
        <h2 className="text-[18px] font-bold">Two-factor sign-in</h2>
        <TwoFactor enabled={me.mfa_enabled} />
      </section>
      <section className="card">
        <h2 className="text-[18px] font-bold">Change password</h2>
        <ChangePassword />
      </section>
    </div>
  );
}

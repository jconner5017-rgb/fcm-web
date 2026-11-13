import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import type { PendingLink } from "@/lib/types";
import Panel from "@/components/Panel";
import { ApproveLink, EnterCode } from "@/components/LinkForms";

export const metadata = { title: "Link a PC" };

export default async function LinkPage({ searchParams }: { searchParams: Promise<{ code?: string }> }) {
  const { code } = await searchParams;
  const { me } = await getMe();
  if (!me) redirect("/signin?next=" + encodeURIComponent("/link" + (code ? `?code=${code}` : "")));

  if (!code) {
    return (
      <Panel title="Link a PC">
        <p className="mt-2 text-muted">Enter the code FireCardManager shows under Settings &gt; Cloud Sync.</p>
        <EnterCode />
      </Panel>
    );
  }

  const r = await apiGet<PendingLink>("/v1/devices/link/" + encodeURIComponent(code));
  if (!r.data) {
    return (
      <Panel title="Link a PC">
        <p className="mt-4">
          {r.code === "code_used" ? "That code was already approved. Check the desktop app." : "That code wasn't found or has expired (codes last 10 minutes). Start again from the desktop app."}
        </p>
        <EnterCode />
      </Panel>
    );
  }
  return (
    <Panel title="Link this PC to your account?">
      <ApproveLink link={r.data} email={me.account.email} />
    </Panel>
  );
}

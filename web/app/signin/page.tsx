import { redirect } from "next/navigation";
import { getMe } from "@/lib/api";
import { SignInForm } from "@/components/AuthForms";
import Panel from "@/components/Panel";

export const metadata = { title: "Sign in" };

export default async function SignInPage({ searchParams }: { searchParams: Promise<{ next?: string; closed?: string }> }) {
  const { next, closed } = await searchParams;
  const { me, mfaPending } = await getMe();
  if (me) redirect(next && next.startsWith("/") && !next.startsWith("//") ? next : "/");
  return (
    <Panel title="Sign in">
      {closed && <p role="status" className="mb-4 rounded-lg border border-go px-3 py-2 text-[14px]">Your account is closed and its online data is being deleted. Thanks for using FireCardManager Web.</p>}
      <SignInForm next={next} startWithCode={mfaPending} />
    </Panel>
  );
}

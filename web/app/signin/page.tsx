import { redirect } from "next/navigation";
import { getMe } from "@/lib/api";
import { SignInForm } from "@/components/AuthForms";
import Panel from "@/components/Panel";

export const metadata = { title: "Sign in" };

export default async function SignInPage({ searchParams }: { searchParams: Promise<{ next?: string }> }) {
  const { next } = await searchParams;
  const { me, mfaPending } = await getMe();
  if (me) redirect(next && next.startsWith("/") && !next.startsWith("//") ? next : "/");
  return (
    <Panel title="Sign in">
      <SignInForm next={next} startWithCode={mfaPending} />
    </Panel>
  );
}

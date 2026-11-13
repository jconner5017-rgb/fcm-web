import { redirect } from "next/navigation";
import { getMe } from "@/lib/api";
import { SignUpForm } from "@/components/AuthForms";
import Panel from "@/components/Panel";

export const metadata = { title: "Create account" };

export default async function SignUpPage({ searchParams }: { searchParams: Promise<{ next?: string }> }) {
  const { next } = await searchParams;
  const { me } = await getMe();
  if (me) redirect(next && next.startsWith("/") && !next.startsWith("//") ? next : "/");
  return (
    <Panel title="Create your FireCardManager Web account">
      <p className="mt-2 text-muted">See and edit your FireCardManager inventory from a Mac, a phone or any PC.</p>
      <SignUpForm next={next} />
    </Panel>
  );
}

import { ResetForm } from "@/components/AuthForms";
import Panel from "@/components/Panel";

export const metadata = { title: "Choose a new password" };

export default async function ResetPage({ searchParams }: { searchParams: Promise<{ token?: string }> }) {
  const { token } = await searchParams;
  return (
    <Panel title="Choose a new password">
      {token ? <ResetForm token={token} /> : <p className="mt-4">This link is missing its code. Open the link from the email again.</p>}
    </Panel>
  );
}

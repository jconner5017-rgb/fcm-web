import { ForgotForm } from "@/components/AuthForms";
import Panel from "@/components/Panel";

export const metadata = { title: "Reset your password" };

export default function ForgotPage() {
  return (
    <Panel title="Reset your password">
      <p className="mt-2 text-muted">Enter your account email and we&apos;ll send you a link to choose a new password.</p>
      <ForgotForm />
    </Panel>
  );
}

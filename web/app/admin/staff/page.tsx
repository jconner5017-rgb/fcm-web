import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import StaffManager from "@/components/admin/StaffManager";
import IPRules from "@/components/admin/IPRules";

export default async function StaffPage() {
  const { me } = await getMe();
  if (!me?.account.is_admin) redirect("/admin");
  const [r, ip] = await Promise.all([apiGet<{ staff: Parameters<typeof StaffManager>[0]["initial"] }>("/v1/admin/staff"),
    apiGet<Parameters<typeof IPRules>[0]["initial"]>("/v1/admin/ip-rules")]);
  return (
    <div className="mt-6">
      <h1 className="text-[26px] font-bold">Staff</h1>
      <p className="mt-1 text-[14px] text-muted">Only the accounts you give a role here can open the back office.</p>
      <StaffManager initial={r.data?.staff ?? []} />
      {ip.data && <IPRules initial={ip.data} />}
    </div>
  );
}

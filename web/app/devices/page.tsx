import { redirect } from "next/navigation";
import { apiGet, getMe } from "@/lib/api";
import type { Device } from "@/lib/types";
import DeviceList from "@/components/DeviceList";

export const metadata = { title: "Linked PCs" };

export default async function DevicesPage() {
  const { me } = await getMe();
  if (!me) redirect("/signin?next=/devices");
  const devices = (await apiGet<{ devices: Device[] }>("/v1/devices")).data?.devices ?? [];
  return (
    <div className="mt-8 max-w-[760px]">
      <h1 className="text-[26px] font-bold">Linked PCs</h1>
      <p className="mt-2 text-muted">Each FireCardManager app you linked. Removing one stops it syncing at once; its own inventory stays on that PC.</p>
      <DeviceList initial={devices} />
    </div>
  );
}

"use client";

import { useState } from "react";
import { send } from "@/lib/client";

export default function ResendVerification() {
  const [msg, setMsg] = useState("");
  async function go() {
    const r = await send<{ sent: boolean }>("POST", "/v1/me/resend-verification");
    setMsg(!r.ok ? r.message : r.data.sent ? "Sent. Check your inbox." : "Email isn't set up on this server yet.");
  }
  return msg ? <span className="text-[14px]">{msg}</span> : <button type="button" onClick={go} className="btn-ghost text-[14px]">Send link again</button>;
}

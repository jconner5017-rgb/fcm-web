"use client";

import { send } from "@/lib/client";

export default function SignOutButton() {
  async function out() {
    await send("POST", "/v1/auth/logout");
    window.location.href = "/signin";
  }
  return (
    <button type="button" onClick={out} className="font-semibold text-muted hover:text-fire">
      Sign out
    </button>
  );
}

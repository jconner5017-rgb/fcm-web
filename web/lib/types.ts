export type Account = {
  id: string;
  email: string;
  email_verified: boolean;
  is_admin: boolean;
  status: string;
  sync_trial_ends_at: string;
  web_addon_at?: string;
  web_addon_until?: string;
  web_plan?: "web" | "bundle" | "addon" | "standalone" | "";
  created_at: string;
  trial_ended_reason?: "email" | "pc" | "inventory" | "allowed" | "";
};

/** Why a free trial ended early (another account already had one), in words. */
export function trialReuseText(r?: string): string {
  return r === "email" ? "this email address already had a FireCardManager Web free trial on another account"
    : r === "pc" ? "a PC linked to it already had a FireCardManager Web free trial on another account"
    : r === "inventory" ? "its inventory matches an account that already had a FireCardManager Web free trial"
    : "";
}

export type Me = {
  account: Account;
  mfa_enabled: boolean;
  sync_allowed: boolean;
  editions: string[];
  card_limit: number;
};

export type Device = {
  id: string;
  name: string;
  edition: "sports" | "tcg";
  app_version?: string;
  created_at: string;
  last_seen_at?: string;
};

export type PendingLink = {
  user_code: string;
  device_name: string;
  edition: "sports" | "tcg";
  license_state: "valid" | "no_expiry" | "grace" | "trial";
  license_expires_at?: string;
  expires_at: string;
};

export type AdminAccount = Account & {
  web_addon_note?: string;
  devices: number;
  cards: number;
  last_sync?: string;
};

export type ApiError = { error: { code: string; message: string; fields?: Record<string, string> } };

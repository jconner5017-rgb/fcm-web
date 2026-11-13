// Shapes and formatting for the inventory pages. Card fields come straight
// from the desktop app, named like its properties in snake_case.

export type CardData = {
  sport?: string;
  player_name?: string;
  team?: string | null;
  year?: number;
  season_end?: number | null;
  brand?: string;
  set_name?: string;
  card_number?: string;
  variety?: string | null;
  condition?: string;
  estimated_value?: number;
  slot_row?: string | null;
  slot_number?: number | null;
  status?: string;
  sale_price?: number | null;
  start_bid?: number | null;
  reserve_price?: number | null;
  paid_price?: number | null;
  allow_offers?: boolean;
  ebay_listing_url?: string | null;
  ebay_listing_id?: string | null;
  marketplace_listing_id?: string | null;
  marketplace_listed_at?: string | null;
  is_rookie?: boolean;
  is_autograph?: boolean;
  is_memorabilia?: boolean;
  is_short_print?: boolean;
  is_error_variant?: boolean;
  is_blank_back?: boolean;
  is_chase?: boolean;
  is_foil?: boolean;
  is_sketch_card?: boolean;
  grading_company?: string | null;
  grading_cert_number?: string | null;
  card_grade?: string | null;
  serial_number?: string | null;
  card_sight_price?: number | null;
  card_sight_note?: string | null;
  last_priced_at?: string | null;
  created_at?: string;
  updated_at?: string;
  sku?: string | null;
  front_photo_id?: string | null;
  back_photo_id?: string | null;
  front_thumb_photo_id?: string | null;
  back_thumb_photo_id?: string | null;
};

// thumb_url / front_url / back_url: signed links straight to photo storage (fast, cacheable for the hour).
export type CardRow = { id: string; box_id?: string; box_name?: string; updated_at: string; data: CardData; thumb_url?: string; back_thumb_url?: string; front_url?: string; back_url?: string };
export type CardDetail = CardRow & { edition: string; version: number; front_url?: string; back_url?: string };

export type InvSummary = {
  editions: { edition: string; cards: number }[];
  edition: string;
  boxes: { id: string; name: string; description?: string; color_theme?: string; cards: number; value: number; rows: { row: string; cards: number }[] }[];
  unboxed: number;
  cards: number;
  value: number;
  by_status: Record<string, number>;
  sold: number;
  conflicts: number;
  last_sync?: string;
};

export type CardPage = { edition: string; cards: CardRow[]; total: number; page: number; per_page: number };

export type SoldData = {
  sport?: string;
  player_name?: string;
  team?: string | null;
  year?: number;
  brand?: string;
  set_name?: string;
  card_number?: string;
  variety?: string | null;
  condition?: string;
  estimated_value_at_sale?: number | null;
  sale_price?: number | null;
  shipping_charged?: number | null;
  buyer?: string | null;
  sale_date?: string | null;
  notes?: string | null;
  tracking_number?: string | null;
  carrier?: string | null;
  shipped_date_utc?: string | null;
  sold_outside_ebay?: boolean;
  marketplace_order_id?: string | null;
  ebay_order_id?: string | null;
};
export type SoldPage = { edition: string; sold: { id: string; data: SoldData }[]; total: number; revenue: number; page: number; per_page: number };

const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });
export function money(n?: number | null): string {
  return typeof n === "number" ? usd.format(n) : "";
}

/** 2023 or 2023-24. */
export function seasonText(d: { year?: number; season_end?: number | null }): string {
  if (!d.year) return "";
  if (d.season_end && d.season_end !== d.year) return `${d.year}-${String(d.season_end).slice(-2)}`;
  return String(d.year);
}

/** "2023-24 Panini Prizm #12 · Silver" */
export function setLine(d: CardData | SoldData): string {
  const brand = d.brand && d.set_name && d.set_name.toLowerCase().includes(d.brand.toLowerCase()) ? "" : d.brand;
  const parts = [seasonText(d as CardData), brand, d.set_name, d.card_number ? `#${d.card_number}` : ""].filter(Boolean);
  return parts.join(" ") + (d.variety ? ` · ${d.variety}` : "");
}

export function gradeText(d: CardData): string {
  if (!d.grading_company && !d.card_grade) return "";
  return [d.grading_company, d.card_grade].filter(Boolean).join(" ");
}

export function locationText(c: { box_name?: string; data: CardData }): string {
  if (!c.box_name) return "Not in a box";
  const parts = [c.box_name];
  if (c.data.slot_row) parts.push(`Row ${c.data.slot_row}`);
  if (c.data.slot_number) parts.push(`Slot ${c.data.slot_number}`);
  return parts.join(" · ");
}

export const STATUS_LABELS: Record<string, string> = {
  NotForSale: "Not for sale",
  Ready: "Ready to list",
  EBA: "eBay auction",
  EBF: "eBay Buy It Now",
  Sold: "Sold",
};

/** Where the card is live: eBay, Slabs & Singles, both, or nowhere. */
export function liveText(d: CardData): string {
  const ebay = d.status === "EBA" || d.status === "EBF";
  const ss = !!d.marketplace_listing_id;
  if (ebay && ss) return "Live · eBay + S&S";
  if (ebay) return d.status === "EBA" ? "Live · eBay auction" : "Live · eBay";
  if (ss) return "Live · S&S";
  return "";
}

export function statusText(d: CardData): string {
  return liveText(d) || STATUS_LABELS[d.status ?? ""] || d.status || "";
}

export function flags(d: CardData): string[] {
  const out: string[] = [];
  if (d.is_rookie) out.push("RC");
  if (d.is_autograph) out.push("Auto");
  if (d.is_memorabilia) out.push("Relic");
  if (d.is_short_print) out.push("SP");
  if (d.serial_number) out.push(d.serial_number.includes("/") ? d.serial_number : `#/${d.serial_number}`);
  if (d.is_chase) out.push("Chase");
  if (d.is_foil) out.push("Foil");
  if (d.is_sketch_card) out.push("Sketch");
  if (d.is_error_variant) out.push("Error");
  return out;
}

export function photoUrl(id?: string | null): string | null {
  return id ? `/api/v1/photos/${encodeURIComponent(id)}` : null;
}

export function thumbUrl(d: CardData): string | null {
  return photoUrl(d.front_thumb_photo_id || d.front_photo_id);
}

export type Params = Record<string, string | undefined>;

/** /inventory?…, keeping the current filters and applying changes (undefined or "" drops one). */
export function href(base: string, cur: Params, change: Params = {}): string {
  const p = new URLSearchParams();
  const all = { ...cur, ...change };
  for (const [k, v] of Object.entries(all)) if (v) p.set(k, v);
  const s = p.toString();
  return s ? `${base}?${s}` : base;
}

/** First value of a Next.js search param. */
export function one(v: string | string[] | undefined): string | undefined {
  return Array.isArray(v) ? v[0] : v;
}

// ---------------- Boxes page ----------------

export type TreeBox = { id: string; name: string; label: string; cards: number; rows: { row: string; cards: number }[] };
export type TreeSport = { sport: string; cards: number; boxes: TreeBox[] };

/** The desktop's location prefixes (LocationPrefixHelper), Sports and TCG. */
const PREFIX: Record<string, string> = {
  Basketball: "BK", Football: "FT", Baseball: "BL", Hockey: "HK", Soccer: "SC", Fighting: "UFC", Wrestling: "WW", Racing: "RC", Boxing: "BC",
  Golf: "GF", Mma: "MA", Multisport: "MS", Tennis: "TN",
  Entertainment: "EN", Superheroes: "SH", SciFi: "SF", Fantasy: "FN", Anime: "AN", Horror: "HR", Music: "MU", Historical: "HI", Novelty: "NV",
  Mixed: "MX", Pokemon: "PK", Magic: "MG", YuGiOh: "YG", OnePiece: "OP", Lorcana: "LC", DragonBall: "DB", Digimon: "DG", FleshAndBlood: "FB",
  FinalFantasy: "FF", StarWarsTcg: "SW", WeissSchwarz: "WS", Vanguard: "VG", Naruto: "NR", MetaZoo: "MZ", Riftbound: "RB", WorldOfWarcraft: "WC",
  MyLittlePony: "MP", Sorcery: "SC", Netrunner: "NT", UniVersus: "UV", OtherTcg: "OT",
};

export function boxNumber(name: string): number {
  const m = /\(cont\. (\d+)\)/i.exec(name);
  return m ? Number(m[1]) : 1;
}

/** "BK1-A-07", exactly as FireCardManager shows a card's location. */
export function locationCode(c: { box_name?: string; data: CardData }): string {
  const { box_name: box } = c;
  const { slot_row: row, slot_number: slot, sport } = c.data;
  if (!box || !row || !slot) return "Unassigned";
  const base = box.split(" (cont. ")[0].toLowerCase();
  const kind = base.endsWith(" collection") ? "C" : base.endsWith(" lot box") ? "L" : "";
  return `${PREFIX[sport ?? ""] ?? "GN"}${kind}${boxNumber(box)}-${row}-${String(slot).padStart(2, "0")}`;
}

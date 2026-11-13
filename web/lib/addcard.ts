// Shared by Add card (one at a time with the camera) and Batch upload.
import { send } from "./client";

export const SPORTS = ["Baseball", "Basketball", "Football", "Hockey", "Soccer", "Boxing", "Golf", "Mma", "Racing", "Tennis", "Wrestling", "Multisport"];
export const TCG = ["Pokemon", "Magic", "YuGiOh", "OnePiece", "Lorcana", "DragonBall", "Digimon", "FleshAndBlood", "FinalFantasy", "StarWarsTcg", "WeissSchwarz",
  "Vanguard", "Naruto", "MetaZoo", "Riftbound", "WorldOfWarcraft", "MyLittlePony", "Sorcery", "Netrunner", "UniVersus", "OtherTcg", "Superheroes", "SciFi",
  "Fantasy", "Anime", "Horror", "Entertainment", "Music", "Historical", "Novelty"];

/** A photo made ready in the browser: full size for storage, a thumbnail, and a mid size for the AI. */
export type Shot = { preview: string; full: Blob; thumb: Blob; ai: Blob };

/** Loads a photo (EXIF rotation applied by the browser) and makes a JPEG no longer than `side` px. */
export async function shrink(file: Blob, side: number, quality = 0.85): Promise<Blob> {
  const url = URL.createObjectURL(file);
  try {
    const img = new Image();
    img.decoding = "async";
    img.src = url;
    await img.decode();
    const scale = Math.min(1, side / Math.max(img.naturalWidth, img.naturalHeight));
    const w = Math.round(img.naturalWidth * scale), h = Math.round(img.naturalHeight * scale);
    const c = document.createElement("canvas");
    c.width = w;
    c.height = h;
    c.getContext("2d")!.drawImage(img, 0, 0, w, h);
    return await new Promise<Blob>((ok, fail) => c.toBlob((b) => (b ? ok(b) : fail(new Error("Couldn't read the photo."))), "image/jpeg", quality));
  } finally {
    URL.revokeObjectURL(url);
  }
}

export async function makeShot(file: Blob): Promise<Shot> {
  const [full, thumb, ai] = await Promise.all([shrink(file, 1600), shrink(file, 400, 0.8), shrink(file, 1100, 0.85)]);
  return { preview: URL.createObjectURL(thumb), full, thumb, ai };
}

export async function base64(b: Blob): Promise<string> {
  const buf = new Uint8Array(await b.arrayBuffer());
  let s = "";
  for (let i = 0; i < buf.length; i += 0x8000) s += String.fromCharCode(...buf.subarray(i, i + 0x8000));
  return btoa(s);
}

export type Identified = { fields: Record<string, unknown>; notes?: string; confidence?: number };

export function identify(edition: string, front: Shot, back: Shot) {
  return (async () => send<Identified>("POST", "/v1/inventory/identify", { edition, front: await base64(front.ai), back: await base64(back.ai) }))();
}

export type Created = { id: string; box_name?: string; data: { slot_row?: string; slot_number?: number; player_name?: string } };

/**
 * Sends a saved card's photos straight to photo storage, then has the server
 * attach them. Returns a note for the seller when something didn't upload.
 */
export async function uploadPhotos(cardId: string, front: Shot, back: Shot): Promise<string> {
  const blobs: Record<string, Blob> = { front: front.full, front_thumb: front.thumb, back: back.full, back_thumb: back.thumb };
  const u = await send<{ uploads: { side: string; upload_url?: string; photo_id?: string; error?: string }[] }>(
    "POST", `/v1/inventory/cards/${cardId}/photo-urls`, { photos: Object.entries(blobs).map(([side, b]) => ({ side, bytes: b.size })) },
  );
  if (!u.ok) return "The photos didn't upload: " + u.message;
  const ids: string[] = [];
  await Promise.all(u.data.uploads.map(async (s) => {
    if (!s.upload_url || !s.photo_id) return;
    try {
      const res = await fetch(s.upload_url, { method: "PUT", body: blobs[s.side], headers: { "content-type": "image/jpeg" } });
      if (res.ok) ids.push(s.photo_id);
    } catch {}
  }));
  if (ids.length) await send("POST", "/v1/inventory/photos/confirm", { photo_ids: ids });
  return ids.length < 4 ? "Some photos didn't upload; add them again from the card page later." : "";
}

export function whereText(c: Created): string {
  const d = c.data;
  return c.box_name ? `${c.box_name}${d.slot_row ? ` · Row ${d.slot_row}` : ""}${d.slot_number ? ` · Slot ${d.slot_number}` : ""}` : "Not in a box";
}

/** Sorts file names the way a person would: IMG_2 before IMG_10. */
export function byName(a: File, b: File): number {
  return a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" });
}

// ---- The card form, as text fields the seller can fix before saving ----

export const EMPTY_FORM = {
  sport: "", player_name: "", team: "", year: "", season_end: "", brand: "", set_name: "", card_number: "", variety: "", serial_number: "",
  condition: "Raw - NM", grading_company: "", card_grade: "", estimated_value: "", sale_price: "", status: "Ready",
};
export type CardForm = typeof EMPTY_FORM;
export const NO_FLAGS = { is_rookie: false, is_autograph: false, is_memorabilia: false, is_short_print: false, is_chase: false, is_foil: false, is_sketch_card: false };
export type Flags = typeof NO_FLAGS;
export type Extra = { sale_price_percent?: unknown; last_priced_at?: unknown };

/** The AI's fields (or none) as a form. */
export function fillForm(fields: Record<string, unknown>): { form: CardForm; flags: Flags; extra: Extra } {
  const form = { ...EMPTY_FORM };
  for (const k of Object.keys(EMPTY_FORM) as (keyof CardForm)[]) {
    const v = fields[k];
    if (v !== undefined && v !== null) form[k] = String(v);
  }
  const flags = { ...NO_FLAGS };
  for (const k of Object.keys(NO_FLAGS) as (keyof Flags)[]) if (typeof fields[k] === "boolean") flags[k] = fields[k] as boolean;
  return { form, flags, extra: { sale_price_percent: fields.sale_price_percent, last_priced_at: fields.last_priced_at } };
}

/** The form as the fields a new card is saved with, or the problems to fix. */
export function toFields(form: CardForm, flags: Flags, extra: Extra, tcg: boolean): { fields: Record<string, unknown>; bad: Record<string, string> } {
  const fields: Record<string, unknown> = { ...flags };
  const bad: Record<string, string> = {};
  for (const [k, v] of Object.entries(form)) {
    if (["year", "season_end", "estimated_value", "sale_price"].includes(k)) {
      const s = v.replace(/[$,\s]/g, "");
      if (s === "") continue;
      if (Number.isNaN(Number(s))) bad[k] = "Enter a number.";
      else fields[k] = Number(s);
    } else if (v.trim() !== "") fields[k] = v.trim();
  }
  if (!tcg) for (const k of ["is_chase", "is_foil", "is_sketch_card"]) delete fields[k];
  if (form.sale_price.trim() === "" && form.status === "Ready") fields.status = "NotForSale";
  if (extra.sale_price_percent != null && form.sale_price.trim() !== "") fields.sale_price_percent = extra.sale_price_percent;
  if (extra.last_priced_at) fields.last_priced_at = extra.last_priced_at;
  return { fields, bad };
}

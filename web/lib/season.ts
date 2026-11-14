// Seasonal looks: the accent colour, a stripe under the header and a short
// greeting change around US holidays. Off unless the seller turns them on
// (Automatic, or one picked) on the Account page, remembered in this browser.

export type Season = "newyear" | "christmas" | "halloween" | "july4" | "veterans" | "thanksgiving";

export const SEASONS: { key: Season; name: string; emoji: string; greeting: string }[] = [
  { key: "newyear", name: "New Year's", emoji: "🎆", greeting: "Happy New Year! Here's to a great year of sales." },
  { key: "christmas", name: "Christmas", emoji: "🎄", greeting: "Merry Christmas from FireCardManager!" },
  { key: "halloween", name: "Halloween", emoji: "🎃", greeting: "Happy Halloween!" },
  { key: "july4", name: "Fourth of July", emoji: "🎇", greeting: "Happy Fourth of July!" },
  { key: "veterans", name: "Veterans Day", emoji: "🇺🇸", greeting: "Happy Veterans Day, and thank you to everyone who served. FireCardManager is veteran owned and operated." },
  { key: "thanksgiving", name: "Thanksgiving", emoji: "🦃", greeting: "Happy Thanksgiving! We're thankful for you." },
];

/** The look to show for a saved choice: "off" (the default, also when
 *  nothing is saved), "auto" (follows the calendar) or one season picked.
 *  Self-contained, because it also runs as a plain script before the page
 *  paints (layout.tsx). */
export function chosenSeason(pref: string | null, d: Date): string {
  if (!pref || pref === "off") return "";
  if (pref !== "auto") return pref;
  const m = d.getMonth() + 1, day = d.getDate();
  if ((m === 12 && day >= 30) || (m === 1 && day <= 2)) return "newyear";
  if (m === 12) return "christmas";
  if (m === 10 && day >= 17) return "halloween";
  if ((m === 6 && day >= 28) || (m === 7 && day <= 5)) return "july4";
  if (m === 11) {
    if (day >= 8 && day <= 12) return "veterans";
    // Thanksgiving: the fourth Thursday of November, from the Saturday before to the Sunday after.
    const first = new Date(d.getFullYear(), 10, 1).getDay();
    const thu = 1 + ((4 - first + 7) % 7) + 21;
    if (day >= thu - 5 && day <= thu + 3) return "thanksgiving";
  }
  return "";
}

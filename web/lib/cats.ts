// Category names as FireCardManager shows them; safe on server and client.
const CAT_NAMES: Record<string, string> = {
  Mma: "MMA", Multisport: "Multi-sport", Magic: "Magic: The Gathering", YuGiOh: "Yu-Gi-Oh!", OnePiece: "One Piece", DragonBall: "Dragon Ball",
  FleshAndBlood: "Flesh and Blood", FinalFantasy: "Final Fantasy", StarWarsTcg: "Star Wars TCG", WeissSchwarz: "Weiss Schwarz",
  WorldOfWarcraft: "World of Warcraft", MyLittlePony: "My Little Pony", UniVersus: "UniVersus", MetaZoo: "MetaZoo", OtherTcg: "Other TCG", SciFi: "Sci-Fi",
};

/** A sport or TCG category as FireCardManager shows it (YuGiOh -> Yu-Gi-Oh!). */
export function catName(s?: string | null): string {
  if (!s) return "";
  return CAT_NAMES[s] ?? s.replace(/([a-z])([A-Z])/g, "$1 $2");
}

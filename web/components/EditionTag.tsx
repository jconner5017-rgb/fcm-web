import Link from "next/link";

/** Which edition the site is showing; it's switched on the Home page. */
export default function EditionTag({ edition }: { edition: string }) {
  return (
    <Link href="/" title="Switch Sports | TCG on the Home page"
      className="rounded-full border-2 border-go px-3 py-1 text-[13px] font-semibold text-go hover:bg-go/10">
      {edition === "tcg" ? "TCG" : "Sports"}
    </Link>
  );
}

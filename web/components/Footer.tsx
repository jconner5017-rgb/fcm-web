export default function Footer() {
  return (
    <footer className="border-t border-line">
      <div className="mx-auto flex max-w-[1100px] flex-wrap items-center justify-between gap-4 px-4 py-6 text-[13px] text-muted">
        <div>
          <p>© {new Date().getFullYear()} FireCardManager.com</p>
          <p className="mt-1">
            <a href="https://firecardmanager.com" className="hover:text-fire">firecardmanager.com</a>
            {" · "}
            <a href="https://firecardmanager.com/privacy-policy/" className="hover:text-fire">Privacy</a>
          </p>
        </div>
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src="/veteran-owned-light.png" alt="Veteran Owned and Operated Business" height={44} className="vet-light h-11 w-auto" />
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src="/veteran-owned-dark.png" alt="Veteran Owned and Operated Business" height={44} className="vet-dark h-11 w-auto" />
      </div>
    </footer>
  );
}

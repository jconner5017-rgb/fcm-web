import Link from "next/link";
import { getMe } from "@/lib/api";
import ThemeToggle from "./ThemeToggle";
import SignOutButton from "./SignOutButton";

export default async function Header() {
  const { me } = await getMe();
  return (
    <header className="border-b border-line">
      <div className="mx-auto flex max-w-[1100px] flex-wrap items-center gap-x-5 gap-y-2 px-4 py-3">
        <Link href="/" className="flex items-center gap-2">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/icon.svg" alt="" width={34} height={34} />
          <span className="leading-tight">
            <span className="block text-[17px] font-bold">FireCardManager</span>
            <span className="block text-[12px] text-muted">Web</span>
          </span>
        </Link>
        <nav className="ml-auto flex flex-wrap items-center gap-x-4 gap-y-1 text-[14px] font-semibold">
          {me ? (
            <>
              <Link href="/" className="hover:text-fire">Home</Link>
              <Link href="/inventory" className="hover:text-fire">Inventory</Link>
              <Link href="/editor" className="hidden hover:text-fire lg:inline">Card Editor</Link>
              <Link href="/boxes" className="hover:text-fire">Boxes</Link>
              <Link href="/lister" className="hover:text-fire">Bulk Lister</Link>
              <Link href="/live" className="hover:text-fire">Live</Link>
              <Link href="/sold" className="hover:text-fire">Sold</Link>
              <Link href="/export" className="hidden hover:text-fire lg:inline">Import/Export</Link>
              <Link href="/devices" className="hover:text-fire">Linked PCs</Link>
              <Link href="/account" className="hover:text-fire">Account</Link>
              {me.account.is_admin && <Link href="/admin" className="hover:text-fire">Admin</Link>}
              <SignOutButton />
            </>
          ) : (
            <>
              <Link href="/signin" className="hover:text-fire">Sign in</Link>
              <Link href="/signup" className="btn !py-1.5">Create account</Link>
            </>
          )}
          <ThemeToggle />
        </nav>
      </div>
    </header>
  );
}

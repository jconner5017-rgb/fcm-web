import type { Metadata } from "next";
import { Archivo } from "next/font/google";
import "./globals.css";
import Header from "@/components/Header";
import Footer from "@/components/Footer";

const archivo = Archivo({ subsets: ["latin"], variable: "--font-archivo", display: "swap" });

export const metadata: Metadata = {
  title: { default: "FireCardManager Web", template: "%s | FireCardManager Web" },
  description: "Your FireCardManager inventory, from any device.",
  robots: { index: false, follow: false },
};

// Applies the saved light/dark choice before the page paints. Dark unless
// the seller picked light, like the desktop app.
const THEME_SCRIPT = `(function(){try{var t=localStorage.getItem("fcm-theme");document.documentElement.dataset.theme=t==="light"?"light":"dark";}catch(e){document.documentElement.dataset.theme="dark";}})();`;

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={archivo.variable} suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_SCRIPT }} />
      </head>
      <body className="flex min-h-screen flex-col font-sans text-[15px] leading-normal">
        <Header />
        <main id="main" className="mx-auto w-full max-w-[1100px] flex-1 px-4 pb-16">
          {children}
        </main>
        <Footer />
      </body>
    </html>
  );
}

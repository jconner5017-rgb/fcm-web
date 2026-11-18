import type { MetadataRoute } from "next";

// Lets "Install app" / "Add to Home Screen" use the FireCardManager icon.
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "FireCardManager Web",
    short_name: "FireCardManager",
    description: "Your FireCardManager inventory, from any device.",
    start_url: "/",
    display: "standalone",
    background_color: "#111317",
    theme_color: "#111317",
    icons: [
      { src: "/icon-192.png", sizes: "192x192", type: "image/png" },
      { src: "/icon-512.png", sizes: "512x512", type: "image/png" },
      { src: "/icon-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
    ],
  };
}

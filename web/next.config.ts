import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  eslint: { ignoreDuringBuilds: true },
  // The brand images can be loaded by firecardmanager.com (the WordPress site).
  async headers() {
    return [{ source: "/brand/:file*", headers: [{ key: "Access-Control-Allow-Origin", value: "https://firecardmanager.com" }] }];
  },
};

export default nextConfig;

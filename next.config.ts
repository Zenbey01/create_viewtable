import type { NextConfig } from "next";

// NEXT_DIST_DIR lets the e2e test run its own server beside a dev server.
const nextConfig: NextConfig = { distDir: process.env.NEXT_DIST_DIR || ".next" };

export default nextConfig;

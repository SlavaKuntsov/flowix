/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  async rewrites() {
    // In dev, proxy /api and /hls to gateway to avoid CORS
    const gateway = process.env.GATEWAY_URL || "http://localhost:8080";
    return [
      { source: "/api/:path*", destination: `${gateway}/api/:path*` },
      { source: "/hls/:path*", destination: `${gateway}/hls/:path*` },
    ];
  },
};
export default nextConfig;

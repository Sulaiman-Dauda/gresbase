/** @type {import('next').NextConfig} */
const nextConfig = {
  // Static export for embedding in the Go binary
  output: 'export',

  // Base path — uncomment if serving from a sub-path
  // basePath: '/_',

  // Disable image optimization (not supported with static export)
  images: {
    unoptimized: true,
  },

  // Trailing slash for consistency
  trailingSlash: false,

  transpilePackages: [],
}

module.exports = nextConfig

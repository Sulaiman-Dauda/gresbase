/** @type {import('next').NextConfig} */
const isDev = process.env.NODE_ENV === 'development'

const nextConfig = {
  // Static export for embedding in the Go binary (skipped in dev — next dev serves dynamically)
  output: isDev ? undefined : 'export',

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

// In dev, proxy /api and /uploads to the Go backend so credentials/cookies work
// correctly. In production the Go binary serves the static export directly, so
// no proxy is needed — and `rewrites` is omitted entirely to keep the static
// export build free of "rewrites not applied when exporting" warnings.
if (isDev) {
  nextConfig.rewrites = async () => {
    const backendURL = process.env.NEXT_PUBLIC_BACKEND_URL || 'http://localhost:8080'
    return [
      { source: '/api/:path*', destination: `${backendURL}/api/:path*` },
      { source: '/uploads/:path*', destination: `${backendURL}/uploads/:path*` },
    ]
  }
}

module.exports = nextConfig

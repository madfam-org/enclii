/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  // Workspace packages need transpilePackages so Next/Turbopack resolves
  // their nested workspace deps (e.g. ui-components → shared-lib) instead
  // of giving up at the package boundary with "Module not found".
  transpilePackages: ['@janua/ui', '@enclii/ui-components', '@enclii/shared-lib'],
  env: {
    ENCLII_API_URL: process.env.ENCLII_API_URL || "http://localhost:4200",
    // Theme skin default (enterprise or solarpunk)
    NEXT_PUBLIC_THEME_DEFAULT: process.env.NEXT_PUBLIC_THEME_DEFAULT || "enterprise",
  },
  images: {
    // Enable external images for avatars (GitHub, Gravatar)
    remotePatterns: [
      {
        protocol: 'https',
        hostname: 'github.com',
        pathname: '/**',
      },
      {
        protocol: 'https',
        hostname: 'avatars.githubusercontent.com',
        pathname: '/**',
      },
      {
        protocol: 'https',
        hostname: 'www.gravatar.com',
        pathname: '/avatar/**',
      },
      {
        protocol: 'https',
        hostname: 'gravatar.com',
        pathname: '/avatar/**',
      },
    ],
  },
};

module.exports = nextConfig;

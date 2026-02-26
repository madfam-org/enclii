/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  transpilePackages: ['@janua/ui'],
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

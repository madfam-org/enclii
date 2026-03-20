import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Enclii Documentation',
  tagline: 'Deploy, scale, and operate — on infrastructure you own',
  favicon: 'img/favicon.ico',

  url: 'https://docs.enclii.dev',
  baseUrl: '/',

  organizationName: 'madfam-io',
  projectName: 'enclii',

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'throw',
  onBrokenAnchors: 'warn',

  // Use standard markdown to avoid MDX parsing issues with <placeholders>
  markdown: {
    format: 'md',
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          // In Docker build, docs are at ../docs; locally at ../../docs
          path: process.env.DOCKER_BUILD === 'true' ? '../docs' : '../../docs',
          routeBasePath: '/',
          editUrl: 'https://github.com/madfam-io/enclii/tree/main/docs/',
          // Exclude archived and draft content
          exclude: [
            '**/archive/**',
            '**/drafts/**',
          ],
          // Show last updated time for docs
          showLastUpdateTime: true,
        },
        pages: false,
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  plugins: [],

  themeConfig: {
    image: 'img/enclii-social-card.png',
    navbar: {
      title: 'Enclii',
      logo: {
        alt: 'Enclii Logo',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Documentation',
        },
        {
          type: 'doc',
          docId: 'api-reference/index',
          label: 'API Reference',
          position: 'left',
        },
        {
          href: 'https://app.enclii.dev',
          label: 'Dashboard',
          position: 'right',
        },
        {
          href: 'https://github.com/madfam-io/enclii',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Documentation',
          items: [
            {
              label: 'Getting Started',
              to: '/getting-started/QUICKSTART',
            },
            {
              label: 'API Reference',
              to: '/api-reference/',
            },

            {
              label: 'CLI Commands',
              to: '/cli/',
            },
            {
              label: 'Guides',
              to: '/guides/ONBOARDING_GUIDE',
            },
          ],
        },
        {
          title: 'Help',
          items: [
            {
              label: 'Troubleshooting',
              to: '/troubleshooting/',
            },
            {
              label: 'FAQ',
              to: '/faq/',
            },
            {
              label: 'GitHub Issues',
              href: 'https://github.com/madfam-io/enclii/issues',
            },
          ],
        },
        {
          title: 'Community',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/madfam-io/enclii',
            },
            {
              label: 'Discord',
              href: 'https://discord.gg/madfam',
            },
            {
              label: 'Status',
              href: 'https://status.enclii.dev',
            },
          ],
        },
        {
          title: 'More',
          items: [
            {
              label: 'Enclii Website',
              href: 'https://enclii.dev',
            },
            {
              label: 'MADFAM',
              href: 'https://madfam.io',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} MADFAM. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'go', 'typescript', 'json'],
    },
    colorMode: {
      defaultMode: 'dark',
      disableSwitch: false,
      respectPrefersColorScheme: true,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;

import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  docsSidebar: [
    {
      type: 'doc',
      id: 'README',
      label: 'Overview',
    },
    {
      type: 'doc',
      id: 'quickstart',
      label: '5-minute Quickstart',
    },
    {
      type: 'doc',
      id: 'templates',
      label: 'Template Catalog',
    },

    // Migrating
    {
      type: 'category',
      label: 'Migrating to Enclii',
      collapsed: false,
      items: [
        'guides/migrating',
        'guides/migrating-from-vercel',
        'guides/migrating-from-railway',
        'guides/migrating-from-heroku',
      ],
    },

    // Getting Started (platform contributors)
    {
      type: 'category',
      label: 'Platform Contributor Setup',
      collapsed: true,
      items: [
        'getting-started/QUICKSTART',
        'getting-started/DEVELOPMENT',
        'getting-started/BUILD_SETUP',
      ],
    },

    // Guides
    {
      type: 'category',
      label: 'Guides',
      items: [
        'guides/ONBOARDING_GUIDE',
        'guides/SELF_HOSTING',
        'guides/RAILWAY_MIGRATION_GUIDE',
        'guides/VERCEL_MIGRATION_GUIDE',
        'guides/HEROKU_MIGRATION_GUIDE',
        'guides/TESTING_GUIDE',
        'guides/cli-auth-setup',
        'guides/sso-deployment',
        'guides/database-operations',
      ],
    },

    // CLI Reference
    {
      type: 'category',
      label: 'CLI Reference',
      items: [
        'cli/README',
        {
          type: 'category',
          label: 'Commands',
          items: [
            'cli/commands/deploy',
            'cli/commands/init',
            'cli/commands/local',
            'cli/commands/login',
            'cli/commands/logout',
            'cli/commands/logs',
            'cli/commands/ps',
            'cli/commands/rollback',
            'cli/commands/services-sync',
            'cli/commands/version',
            'cli/commands/whoami',
          ],
        },
      ],
    },

    // SDKs
    {
      type: 'category',
      label: 'SDKs',
      items: [
        {
          type: 'category',
          label: 'TypeScript SDK',
          items: [
            'sdk/typescript/index',
            'sdk/typescript/authentication',
            'sdk/typescript/projects',
            'sdk/typescript/services',
            'sdk/typescript/deployments',
            'sdk/typescript/domains',
          ],
        },
      ],
    },

    // Troubleshooting
    {
      type: 'category',
      label: 'Troubleshooting',
      items: [
        'troubleshooting/index',
        'troubleshooting/api-errors',
        'troubleshooting/build-failures',
        'troubleshooting/deployment-issues',
        'troubleshooting/auth-problems',
        'troubleshooting/networking',
      ],
    },

    // FAQ
    {
      type: 'category',
      label: 'FAQ',
      items: [
        'faq/index',
        'faq/general',
        'faq/billing',
        'faq/security',
        'faq/migration',
      ],
    },

    // Infrastructure
    {
      type: 'category',
      label: 'Infrastructure',
      items: [
        'infrastructure/README',
        'infrastructure/CLOUDFLARE',
        'infrastructure/GITOPS',
        'infrastructure/STORAGE',
        'infrastructure/EXTERNAL_SECRETS',
        'infrastructure/INFRA_ANATOMY',
        'infrastructure/dns-setup-porkbun',
        'infrastructure/npm-registry',
      ],
    },

    // Integrations
    {
      type: 'category',
      label: 'Integrations',
      items: [
        'integrations/github',
        'integrations/sso',
        'integrations/compliance-webhooks',
      ],
    },

    // Architecture
    {
      type: 'category',
      label: 'Architecture',
      items: [
        'architecture/ARCHITECTURE',
        'architecture/API',
        'architecture/BLUE_OCEAN_ROADMAP',
        'architecture/SOFTWARE_SPEC',
        'architecture/ENCLII_CAPABILITY_MATRIX',
        'architecture/ENCLII_EXECUTIVE_SUMMARY',
        'architecture/ENCLII_QUICK_REFERENCE',
      ],
    },

    // Production
    {
      type: 'category',
      label: 'Production',
      items: [
        'production/PRODUCTION_DEPLOYMENT_ROADMAP',
        'production/PRODUCTION_CHECKLIST',
        'production/GAP_ANALYSIS',
        'production/BUILD_PIPELINE',

      ],
    },

    // Implementation
    {
      type: 'category',
      label: 'Implementation',
      collapsed: true,
      items: [
        'implementation/MVP_IMPLEMENTATION',
        'implementation/BUILD_PIPELINE_IMPLEMENTATION',
        'implementation/CLI_IMPLEMENTATION_COMPLETE',
        'implementation/BLUE_OCEAN_IMPLEMENTATION_STATUS',
      ],
    },

    // Reference
    {
      type: 'category',
      label: 'Reference',
      collapsed: true,
      items: [
        'api-reference/index',
        'reference/service-spec',
      ],
    },

    // Runbooks
    {
      type: 'category',
      label: 'Runbooks',
      collapsed: true,
      items: [
        'runbooks/DATABASE_RECOVERY',
      ],
    },

    // Functions (Serverless)
    {
      type: 'category',
      label: 'Functions',
      collapsed: true,
      items: [
        'functions/quickstart',
        'functions/configuration',
        'functions/runtimes',
      ],
    },

    // Design
    {
      type: 'category',
      label: 'Design',
      collapsed: true,
      items: [
        'design/CLOUDFLARE_TUNNEL_UI',
        'design/MONOREPO_PROJECT_MODEL',
      ],
    },

    // Compliance
    {
      type: 'category',
      label: 'Compliance',
      collapsed: true,
      items: [
        'compliance/SOC2_CONTROLS_MAPPING',
        'compliance/CHANGE_MANAGEMENT',
        'compliance/VULNERABILITY_MANAGEMENT',
        'compliance/DATA_CLASSIFICATION',
        'compliance/VENDOR_RISK_ASSESSMENT',
      ],
    },

    // Security
    {
      type: 'category',
      label: 'Security',
      collapsed: true,
      items: [
        'security/SECRET_ROTATION_LOG',
        'infrastructure/KYVERNO_POLICIES',
      ],
    },

    // Operations
    {
      type: 'category',
      label: 'Operations',
      collapsed: true,
      items: [
        'operations/INCIDENT_RESPONSE',
        'production/POST_INCIDENT_REVIEWS',
        'production/ANTI_FRAGILITY_SYSTEM',
      ],
    },
  ],
};

export default sidebars;

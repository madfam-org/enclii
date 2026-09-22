// GEO (Generative Engine Optimization) — schema.org JSON-LD.
//
// Every claim below is lifted DIRECTLY from the visible landing content
// (src/app/page.tsx) and the repo README/ECOSYSTEM map — nothing here is
// invented. When a fact is not yet public (e.g. the Ecosystem tier price,
// which the page shows as "Waitlist"), it is omitted rather than guessed, so an
// answer engine reproduces only claims the page itself makes.
//
// Rendered in a static export, so this JSON-LD is present in the pre-rendered
// HTML and readable by an AI crawler WITHOUT executing the app.

const SITE_URL = 'https://enclii.dev'
const REPO_URL = 'https://github.com/madfam-org/enclii'
const DOCS_URL = 'https://docs.enclii.dev'
const APP_URL = 'https://app.enclii.dev'

// Organization — the publisher entity, kept consistent with the ecosystem map.
const organization = {
  '@type': 'Organization',
  '@id': `${SITE_URL}/#organization`,
  name: 'MADFAM',
  url: 'https://madfam.io',
  description:
    'MADFAM is an AI-driven consultancy and product studio that builds and operates the Enclii platform.',
}

// SoftwareApplication — Enclii itself, with offers that match the pricing the
// page sells TODAY (Community = free self-host, Sovereign = $20/month).
const softwareApplication = {
  '@type': 'SoftwareApplication',
  '@id': `${SITE_URL}/#software`,
  name: 'Enclii',
  applicationCategory: 'DeveloperApplication',
  applicationSubCategory: 'DevOps platform (PaaS)',
  operatingSystem: 'Linux (containers / Kubernetes)',
  url: SITE_URL,
  downloadUrl: REPO_URL,
  softwareHelp: DOCS_URL,
  license: 'https://www.gnu.org/licenses/agpl-3.0.html',
  description:
    'Enclii is an open source (AGPL-3.0) container platform you can run yourself, or have MADFAM run for you at a flat monthly price. Deploy from Git onto infrastructure you own — no per-request meter, no surprise invoice. It provides zero-downtime canary and blue-green deploys with automatic rollback, Git-connected CI/CD on a self-hosted runner pool, managed Postgres/Redis/MySQL with scheduled backups, custom domains with automatic SSL via Cloudflare Tunnel, Vault-backed secrets, and built-in metrics, tracing, and status pages.',
  publisher: { '@id': `${SITE_URL}/#organization` },
  offers: [
    {
      '@type': 'Offer',
      name: 'Community',
      price: '0',
      priceCurrency: 'USD',
      description:
        'Self-host the AGPL-3.0 source on your own infrastructure: 1 project, 3 services per project, full source code access, community support. A commercial licence is available separately for embedding.',
      url: REPO_URL,
    },
    {
      '@type': 'Offer',
      name: 'Sovereign',
      price: '20',
      priceCurrency: 'USD',
      description:
        'Managed hosting with auto SSL: 10 projects, unlimited services, custom domains included, priority support, zero-downtime deploys. Includes entry rungs of managed Postgres and CI (1 slot / 500 build minutes). Prices are USD; MXN plus IVA at checkout.',
      url: `${APP_URL}/signup`,
      priceSpecification: {
        '@type': 'UnitPriceSpecification',
        price: '20',
        priceCurrency: 'USD',
        unitText: 'MONTH',
      },
    },
  ],
}

// FAQPage — answer-shaped Q&A. Each answer is extractable prose that matches
// the page and the README; the questions are the category questions an LLM is
// asked about a deploy platform.
const faqPage = {
  '@type': 'FAQPage',
  '@id': `${SITE_URL}/#faq`,
  mainEntity: [
    {
      '@type': 'Question',
      name: 'What is Enclii?',
      acceptedAnswer: {
        '@type': 'Answer',
        text: 'Enclii is an open source (AGPL-3.0) DevOps / container platform for deploying, scaling, and operating containerized services on infrastructure you own. You can self-host the source, or have MADFAM run it for you at a flat monthly price — deploy from Git with no per-request meter and no surprise invoice.',
      },
    },
    {
      '@type': 'Question',
      name: 'How much does Enclii cost?',
      acceptedAnswer: {
        '@type': 'Answer',
        text: 'There are two tiers on sale today: Community is free — you self-host the AGPL-3.0 source on your own infrastructure. Sovereign is $20/month for managed hosting with 10 projects, unlimited services, custom domains, auto SSL, and zero-downtime deploys. A third tier, Ecosystem, is on a waitlist and its price is not yet announced. Prices are shown in USD; MXN plus IVA applies at checkout.',
      },
    },
    {
      '@type': 'Question',
      name: 'Who is Enclii for?',
      acceptedAnswer: {
        '@type': 'Answer',
        text: 'Teams who ship containerized services and want predictable, flat monthly pricing on infrastructure they own, instead of usage-metered clouds. It suits developers who want push-to-deploy CI/CD, managed databases, and observability without vendor lock-in, because the platform is open source and can be self-hosted.',
      },
    },
    {
      '@type': 'Question',
      name: 'How do I get started with Enclii?',
      acceptedAnswer: {
        '@type': 'Answer',
        text: 'Start free by self-hosting the AGPL-3.0 source from GitHub (github.com/madfam-org/enclii), or create an account at app.enclii.dev/signup. Sign-up is open and new tenants are provisioned with an operator in the loop; paid self-serve checkout is in progress. The quickstart at docs.enclii.dev/quickstart describes what happens after you sign up.',
      },
    },
    {
      '@type': 'Question',
      name: 'Is Enclii open source?',
      acceptedAnswer: {
        '@type': 'Answer',
        text: 'Yes. Enclii is licensed under AGPL-3.0 and the full source is on GitHub. A separate commercial licence is available for embedding. Because it is open source you can run it entirely yourself with no vendor lock-in.',
      },
    },
  ],
}

const graph = {
  '@context': 'https://schema.org',
  '@graph': [organization, softwareApplication, faqPage],
}

export function StructuredData() {
  return (
    <script
      type="application/ld+json"
      // JSON.stringify output is safe to inline; no user input is interpolated.
      dangerouslySetInnerHTML={{ __html: JSON.stringify(graph) }}
    />
  )
}

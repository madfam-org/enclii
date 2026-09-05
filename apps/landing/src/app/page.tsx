import {
  ArrowRight,
  BarChart3,
  Boxes,
  Container,
  Cpu,
  Crown,
  Database,
  ExternalLink,
  FileText,
  GitBranch,
  Globe,
  HardDrive,
  Network,
  RefreshCw,
  ServerCog,
  Sparkles,
  Wallet,
  Wrench,
  Zap,
} from 'lucide-react'

import {
  BenefitRow,
  CapabilityCard,
  EngagementCard,
  FeatureCard,
  IncludedCard,
  PlannedLineCard,
  PricingCard,
} from '@/components/cards'
import { SiteFooter, SiteNav } from '@/components/site-chrome'

// Proposed names pending owner ruling R16. Nothing below hard-codes a product
// name: change it here once and the whole page follows.
const NAMES = {
  runners: 'Fragua',
  data: 'Enclii Depot',
  content: 'Publica',
} as const

const CONTACT = 'hello@enclii.dev'

function waitlistHref(subject: string) {
  return `mailto:${CONTACT}?subject=${encodeURIComponent(subject)}`
}

export default function Home() {
  return (
    <main className="min-h-screen">
      <SiteNav />

      {/* Hero Section */}
      <section className="hero-gradient pt-32 pb-20 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto text-center">
          <div className="inline-flex items-center gap-2 bg-solarpunk-green/10 backdrop-blur-sm px-4 py-2 rounded-full text-solarpunk-green text-sm mb-8 pulse-glow">
            <span className="inline-block w-2 h-2 bg-solarpunk-green rounded-full animate-pulse"></span>
            Open source, AGPL-3.0
          </div>
          <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold text-white mb-6 leading-tight">
            Deploy Without<br />the Bill Shock
          </h1>
          <p className="text-xl text-white/80 mb-10 max-w-2xl mx-auto">
            An open source container platform you can run yourself, or let us run for you at a flat
            monthly price. Deploy from Git onto infrastructure you own — no per-request meter, no
            surprise invoice.
          </p>
          <div className="flex flex-col sm:flex-row gap-4 justify-center">
            <a
              href="https://app.enclii.dev/signup"
              className="inline-flex items-center justify-center gap-2 bg-solarpunk-green text-solarpunk-slate px-8 py-4 rounded-xl font-semibold text-lg hover:bg-solarpunk-green-dim transition-colors shadow-lg"
            >
              Create your account
              <ArrowRight className="w-5 h-5" />
            </a>
            <a
              href="https://docs.enclii.dev"
              className="inline-flex items-center justify-center gap-2 bg-white/10 text-white border border-white/20 px-8 py-4 rounded-xl font-semibold text-lg hover:bg-white/20 transition-colors"
            >
              View Documentation
              <ExternalLink className="w-5 h-5" />
            </a>
          </div>
          <p className="text-sm text-white/60 mt-6 max-w-2xl mx-auto">
            Sign-up is open, and new tenants are provisioned with an operator in the loop. Paid
            self-serve checkout is in progress — the{' '}
            <a href="https://docs.enclii.dev/quickstart" className="underline hover:text-white">
              quickstart
            </a>{' '}
            describes what happens after you sign up.
          </p>
        </div>
      </section>

      {/* Features Section */}
      <section className="py-24 px-4 sm:px-6 lg:px-8 bg-gray-50 dark:bg-gray-900">
        <div className="max-w-7xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl sm:text-4xl font-bold text-gray-900 dark:text-white mb-4">
              Everything You Need to Ship Fast
            </h2>
            <p className="text-lg text-gray-600 dark:text-gray-400 max-w-2xl mx-auto">
              Deployment infrastructure at a price you can predict a year ahead.
            </p>
          </div>
          <div className="grid md:grid-cols-2 lg:grid-cols-4 gap-8">
            <FeatureCard
              icon={<Wallet className="w-8 h-8" />}
              title="Flat Monthly Price"
              description="One subscription per month, not a usage meter. Usage tracking and budget alerts are built in so you can see where resources go."
            />
            <FeatureCard
              icon={<RefreshCw className="w-8 h-8" />}
              title="Zero-Downtime Deploys"
              description="Canary and blue-green deployment strategies with automatic rollback on failure, and one-command rollback afterwards."
            />
            <FeatureCard
              icon={<BarChart3 className="w-8 h-8" />}
              title="Metrics and Traces"
              description="Metrics, dashboards, tracing, and status pages included. Log streaming is in beta."
            />
            <FeatureCard
              icon={<GitBranch className="w-8 h-8" />}
              title="Git-Connected CI/CD"
              description="Push to deploy, on a self-hosted runner pool rather than a rented one. Production from main."
            />
          </div>
        </div>
      </section>

      {/* What you can buy today */}
      <section id="buy-today" className="py-24 px-4 sm:px-6 lg:px-8">
        <div className="max-w-6xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl sm:text-4xl font-bold text-gray-900 dark:text-white mb-4">
              What you can buy today
            </h2>
            <p className="text-lg text-gray-600 dark:text-gray-400">
              One deploy platform, three ways to run it. Everything on this page is either sold
              today or marked as planned.
            </p>
          </div>

          <div className="grid md:grid-cols-3 gap-8">
            <PricingCard
              icon={<Sparkles className="w-6 h-6" />}
              name="Community"
              price="Free"
              description="Self-host the AGPL-3.0 source on your own infrastructure"
              features={[
                '1 project',
                '3 services per project',
                'Full source code access',
                'Community support',
                'Self-hosted infrastructure',
              ]}
              footnote="You run it, you own it. A commercial licence is available separately for embedding."
              cta={{ label: 'View on GitHub', href: 'https://github.com/madfam-org/enclii', external: true }}
            />

            <PricingCard
              icon={<Zap className="w-6 h-6" />}
              name="Sovereign"
              price="$20"
              priceNote="/month"
              description="Managed hosting with auto SSL"
              features={[
                '10 projects',
                'Unlimited services',
                'Custom domains included',
                'Auto SSL certificates',
                'Priority support',
                'Zero-downtime deploys',
              ]}
              cta={{ label: 'Start Building', href: 'https://app.enclii.dev/signup' }}
              highlighted
            />

            <PricingCard
              icon={<Crown className="w-6 h-6" />}
              name="Ecosystem"
              price="Waitlist"
              description="Full bundle with team management. Pricing to be announced."
              features={[
                'Unlimited projects',
                'Unlimited services',
                'Team management',
                'SSO integration (Janua)',
                'Billing integration (Dhanam)',
                'Support and availability terms published with the tier',
              ]}
              cta={{ label: 'Join the waitlist', href: waitlistHref('Enclii Ecosystem waitlist') }}
            />
          </div>

          <p className="mt-10 text-center text-sm text-gray-500 dark:text-gray-400">
            All prices on this page are shown in USD; MXN pricing plus IVA at checkout.
          </p>

          {/* Included with Sovereign */}
          <div className="mt-16">
            <h3 className="text-xl font-bold text-gray-900 dark:text-white mb-2 text-center">
              Included with Sovereign
            </h3>
            <p className="text-sm text-gray-600 dark:text-gray-400 mb-8 text-center">
              The entry rungs of the other product lines come with the subscription — no separate
              purchase.
            </p>
            <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-6">
              <IncludedCard
                icon={<Database className="w-5 h-5" />}
                title="Managed Postgres"
                description={`A small managed Postgres database with daily backups — the free rung of the ${NAMES.data} line. Redis and MySQL add-ons run on the same platform.`}
              />
              <IncludedCard
                icon={<Cpu className="w-5 h-5" />}
                title="1 CI slot, 500 min"
                description={`One concurrent build slot and 500 build minutes a month on the shared runner pool — the entry rung of the ${NAMES.runners} line.`}
              />
              <IncludedCard
                icon={<Globe className="w-5 h-5" />}
                title="Custom domains"
                description="Bring your own domains, with automatic certificates and zero-trust ingress routing, at no extra cost."
              />
              <IncludedCard
                icon={<GitBranch className="w-5 h-5" />}
                title="Preview environments"
                description="A disposable environment per pull request. In beta and not yet generally available — ask us to turn it on."
                badge="Beta"
              />
            </div>
          </div>
        </div>
      </section>

      {/* Inside engagements today */}
      <section className="py-24 px-4 sm:px-6 lg:px-8 bg-gray-50 dark:bg-gray-900">
        <div className="max-w-6xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl sm:text-4xl font-bold text-gray-900 dark:text-white mb-4">
              Inside engagements today
            </h2>
            <p className="text-lg text-gray-600 dark:text-gray-400 max-w-3xl mx-auto">
              MADFAM already runs client platforms on Enclii as part of a retainer. Hosting is not
              billed as a separate SKU there — it is carried by the engagement, together with the
              people who operate it.
            </p>
          </div>
          <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-6">
            <EngagementCard
              icon={<ServerCog className="w-5 h-5" />}
              title="vCTO retainer with hosting"
              description="A fractional CTO engagement where the client's services run on Enclii and MADFAM operates the platform for them."
            />
            <EngagementCard
              icon={<Boxes className="w-5 h-5" />}
              title="ERP rungs, per seat"
              description="Business systems delivered per seat, hosted on the same platform, with the operations included rather than resold."
            />
            <EngagementCard
              icon={<Cpu className="w-5 h-5" />}
              title="Dedicated CI runner"
              description="Build capacity reserved for one client instead of the shared pool, provisioned inside the retainer."
            />
            <EngagementCard
              icon={<Wrench className="w-5 h-5" />}
              title="Migration and onboarding"
              description="Wix to Enclii site migration, and an operator-run onboarding kit that takes a repository from zero to deploying."
            />
          </div>
          <div className="mt-12 text-center">
            <a
              href="https://madfam.io"
              className="inline-flex items-center justify-center gap-2 bg-solarpunk-deep text-white px-8 py-4 rounded-xl font-semibold text-lg hover:bg-solarpunk-slate transition-colors"
            >
              Talk to us
              <ArrowRight className="w-5 h-5" />
            </a>
            <p className="mt-4 text-sm text-gray-500 dark:text-gray-400">
              Engagements are scoped and priced case by case — no list price on this page.
            </p>
          </div>
        </div>
      </section>

      {/* Coming soon: the other product lines */}
      <section className="py-24 px-4 sm:px-6 lg:px-8">
        <div className="max-w-6xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl sm:text-4xl font-bold text-gray-900 dark:text-white mb-4">
              Coming soon
            </h2>
            <p className="text-lg text-gray-600 dark:text-gray-400 max-w-3xl mx-auto">
              Three product lines are being built out of what already runs the platform. The
              ladders and prices below are planned, not on sale: there is no checkout for them
              yet, and the names are still provisional.
            </p>
          </div>

          <div className="grid lg:grid-cols-3 gap-8">
            <PlannedLineCard
              icon={<Cpu className="w-6 h-6" />}
              line="Runners"
              name={NAMES.runners}
              summary="Managed CI runners on infrastructure you can leave. Planned tiers:"
              tiers={[
                { name: 'Arranque', detail: '2 concurrent / 10,000 min', price: '$49/mo (planned)' },
                { name: 'Equipo', detail: '5 concurrent / 40,000 min', price: '$149/mo (planned)' },
                { name: 'Escala', detail: '12 concurrent / 120,000 min', price: '$399/mo (planned)' },
                { name: 'Dedicada', detail: 'Your own scale set and machine', price: '$449/mo (planned)' },
                {
                  name: `${NAMES.runners} Builds`,
                  detail: 'Remote build cache, multi-arch, SBOM',
                  price: '+$99/mo (planned)',
                },
              ]}
              caveat="Pooled tiers are isolated by namespace, quota, rootless execution, and an egress allowlist; the kernel is shared, and we say so. Only the dedicated tier is a machine of its own."
              cta={{ label: 'Join the waitlist', href: waitlistHref(`${NAMES.runners} runners waitlist`) }}
            />

            <PlannedLineCard
              icon={<Database className="w-6 h-6" />}
              line="Data"
              name={NAMES.data}
              summary="Managed Postgres with backups, connection pooling, and a data API. Planned tiers:"
              tiers={[
                {
                  name: 'Community',
                  detail: '1 GB / 10 connections, daily backups',
                  price: '$0, included with Sovereign',
                },
                { name: 'Standard', detail: 'Own cluster, 10 GB / 40 connections', price: '$29/mo (planned)' },
                { name: 'HA', detail: 'Three instances, 50 GB / 100 connections', price: '$99/mo (planned)' },
                { name: 'Dedicated', detail: '500 GB and up, by contract', price: 'from $349/mo (planned)' },
              ]}
              caveat="Backup, retention, and recovery terms are published with each tier when it ships. Redis and MySQL add-ons run on the same platform today."
              cta={{ label: 'Join the waitlist', href: waitlistHref(`${NAMES.data} waitlist`) }}
            />

            <PlannedLineCard
              icon={<FileText className="w-6 h-6" />}
              line="Content"
              name={NAMES.content}
              summary="A multi-tenant CMS for the sites Enclii already hosts. Planned tiers:"
              tiers={[
                {
                  name: 'Sitio',
                  detail: 'One site, own domains, es/en/pt, drafts, media, export',
                  price: 'Price to be announced',
                },
                { name: 'Multisitio', detail: 'Several sites under one organisation', price: 'Price to be announced' },
                { name: 'Dedicado', detail: 'Your own instance, database, and bucket', price: 'Priced with the engagement' },
              ]}
              caveat="Priced per site with seats included. No number is published yet — the price is set before the tier opens."
              cta={{ label: 'Join the waitlist', href: waitlistHref(`${NAMES.content} CMS waitlist`) }}
            />
          </div>

          <p className="mt-10 text-center text-sm text-gray-500 dark:text-gray-400">
            Product names are provisional and may change before launch.
          </p>
        </div>
      </section>

      {/* Capabilities Section */}
      <section className="py-24 px-4 sm:px-6 lg:px-8 bg-gray-50 dark:bg-gray-900">
        <div className="max-w-6xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl sm:text-4xl font-bold text-gray-900 dark:text-white mb-4">
              Built on Real Infrastructure
            </h2>
            <p className="text-lg text-gray-600 dark:text-gray-400">
              Truth-based capabilities. No marketing fluff.
            </p>
          </div>
          <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-8">
            <CapabilityCard
              icon={<Container className="w-8 h-8" />}
              title="Docker Containers"
              description="Deploy any Dockerfile or use Buildpacks for auto-detection. Full control over your runtime."
            />
            <CapabilityCard
              icon={<Network className="w-8 h-8" />}
              title="Port Mapping"
              description="Expose any port (4200-8080) for your services. Internal and external routing supported."
            />
            <CapabilityCard
              icon={<HardDrive className="w-8 h-8" />}
              title="Persistent Volumes"
              description="Longhorn CSI block storage for databases. Data persists across deployments."
            />
            <CapabilityCard
              icon={<Globe className="w-8 h-8" />}
              title="Custom Domains"
              description="Zero-trust ingress via Cloudflare Tunnel. Auto SSL certificates included."
            />
            <CapabilityCard
              icon={<Database className="w-8 h-8" />}
              title="Managed Databases"
              description="Postgres, Redis, and MySQL add-ons with scheduled backups, retention, and per-tenant network policy. Running in production today."
            />
            <CapabilityCard
              icon={<Cpu className="w-8 h-8" />}
              title="Self-Hosted CI Runners"
              description="Enclii's own builds run on a self-hosted GitHub Actions runner pool on this platform, from a signed runner image."
            />
          </div>
        </div>
      </section>

      {/* Why Enclii Section */}
      <section className="py-24 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl sm:text-4xl font-bold text-gray-900 dark:text-white mb-4">
              Built for Teams Who Ship
            </h2>
          </div>
          <div className="space-y-6">
            <BenefitRow text="Deploy any Dockerfile or use auto-detection with Nixpacks/Buildpacks" />
            <BenefitRow text="Automatic SSL certificates and custom domain routing" />
            <BenefitRow text="Managed Postgres, Redis, and MySQL add-ons with scheduled backups" />
            <BenefitRow text="Built-in secrets management backed by Vault" />
            <BenefitRow text="Usage tracking with budget alerts before you overspend" />
            <BenefitRow text="AGPL-3.0 open source, with a commercial licence for embedding — no vendor lock-in" />
          </div>
        </div>
      </section>

      {/* CTA Section */}
      <section className="py-24 px-4 sm:px-6 lg:px-8 hero-gradient">
        <div className="max-w-4xl mx-auto text-center">
          <h2 className="text-3xl sm:text-4xl font-bold text-white mb-6">Ready to Deploy Smarter?</h2>
          <p className="text-lg text-white/80 mb-10 max-w-2xl mx-auto">
            Start free on the self-hosted tier, or sign up and we will help you onboard.
          </p>
          <div className="flex flex-col sm:flex-row gap-4 justify-center">
            <a
              href="https://app.enclii.dev/signup"
              className="inline-flex items-center justify-center gap-2 bg-solarpunk-green text-solarpunk-slate px-8 py-4 rounded-xl font-semibold text-lg hover:bg-solarpunk-green-dim transition-colors shadow-lg"
            >
              Create your account
              <ArrowRight className="w-5 h-5" />
            </a>
            <a
              href="https://github.com/madfam-org/enclii"
              className="inline-flex items-center justify-center gap-2 bg-white/10 text-white border border-white/20 px-8 py-4 rounded-xl font-semibold text-lg hover:bg-white/20 transition-colors"
            >
              Star on GitHub
              <ExternalLink className="w-5 h-5" />
            </a>
          </div>
        </div>
      </section>

      <SiteFooter />
    </main>
  )
}

import { ArrowRight, Zap, RefreshCw, BarChart3, GitBranch, Check, ExternalLink, Container, Network, Database, Globe, Sparkles, Crown } from 'lucide-react'

export default function Home() {
  return (
    <main className="min-h-screen">
      {/* Navigation */}
      <nav className="fixed top-0 left-0 right-0 z-50 bg-white/80 dark:bg-gray-900/80 backdrop-blur-md border-b border-gray-200 dark:border-gray-800">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex justify-between items-center h-16">
            <div className="flex items-center gap-2">
              <div className="w-8 h-8 bg-solarpunk-deep rounded-lg flex items-center justify-center">
                <span className="text-white font-bold text-lg">E</span>
              </div>
              <span className="font-bold text-xl text-gray-900 dark:text-white">Enclii</span>
            </div>
            <div className="flex items-center gap-2 sm:gap-4">
              <a
                href="https://docs.enclii.dev"
                className="hidden sm:inline text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white transition-colors text-sm"
              >
                Docs
              </a>
              <a
                href="https://github.com/madfam-org/enclii"
                className="hidden sm:inline text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white transition-colors text-sm"
              >
                GitHub
              </a>
              <a
                href="https://app.enclii.dev/signup"
                className="inline-flex items-center gap-2 bg-solarpunk-green text-solarpunk-slate px-3 py-2 sm:px-4 rounded-lg font-medium hover:bg-solarpunk-green-dim transition-colors text-sm sm:text-base"
              >
                Get Started
                <ArrowRight className="w-4 h-4" />
              </a>
            </div>
          </div>
        </div>
      </nav>

      {/* Hero Section */}
      <section className="hero-gradient pt-32 pb-20 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto text-center">
          <div className="inline-flex items-center gap-2 bg-solarpunk-green/10 backdrop-blur-sm px-4 py-2 rounded-full text-solarpunk-green text-sm mb-8 pulse-glow">
            <span className="inline-block w-2 h-2 bg-solarpunk-green rounded-full animate-pulse"></span>
            Production Ready
          </div>
          <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold text-white mb-6 leading-tight">
            Deploy Without<br />the Bill Shock
          </h1>
          <p className="text-xl text-white/80 mb-10 max-w-2xl mx-auto">
            Open source container deployment platform. Auto-scaling, zero-downtime deployments,
            and built-in observability on infrastructure you own.
          </p>
          <div className="flex flex-col sm:flex-row gap-4 justify-center">
            <a
              href="https://app.enclii.dev/signup"
              className="inline-flex items-center justify-center gap-2 bg-solarpunk-green text-solarpunk-slate px-8 py-4 rounded-xl font-semibold text-lg hover:bg-solarpunk-green-dim transition-colors shadow-lg"
            >
              Start Deploying
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
              Enterprise-grade deployment infrastructure without the enterprise price tag.
            </p>
          </div>
          <div className="grid md:grid-cols-2 lg:grid-cols-4 gap-8">
            <FeatureCard
              icon={<Zap className="w-8 h-8" />}
              title="Auto-Scaling"
              description="Scale to zero when idle, scale to millions under load. Pay only for what you use."
            />
            <FeatureCard
              icon={<RefreshCw className="w-8 h-8" />}
              title="Zero-Downtime Deploys"
              description="Canary and blue-green deployment strategies with automatic rollback on failure."
            />
            <FeatureCard
              icon={<BarChart3 className="w-8 h-8" />}
              title="Built-in Observability"
              description="Logs, metrics, and traces included. Know exactly what your services are doing."
            />
            <FeatureCard
              icon={<GitBranch className="w-8 h-8" />}
              title="Git-Connected CI/CD"
              description="Push to deploy. Preview environments for every PR. Production from main."
            />
          </div>
        </div>
      </section>

      {/* Pricing Section - 3 Tiers */}
      <section className="py-24 px-4 sm:px-6 lg:px-8">
        <div className="max-w-6xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl sm:text-4xl font-bold text-gray-900 dark:text-white mb-4">
              Simple, Transparent Pricing
            </h2>
            <p className="text-lg text-gray-600 dark:text-gray-400">
              Start free, scale as you grow. No hidden fees, no surprises.
            </p>
          </div>

          <div className="grid md:grid-cols-3 gap-8">
            {/* Community Tier */}
            <PricingCard
              icon={<Sparkles className="w-6 h-6" />}
              name="Community"
              price="Free"
              description="Self-host with AGPL-3.0 source"
              features={[
                '1 project',
                '3 services per project',
                'Full source code access',
                'Community support',
                'Self-hosted infrastructure',
              ]}
              cta={{ label: 'View on GitHub', href: 'https://github.com/madfam-org/enclii', external: true }}
            />

            {/* Sovereign Tier */}
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

            {/* Ecosystem Tier */}
            <PricingCard
              icon={<Crown className="w-6 h-6" />}
              name="Ecosystem"
              price="Coming Soon"
              description="Full bundle with team management"
              features={[
                'Unlimited projects',
                'Unlimited services',
                'Team management',
                'SSO integration (Janua)',
                'Billing integration (Dhanam)',
                'SLA guarantee',
              ]}
              cta={{ label: 'Join Waitlist', href: '#', disabled: true }}
            />
          </div>

          {/* Cost Comparison Note */}
          <div className="mt-12 text-center">
            <p className="text-gray-600 dark:text-gray-400 mb-4">
              Compare to traditional SaaS: <span className="line-through">Railway Pro $2,000/mo + Auth0 $220/mo</span>
            </p>
            <p className="text-lg font-semibold text-solarpunk-green-dim dark:text-solarpunk-green">
              5-year savings: up to $127,200 with zero vendor lock-in
            </p>
          </div>
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
          <div className="grid md:grid-cols-2 lg:grid-cols-4 gap-8">
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
              icon={<Database className="w-8 h-8" />}
              title="Persistent Volumes"
              description="Longhorn CSI block storage for databases. Data persists across deployments."
            />
            <CapabilityCard
              icon={<Globe className="w-8 h-8" />}
              title="Custom Domains"
              description="Zero-trust ingress via Cloudflare Tunnel. Auto SSL certificates included."
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
            <BenefitRow text="Preview environments for every pull request" />
            <BenefitRow text="Built-in secrets management with rotation support" />
            <BenefitRow text="Cost tracking with budget alerts before you overspend" />
            <BenefitRow text="Open source with AGPL-3.0 license - no vendor lock-in" />
          </div>
        </div>
      </section>

      {/* CTA Section */}
      <section className="py-24 px-4 sm:px-6 lg:px-8 hero-gradient">
        <div className="max-w-4xl mx-auto text-center">
          <h2 className="text-3xl sm:text-4xl font-bold text-white mb-6">
            Ready to Deploy Smarter?
          </h2>
          <p className="text-lg text-white/80 mb-10 max-w-2xl mx-auto">
            Join teams who are shipping faster while keeping costs under control.
          </p>
          <div className="flex flex-col sm:flex-row gap-4 justify-center">
            <a
              href="https://app.enclii.dev/signup"
              className="inline-flex items-center justify-center gap-2 bg-solarpunk-green text-solarpunk-slate px-8 py-4 rounded-xl font-semibold text-lg hover:bg-solarpunk-green-dim transition-colors shadow-lg"
            >
              Start Building Free
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

      {/* Footer */}
      <footer className="border-t border-gray-200 dark:border-gray-800 py-12 px-4 sm:px-6 lg:px-8">
        <div className="max-w-7xl mx-auto">
          <div className="grid md:grid-cols-4 gap-8">
            <div>
              <div className="flex items-center gap-2 mb-4">
                <div className="w-8 h-8 bg-solarpunk-deep rounded-lg flex items-center justify-center">
                  <span className="text-white font-bold text-lg">E</span>
                </div>
                <span className="font-bold text-xl text-gray-900 dark:text-white">Enclii</span>
              </div>
              <p className="text-gray-600 dark:text-gray-400 text-sm">
                Deploy without the bill shock.
              </p>
            </div>
            <div>
              <h4 className="font-semibold text-gray-900 dark:text-white mb-4">Product</h4>
              <ul className="space-y-2 text-sm">
                <FooterLink href="https://app.enclii.dev">Dashboard</FooterLink>
                <FooterLink href="https://docs.enclii.dev">Documentation</FooterLink>
                <FooterLink href="https://docs.enclii.dev/changelog">Changelog</FooterLink>
              </ul>
            </div>
            <div>
              <h4 className="font-semibold text-gray-900 dark:text-white mb-4">Resources</h4>
              <ul className="space-y-2 text-sm">
                <FooterLink href="https://github.com/madfam-org/enclii">GitHub</FooterLink>
                <FooterLink href="https://docs.enclii.dev/guides">Guides</FooterLink>
                <FooterLink href="https://docs.enclii.dev/api">API Reference</FooterLink>
              </ul>
            </div>
            <div>
              <h4 className="font-semibold text-gray-900 dark:text-white mb-4">Company</h4>
              <ul className="space-y-2 text-sm">
                <FooterLink href="https://madfam.io">About</FooterLink>
                <FooterLink href="https://status.enclii.dev">Status</FooterLink>
              </ul>
            </div>
          </div>
          <div className="border-t border-gray-200 dark:border-gray-800 mt-12 pt-8 text-center text-gray-600 dark:text-gray-400 text-sm">
            <p>&copy; {new Date().getFullYear()} Madfam. All rights reserved.</p>
          </div>
        </div>
      </footer>
    </main>
  )
}

function FeatureCard({ icon, title, description }: { icon: React.ReactNode; title: string; description: string }) {
  return (
    <div className="feature-card bg-white dark:bg-gray-800 p-6 rounded-xl shadow-lg border border-gray-200 dark:border-gray-700">
      <div className="w-14 h-14 bg-solarpunk-green-muted text-solarpunk-green-dim dark:text-solarpunk-green rounded-xl flex items-center justify-center mb-4">
        {icon}
      </div>
      <h3 className="text-xl font-semibold text-gray-900 dark:text-white mb-2">{title}</h3>
      <p className="text-gray-600 dark:text-gray-400">{description}</p>
    </div>
  )
}

function CapabilityCard({ icon, title, description }: { icon: React.ReactNode; title: string; description: string }) {
  return (
    <div className="bg-white dark:bg-gray-800 p-6 rounded-xl border border-gray-200 dark:border-gray-700">
      <div className="w-12 h-12 bg-solarpunk-green-muted text-solarpunk-green-dim dark:text-solarpunk-green rounded-lg flex items-center justify-center mb-4">
        {icon}
      </div>
      <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">{title}</h3>
      <p className="text-sm text-gray-600 dark:text-gray-400">{description}</p>
    </div>
  )
}

interface PricingCardProps {
  icon: React.ReactNode
  name: string
  price: string
  priceNote?: string
  description: string
  features: string[]
  cta: { label: string; href: string; external?: boolean; disabled?: boolean }
  highlighted?: boolean
}

function PricingCard({ icon, name, price, priceNote, description, features, cta, highlighted }: PricingCardProps) {
  return (
    <div className={`relative rounded-2xl p-8 ${
      highlighted
        ? 'bg-solarpunk-deep text-white ring-4 ring-solarpunk-green/20 shadow-xl md:scale-105'
        : 'bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700'
    }`}>
      {highlighted && (
        <div className="absolute -top-4 left-1/2 -translate-x-1/2">
          <span className="bg-solarpunk-green text-solarpunk-slate text-sm font-medium px-3 py-1 rounded-full">
            Most Popular
          </span>
        </div>
      )}

      <div className={`w-12 h-12 rounded-xl flex items-center justify-center mb-4 ${
        highlighted
          ? 'bg-solarpunk-green/20 text-solarpunk-green'
          : 'bg-solarpunk-green-muted text-solarpunk-green-dim dark:text-solarpunk-green'
      }`}>
        {icon}
      </div>

      <h3 className={`text-xl font-bold mb-1 ${highlighted ? 'text-white' : 'text-gray-900 dark:text-white'}`}>
        {name}
      </h3>

      <div className="mb-2">
        <span className={`text-4xl font-bold ${highlighted ? 'text-white' : 'text-gray-900 dark:text-white'}`}>
          {price}
        </span>
        {priceNote && (
          <span className={`text-sm ${highlighted ? 'text-white/80' : 'text-gray-500 dark:text-gray-400'}`}>
            {priceNote}
          </span>
        )}
      </div>

      <p className={`text-sm mb-6 ${highlighted ? 'text-white/80' : 'text-gray-600 dark:text-gray-400'}`}>
        {description}
      </p>

      <ul className="space-y-3 mb-8">
        {features.map((feature, index) => (
          <li key={index} className="flex items-center gap-2">
            <Check className={`w-5 h-5 flex-shrink-0 ${highlighted ? 'text-green-300' : 'text-green-500'}`} />
            <span className={`text-sm ${highlighted ? 'text-white/90' : 'text-gray-700 dark:text-gray-300'}`}>
              {feature}
            </span>
          </li>
        ))}
      </ul>

      {cta.disabled ? (
        <button
          disabled
          className={`w-full py-3 px-4 rounded-xl font-semibold text-center cursor-not-allowed ${
            highlighted
              ? 'bg-white/20 text-white/60'
              : 'bg-gray-100 dark:bg-gray-700 text-gray-400 dark:text-gray-500'
          }`}
        >
          {cta.label}
        </button>
      ) : (
        <a
          href={cta.href}
          target={cta.external ? '_blank' : undefined}
          rel={cta.external ? 'noopener noreferrer' : undefined}
          className={`block w-full py-3 px-4 rounded-xl font-semibold text-center transition-colors ${
            highlighted
              ? 'bg-solarpunk-green text-solarpunk-slate hover:bg-solarpunk-green-dim'
              : 'bg-solarpunk-deep text-white hover:bg-solarpunk-slate'
          }`}
        >
          {cta.label}
          {cta.external && <ExternalLink className="w-4 h-4 inline ml-2" />}
        </a>
      )}
    </div>
  )
}

function BenefitRow({ text }: { text: string }) {
  return (
    <div className="flex items-center gap-4 bg-white dark:bg-gray-800 p-4 rounded-xl border border-gray-200 dark:border-gray-700">
      <div className="w-8 h-8 bg-green-100 dark:bg-green-900/30 text-green-600 dark:text-green-400 rounded-full flex items-center justify-center flex-shrink-0">
        <Check className="w-5 h-5" />
      </div>
      <span className="text-gray-900 dark:text-white">{text}</span>
    </div>
  )
}

function FooterLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <li>
      <a href={href} className="text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white transition-colors">
        {children}
      </a>
    </li>
  )
}

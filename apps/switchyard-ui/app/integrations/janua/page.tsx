'use client';

import { useState } from 'react';
import Link from 'next/link';
import {
  Shield,
  Users,
  Key,
  Lock,
  CheckCircle,
  ArrowRight,
  Zap,
  Globe,
  Code,
  Settings,
  BarChart3,
  Fingerprint,
} from 'lucide-react';

const features = [
  {
    icon: Shield,
    title: 'Enterprise SSO',
    description: 'SAML 2.0 and OIDC support for enterprise customers out of the box.',
  },
  {
    icon: Users,
    title: 'User Management',
    description: 'Built-in user dashboard, roles, and permissions management.',
  },
  {
    icon: Key,
    title: 'Social Login',
    description: 'Google, GitHub, Microsoft, and 20+ social providers pre-configured.',
  },
  {
    icon: Lock,
    title: 'Multi-Factor Auth',
    description: 'TOTP, SMS, and hardware key support for enhanced security.',
  },
  {
    icon: Fingerprint,
    title: 'Passwordless',
    description: 'Magic links and WebAuthn for frictionless authentication.',
  },
  {
    icon: BarChart3,
    title: 'Audit Logs',
    description: 'Complete authentication audit trail for compliance.',
  },
];

const sdkExamples = {
  nextjs: `// app/api/auth/[...janua]/route.ts
import { JanuaAuth } from '@janua/nextjs';

export const { GET, POST } = JanuaAuth({
  // Your Janua instance URL (deployed on Enclii)
  url: process.env.JANUA_URL,
  clientId: process.env.JANUA_CLIENT_ID,
  clientSecret: process.env.JANUA_CLIENT_SECRET,
});

// In your components
import { useSession, signIn } from '@janua/nextjs/client';

export function AuthButton() {
  const { user, isLoading } = useSession();

  if (user) return <span>Welcome, {user.name}</span>;
  return <button onClick={() => signIn()}>Sign In</button>;
}`,
  react: `// JanuaProvider setup
import { JanuaProvider, useAuth } from '@janua/react';

function App() {
  return (
    <JanuaProvider
      url={process.env.REACT_APP_JANUA_URL}
      clientId={process.env.REACT_APP_JANUA_CLIENT_ID}
    >
      <YourApp />
    </JanuaProvider>
  );
}

// Using the hook
function Dashboard() {
  const { user, login, logout, isAuthenticated } = useAuth();

  if (!isAuthenticated) {
    return <button onClick={login}>Sign In</button>;
  }

  return (
    <div>
      <h1>Welcome, {user.name}</h1>
      <button onClick={logout}>Sign Out</button>
    </div>
  );
}`,
  api: `# Direct API usage
curl -X POST https://your-janua.enclii.app/oauth/token \\
  -H "Content-Type: application/json" \\
  -d '{
    "grant_type": "authorization_code",
    "code": "AUTH_CODE",
    "client_id": "your-client-id",
    "client_secret": "your-client-secret",
    "redirect_uri": "https://your-app.com/callback"
  }'

# Response
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "dGhpcyBpcyBhIHJlZnJlc2g...",
  "id_token": "eyJhbGciOiJSUzI1NiIs..."
}`,
};

const pricingTiers = [
  {
    name: 'Included',
    price: '$0',
    description: 'Janua comes free with any Enclii plan',
    features: [
      'Unlimited users',
      'Social login providers',
      'Email/password auth',
      'Basic MFA (TOTP)',
      'User management UI',
      'REST & GraphQL APIs',
      'Community support',
    ],
    cta: 'Deploy Now',
    ctaLink: '/projects/new?template=janua',
    highlighted: false,
  },
  {
    name: 'Pro Features',
    price: '+$49',
    description: 'Enterprise features add-on',
    features: [
      'Everything in Included',
      'Enterprise SSO (SAML/OIDC)',
      'Advanced MFA (WebAuthn, SMS)',
      'Custom branding',
      'Audit log exports',
      'Compliance reports',
      'Priority support',
    ],
    cta: 'Add to Plan',
    ctaLink: '/projects/new?template=janua-enterprise',
    highlighted: true,
  },
];

export default function JanuaIntegrationPage() {
  const [activeTab, setActiveTab] = useState<'nextjs' | 'react' | 'api'>('nextjs');

  return (
    <div className="min-h-screen bg-muted/50">
      {/* Hero Section */}
      <section className="bg-gradient-to-br from-enclii-blue to-blue-700 text-white py-16">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex items-center gap-2 text-blue-200 text-sm mb-4">
            <Link href="/integrations" className="hover:text-white">Integrations</Link>
            <span>/</span>
            <span>Janua Authentication</span>
          </div>

          <div className="grid md:grid-cols-2 gap-12 items-center">
            <div>
              <div className="inline-flex items-center gap-2 px-3 py-1 bg-white/10 rounded-full text-sm mb-6">
                <Shield className="w-4 h-4" />
                <span>Official Integration</span>
              </div>

              <h1 className="text-4xl md:text-5xl font-bold mb-6">
                Janua Authentication
              </h1>

              <p className="text-xl text-blue-100 mb-8">
                Add production-ready authentication to your Enclii deployments.
                Self-hosted, secure, and included free with your plan.
              </p>

              <div className="flex flex-wrap gap-4">
                <Link
                  href="/projects/new?template=janua"
                  className="inline-flex items-center gap-2 px-6 py-3 bg-white text-primary font-semibold rounded-lg hover:bg-white/90 transition-colors"
                >
                  <Zap className="w-5 h-5" />
                  Deploy Janua
                </Link>
                <Link
                  href="https://janua.io/docs"
                  className="inline-flex items-center gap-2 px-6 py-3 bg-primary text-primary-foreground font-semibold rounded-lg hover:bg-primary/90 transition-colors"
                >
                  <Code className="w-5 h-5" />
                  View Docs
                </Link>
              </div>
            </div>

            <div className="bg-white/10 backdrop-blur rounded-2xl p-6">
              <div className="grid grid-cols-2 gap-4">
                <div className="bg-white/10 rounded-xl p-4 text-center">
                  <div className="text-3xl font-bold">∞</div>
                  <div className="text-sm text-blue-200">Users Included</div>
                </div>
                <div className="bg-white/10 rounded-xl p-4 text-center">
                  <div className="text-3xl font-bold">&lt;50ms</div>
                  <div className="text-sm text-blue-200">Auth Latency</div>
                </div>
                <div className="bg-white/10 rounded-xl p-4 text-center">
                  <div className="text-3xl font-bold">20+</div>
                  <div className="text-sm text-blue-200">Social Providers</div>
                </div>
                <div className="bg-white/10 rounded-xl p-4 text-center">
                  <div className="text-3xl font-bold">100%</div>
                  <div className="text-sm text-blue-200">Data Ownership</div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Features Grid */}
      <section className="py-16">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <h2 className="text-3xl font-bold text-foreground text-center mb-4">
            Complete Authentication Solution
          </h2>
          <p className="text-lg text-muted-foreground text-center mb-12 max-w-2xl mx-auto">
            Everything you need to secure your applications, deployed and managed on your Enclii infrastructure.
          </p>

          <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6">
            {features.map((feature, idx) => (
              <div key={idx} className="bg-card rounded-xl p-6 shadow-sm border border-border hover:shadow-md transition-shadow">
                <div className="w-12 h-12 bg-primary/10 rounded-xl flex items-center justify-center mb-4">
                  <feature.icon className="w-6 h-6 text-primary" />
                </div>
                <h3 className="text-lg font-semibold text-foreground mb-2">{feature.title}</h3>
                <p className="text-muted-foreground">{feature.description}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* SDK Examples */}
      <section className="py-16 bg-gray-900">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <h2 className="text-3xl font-bold text-white text-center mb-4">
            Integrate in Minutes
          </h2>
          <p className="text-lg text-gray-400 text-center mb-12 max-w-2xl mx-auto">
            Use our official SDKs or connect directly via OAuth 2.0 / OpenID Connect.
          </p>

          <div className="bg-gray-800 rounded-2xl overflow-hidden">
            {/* Tab Headers */}
            <div className="flex border-b border-gray-700">
              {[
                { id: 'nextjs', label: 'Next.js' },
                { id: 'react', label: 'React' },
                { id: 'api', label: 'REST API' },
              ].map((tab) => (
                <button
                  key={tab.id}
                  onClick={() => setActiveTab(tab.id as any)}
                  className={`px-6 py-4 text-sm font-medium transition-colors ${
                    activeTab === tab.id
                      ? 'text-white bg-gray-700 border-b-2 border-blue-500'
                      : 'text-gray-400 hover:text-white'
                  }`}
                >
                  {tab.label}
                </button>
              ))}
            </div>

            {/* Code Content */}
            <div className="p-6">
              <pre className="text-sm text-gray-300 overflow-x-auto">
                <code>{sdkExamples[activeTab]}</code>
              </pre>
            </div>
          </div>
        </div>
      </section>

      {/* Pricing */}
      <section className="py-16">
        <div className="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8">
          <h2 className="text-3xl font-bold text-foreground text-center mb-4">
            Simple, Transparent Pricing
          </h2>
          <p className="text-lg text-muted-foreground text-center mb-12 max-w-2xl mx-auto">
            Janua is included free with every Enclii plan. Add enterprise features when you need them.
          </p>

          <div className="grid md:grid-cols-2 gap-8">
            {pricingTiers.map((tier, idx) => (
              <div
                key={idx}
                className={`rounded-2xl p-8 ${
                  tier.highlighted
                    ? 'bg-primary text-primary-foreground ring-4 ring-primary ring-offset-4'
                    : 'bg-card border border-border'
                }`}
              >
                <h3 className={`text-xl font-semibold mb-2 ${tier.highlighted ? 'text-primary-foreground' : 'text-foreground'}`}>
                  {tier.name}
                </h3>
                <div className="flex items-baseline gap-1 mb-2">
                  <span className={`text-4xl font-bold ${tier.highlighted ? 'text-primary-foreground' : 'text-foreground'}`}>
                    {tier.price}
                  </span>
                  <span className={tier.highlighted ? 'text-primary-foreground/70' : 'text-muted-foreground'}>/month</span>
                </div>
                <p className={`mb-6 ${tier.highlighted ? 'text-primary-foreground/80' : 'text-muted-foreground'}`}>
                  {tier.description}
                </p>

                <ul className="space-y-3 mb-8">
                  {tier.features.map((feature, fidx) => (
                    <li key={fidx} className="flex items-start gap-3">
                      <CheckCircle className={`w-5 h-5 flex-shrink-0 mt-0.5 ${
                        tier.highlighted ? 'text-blue-200' : 'text-status-success'
                      }`} />
                      <span className={tier.highlighted ? 'text-primary-foreground' : 'text-foreground'}>
                        {feature}
                      </span>
                    </li>
                  ))}
                </ul>

                <Link
                  href={tier.ctaLink}
                  className={`block w-full text-center py-3 px-6 rounded-lg font-semibold transition-colors ${
                    tier.highlighted
                      ? 'bg-card text-primary hover:bg-accent'
                      : 'bg-foreground text-background hover:bg-foreground/90'
                  }`}
                >
                  {tier.cta}
                </Link>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* CTA */}
      <section className="py-16 bg-muted">
        <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 text-center">
          <h2 className="text-3xl font-bold text-foreground mb-4">
            Ready to Add Authentication?
          </h2>
          <p className="text-lg text-muted-foreground mb-8">
            Deploy Janua to your Enclii project in one click. No credit card required.
          </p>
          <div className="flex flex-col sm:flex-row gap-4 justify-center">
            <Link
              href="/projects/new?template=janua"
              className="inline-flex items-center justify-center gap-2 px-8 py-4 bg-enclii-blue text-white font-semibold rounded-lg hover:bg-blue-700 transition-colors"
            >
              Deploy Janua Now
              <ArrowRight className="w-5 h-5" />
            </Link>
            <Link
              href="https://janua.io"
              className="inline-flex items-center justify-center gap-2 px-8 py-4 bg-card text-foreground font-semibold rounded-lg border border-border hover:bg-accent transition-colors"
            >
              Learn More About Janua
            </Link>
          </div>
        </div>
      </section>
    </div>
  );
}

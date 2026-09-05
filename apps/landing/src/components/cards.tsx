import { Check, ExternalLink } from 'lucide-react'

export function FeatureCard({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode
  title: string
  description: string
}) {
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

export function CapabilityCard({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode
  title: string
  description: string
}) {
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

export interface PricingCardProps {
  icon: React.ReactNode
  name: string
  price: string
  priceNote?: string
  description: string
  features: string[]
  footnote?: string
  cta: { label: string; href: string; external?: boolean; disabled?: boolean }
  highlighted?: boolean
}

export function PricingCard({
  icon,
  name,
  price,
  priceNote,
  description,
  features,
  footnote,
  cta,
  highlighted,
}: PricingCardProps) {
  return (
    <div
      className={`relative rounded-2xl p-8 ${
        highlighted
          ? 'bg-solarpunk-deep text-white ring-4 ring-solarpunk-green/20 shadow-xl md:scale-105'
          : 'bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700'
      }`}
    >
      {highlighted && (
        <div className="absolute -top-4 left-1/2 -translate-x-1/2">
          <span className="bg-solarpunk-green text-solarpunk-slate text-sm font-medium px-3 py-1 rounded-full">
            Most Popular
          </span>
        </div>
      )}

      <div
        className={`w-12 h-12 rounded-xl flex items-center justify-center mb-4 ${
          highlighted
            ? 'bg-solarpunk-green/20 text-solarpunk-green'
            : 'bg-solarpunk-green-muted text-solarpunk-green-dim dark:text-solarpunk-green'
        }`}
      >
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
            <Check className={`w-5 h-5 flex-shrink-0 ${highlighted ? 'text-solarpunk-green' : 'text-solarpunk-green-dim'}`} />
            <span className={`text-sm ${highlighted ? 'text-white/90' : 'text-gray-700 dark:text-gray-300'}`}>
              {feature}
            </span>
          </li>
        ))}
      </ul>

      {footnote && (
        <p className={`text-xs mb-6 ${highlighted ? 'text-white/60' : 'text-gray-500 dark:text-gray-400'}`}>
          {footnote}
        </p>
      )}

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

export function IncludedCard({
  icon,
  title,
  description,
  badge,
}: {
  icon: React.ReactNode
  title: string
  description: string
  badge?: string
}) {
  return (
    <div className="bg-white dark:bg-gray-800 p-5 rounded-xl border border-gray-200 dark:border-gray-700">
      <div className="flex items-center gap-3 mb-2">
        <div className="w-9 h-9 bg-solarpunk-green-muted text-solarpunk-green-dim dark:text-solarpunk-green rounded-lg flex items-center justify-center shrink-0">
          {icon}
        </div>
        <h4 className="text-base font-semibold text-gray-900 dark:text-white">{title}</h4>
        {badge && (
          <span className="ml-auto text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400 border border-gray-300 dark:border-gray-600 rounded-full px-2 py-0.5">
            {badge}
          </span>
        )}
      </div>
      <p className="text-sm text-gray-600 dark:text-gray-400">{description}</p>
    </div>
  )
}

export interface PlannedTier {
  name: string
  detail: string
  price: string
}

export function PlannedLineCard({
  icon,
  line,
  name,
  summary,
  tiers,
  caveat,
  cta,
}: {
  icon: React.ReactNode
  line: string
  name: string
  summary: string
  tiers: PlannedTier[]
  caveat: string
  cta: { label: string; href: string }
}) {
  return (
    <div className="flex flex-col bg-white dark:bg-gray-800 rounded-2xl border border-gray-200 dark:border-gray-700 p-8">
      <div className="flex items-center gap-3 mb-4">
        <div className="w-12 h-12 bg-solarpunk-green-muted text-solarpunk-green-dim dark:text-solarpunk-green rounded-xl flex items-center justify-center shrink-0">
          {icon}
        </div>
        <div>
          <p className="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">{line}</p>
          <h3 className="text-xl font-bold text-gray-900 dark:text-white">{name}</h3>
        </div>
      </div>

      <p className="text-sm text-gray-600 dark:text-gray-400 mb-6">{summary}</p>

      <ul className="space-y-3 mb-6">
        {tiers.map((tier) => (
          <li
            key={tier.name}
            className="flex items-baseline justify-between gap-4 border-b border-gray-100 dark:border-gray-700 pb-3 last:border-b-0 last:pb-0"
          >
            <span className="text-sm text-gray-700 dark:text-gray-300">
              <span className="font-semibold text-gray-900 dark:text-white">{tier.name}</span>
              <span className="block text-xs text-gray-500 dark:text-gray-400">{tier.detail}</span>
            </span>
            <span className="text-sm font-semibold text-gray-900 dark:text-white whitespace-nowrap">{tier.price}</span>
          </li>
        ))}
      </ul>

      <p className="text-xs text-gray-500 dark:text-gray-400 mb-6">{caveat}</p>

      <a
        href={cta.href}
        className="mt-auto block w-full py-3 px-4 rounded-xl font-semibold text-center bg-solarpunk-deep text-white hover:bg-solarpunk-slate transition-colors"
      >
        {cta.label}
      </a>
    </div>
  )
}

export function BenefitRow({ text }: { text: string }) {
  return (
    <div className="flex items-center gap-4 bg-white dark:bg-gray-800 p-4 rounded-xl border border-gray-200 dark:border-gray-700">
      <div className="w-8 h-8 bg-solarpunk-green-muted text-solarpunk-green-dim dark:text-solarpunk-green rounded-full flex items-center justify-center flex-shrink-0">
        <Check className="w-5 h-5" />
      </div>
      <span className="text-gray-900 dark:text-white">{text}</span>
    </div>
  )
}

export function EngagementCard({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode
  title: string
  description: string
}) {
  return (
    <div className="bg-white dark:bg-gray-800 p-6 rounded-xl border border-gray-200 dark:border-gray-700">
      <div className="w-11 h-11 bg-solarpunk-green-muted text-solarpunk-green-dim dark:text-solarpunk-green rounded-lg flex items-center justify-center mb-4">
        {icon}
      </div>
      <h3 className="text-base font-semibold text-gray-900 dark:text-white mb-2">{title}</h3>
      <p className="text-sm text-gray-600 dark:text-gray-400">{description}</p>
    </div>
  )
}

export function FooterLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <li>
      <a
        href={href}
        className="text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white transition-colors"
      >
        {children}
      </a>
    </li>
  )
}

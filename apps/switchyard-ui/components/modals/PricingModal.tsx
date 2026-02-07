'use client';

import { ExternalLink, Check, Sparkles, Zap, Crown } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { TIER_CONFIG, type BlockedAction, type FoundryTier } from '@/lib/tiers';

// =============================================================================
// TYPES
// =============================================================================

interface PricingModalProps {
  isOpen: boolean;
  onClose: () => void;
  blockedAction: BlockedAction | null;
  upgradeMessage: string;
  checkoutUrl: string;
  currentTier: FoundryTier;
}

// =============================================================================
// TIER CARD COMPONENT
// =============================================================================

interface TierCardProps {
  tier: FoundryTier | 'null';
  isCurrentTier: boolean;
  isRecommended: boolean;
}

interface TierCardPropsInternal extends TierCardProps {
  checkoutUrl?: string;
}

function TierCard({ tier, isCurrentTier, isRecommended, checkoutUrl }: TierCardPropsInternal) {
  const config = TIER_CONFIG[tier ?? 'null'];
  const Icon = tier === 'ecosystem' ? Crown : tier === 'sovereign' ? Zap : Sparkles;
  // Inside the modal, paid tiers should link to Dhanam checkout, not app.enclii.dev
  const effectiveHref = (tier === 'sovereign' && checkoutUrl) ? checkoutUrl : config.cta.href;

  return (
    <div
      className={`relative rounded-lg border p-4 ${
        isRecommended
          ? 'border-enclii-blue ring-2 ring-enclii-blue/20 bg-enclii-blue/5'
          : 'border-border'
      } ${isCurrentTier ? 'opacity-60' : ''}`}
    >
      {isRecommended && (
        <div className="absolute -top-2.5 left-1/2 -translate-x-1/2">
          <span className="bg-enclii-blue text-white text-xs font-medium px-2 py-0.5 rounded-full">
            Recommended
          </span>
        </div>
      )}

      <div className="flex items-center gap-2 mb-2">
        <Icon className={`h-5 w-5 ${isRecommended ? 'text-enclii-blue' : 'text-muted-foreground'}`} />
        <h4 className="font-semibold">{config.name}</h4>
        {isCurrentTier && (
          <span className="text-xs bg-muted text-muted-foreground px-1.5 py-0.5 rounded">
            Current
          </span>
        )}
      </div>

      <p className="text-2xl font-bold mb-2">{config.price}</p>
      <p className="text-sm text-muted-foreground mb-3">{config.description}</p>

      <ul className="space-y-1.5 text-sm mb-4">
        <li className="flex items-center gap-2">
          <Check className="h-4 w-4 text-status-success flex-shrink-0" />
          <span>
            {config.projectLimit === -1
              ? 'Unlimited projects'
              : `${config.projectLimit} project${config.projectLimit !== 1 ? 's' : ''}`}
          </span>
        </li>
        <li className="flex items-center gap-2">
          <Check className="h-4 w-4 text-status-success flex-shrink-0" />
          <span>
            {config.serviceLimit === -1
              ? 'Unlimited services'
              : `${config.serviceLimit} service${config.serviceLimit !== 1 ? 's' : ''}`}
          </span>
        </li>
        {config.canUseCustomDomains && (
          <li className="flex items-center gap-2">
            <Check className="h-4 w-4 text-status-success flex-shrink-0" />
            <span>Custom domains</span>
          </li>
        )}
        {config.canManageTeams && (
          <li className="flex items-center gap-2">
            <Check className="h-4 w-4 text-status-success flex-shrink-0" />
            <span>Team management</span>
          </li>
        )}
      </ul>

      {!isCurrentTier && (
        <Button
          className="w-full"
          variant={isRecommended ? 'default' : 'outline'}
          disabled={config.cta.disabled}
          asChild={!config.cta.disabled}
        >
          {config.cta.disabled ? (
            <span>{config.cta.label}</span>
          ) : (
            <a href={effectiveHref} target={effectiveHref.startsWith('http') ? '_blank' : undefined}>
              {config.cta.label}
              {effectiveHref.startsWith('http') && <ExternalLink className="ml-2 h-4 w-4" />}
            </a>
          )}
        </Button>
      )}
    </div>
  );
}

// =============================================================================
// PRICING MODAL COMPONENT
// =============================================================================

export function PricingModal({
  isOpen,
  onClose,
  blockedAction,
  upgradeMessage,
  checkoutUrl,
  currentTier,
}: PricingModalProps) {
  const getTitle = () => {
    switch (blockedAction) {
      case 'project':
        return 'Create More Projects';
      case 'deploy':
        return 'Deploy More Services';
      case 'custom-domain':
        return 'Unlock Custom Domains';
      case 'team':
        return 'Unlock Team Features';
      default:
        return 'Upgrade Your Plan';
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="text-xl">{getTitle()}</DialogTitle>
          <DialogDescription>{upgradeMessage}</DialogDescription>
        </DialogHeader>

        <div className="grid sm:grid-cols-3 gap-4 mt-4">
          <TierCard
            tier="community"
            isCurrentTier={currentTier === 'community'}
            isRecommended={false}
          />
          <TierCard
            tier="sovereign"
            isCurrentTier={currentTier === 'sovereign'}
            isRecommended={currentTier !== 'sovereign' && currentTier !== 'ecosystem'}
            checkoutUrl={checkoutUrl}
          />
          <TierCard
            tier="ecosystem"
            isCurrentTier={currentTier === 'ecosystem'}
            isRecommended={false}
          />
        </div>

        <div className="flex justify-between items-center mt-4 pt-4 border-t">
          <p className="text-sm text-muted-foreground">
            Need help choosing?{' '}
            <a href="https://docs.enclii.dev/pricing" className="text-enclii-blue hover:underline">
              Compare plans
            </a>
          </p>
          <Button variant="ghost" onClick={onClose}>
            Maybe Later
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export default PricingModal;

'use client';

import * as React from 'react';
import { cn } from '@/lib/utils';

// =============================================================================
// TYPES
// =============================================================================

interface CircularGaugeProps {
  /** Current value (0-100 or raw value with max specified) */
  value: number;
  /** Maximum value (defaults to 100 for percentage) */
  max?: number;
  /** Size of the gauge in pixels */
  size?: number;
  /** Stroke width of the gauge ring */
  strokeWidth?: number;
  /** Label to display below the value */
  label?: string;
  /** Format function for the value display */
  formatValue?: (value: number, max: number) => string;
  /** Color variant based on threshold */
  variant?: 'default' | 'success' | 'warning' | 'danger' | 'auto';
  /** Warning threshold (0-100 percentage) for auto variant */
  warningThreshold?: number;
  /** Danger threshold (0-100 percentage) for auto variant */
  dangerThreshold?: number;
  /** Additional class names */
  className?: string;
  /** Show percentage or raw value */
  showPercentage?: boolean;
  /** Animation duration in ms (0 to disable) */
  animationDuration?: number;
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

function getColorClasses(
  variant: CircularGaugeProps['variant'],
  percentage: number,
  warningThreshold: number,
  dangerThreshold: number
): { stroke: string; text: string; bg: string } {
  let effectiveVariant = variant;

  if (variant === 'auto') {
    if (percentage >= dangerThreshold) {
      effectiveVariant = 'danger';
    } else if (percentage >= warningThreshold) {
      effectiveVariant = 'warning';
    } else {
      effectiveVariant = 'success';
    }
  }

  switch (effectiveVariant) {
    case 'success':
      return {
        stroke: 'stroke-status-success',
        text: 'text-status-success',
        bg: 'stroke-status-success/20',
      };
    case 'warning':
      return {
        stroke: 'stroke-status-warning',
        text: 'text-status-warning',
        bg: 'stroke-status-warning/20',
      };
    case 'danger':
      return {
        stroke: 'stroke-status-error',
        text: 'text-status-error',
        bg: 'stroke-status-error/20',
      };
    default:
      return {
        stroke: 'stroke-enclii-blue',
        text: 'text-foreground',
        bg: 'stroke-muted',
      };
  }
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function formatNumber(num: number): string {
  if (num >= 1000000) {
    return (num / 1000000).toFixed(1) + 'M';
  }
  if (num >= 1000) {
    return (num / 1000).toFixed(1) + 'K';
  }
  return num.toFixed(0);
}

// =============================================================================
// CIRCULAR GAUGE COMPONENT
// =============================================================================

export function CircularGauge({
  value,
  max = 100,
  size = 120,
  strokeWidth = 8,
  label,
  formatValue,
  variant = 'auto',
  warningThreshold = 75,
  dangerThreshold = 90,
  className,
  showPercentage = true,
  animationDuration = 500,
}: CircularGaugeProps) {
  const [animatedValue, setAnimatedValue] = React.useState(0);

  // Calculate percentage
  const percentage = Math.min(100, Math.max(0, (value / max) * 100));

  // Animation effect
  React.useEffect(() => {
    if (animationDuration === 0) {
      setAnimatedValue(percentage);
      return;
    }

    let startValue = 0;
    setAnimatedValue((prev) => {
      startValue = prev;
      return prev;
    });

    const diff = percentage - startValue;
    const startTime = Date.now();

    const animate = () => {
      const elapsed = Date.now() - startTime;
      const progress = Math.min(1, elapsed / animationDuration);

      // Easing function (ease-out-cubic)
      const eased = 1 - Math.pow(1 - progress, 3);
      const currentValue = startValue + diff * eased;

      setAnimatedValue(currentValue);

      if (progress < 1) {
        requestAnimationFrame(animate);
      }
    };

    requestAnimationFrame(animate);
  }, [percentage, animationDuration]);

  // SVG calculations
  const center = size / 2;
  const radius = (size - strokeWidth) / 2;
  const circumference = 2 * Math.PI * radius;
  const strokeDashoffset = circumference - (animatedValue / 100) * circumference;

  // Get color classes
  const colors = getColorClasses(variant, percentage, warningThreshold, dangerThreshold);

  // Format display value
  const displayValue = formatValue
    ? formatValue(value, max)
    : showPercentage
      ? `${Math.round(percentage)}%`
      : formatNumber(value);

  return (
    <div className={cn('inline-flex flex-col items-center gap-2', className)}>
      <div className="relative" style={{ width: size, height: size }}>
        <svg
          width={size}
          height={size}
          viewBox={`0 0 ${size} ${size}`}
          className="transform -rotate-90"
        >
          {/* Background circle */}
          <circle
            cx={center}
            cy={center}
            r={radius}
            fill="none"
            strokeWidth={strokeWidth}
            className={colors.bg}
          />
          {/* Progress circle */}
          <circle
            cx={center}
            cy={center}
            r={radius}
            fill="none"
            strokeWidth={strokeWidth}
            strokeLinecap="round"
            strokeDasharray={circumference}
            strokeDashoffset={strokeDashoffset}
            className={cn(colors.stroke, 'transition-all duration-300')}
          />
        </svg>
        {/* Center content */}
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <span
            className={cn(
              'font-mono font-semibold tabular-nums',
              colors.text,
              size >= 120 ? 'text-xl' : size >= 80 ? 'text-lg' : 'text-sm'
            )}
          >
            {displayValue}
          </span>
        </div>
      </div>
      {label && (
        <span className="text-xs text-muted-foreground font-medium text-center max-w-[120px] truncate">
          {label}
        </span>
      )}
    </div>
  );
}

// =============================================================================
// USAGE GAUGE VARIANT (Pre-configured for resource usage)
// =============================================================================

interface UsageGaugeProps {
  /** Current usage value */
  used: number;
  /** Maximum/limit value */
  limit: number;
  /** Resource name/label */
  label: string;
  /** Unit type for formatting */
  unit?: 'bytes' | 'number' | 'percentage' | 'hours' | 'requests';
  /** Size variant */
  size?: 'sm' | 'md' | 'lg';
  /** Additional class names */
  className?: string;
  /** Optional overage cost in USD; rendered below the limit line when used>limit */
  overageCostUsd?: number;
}

export function UsageGauge({
  used,
  limit,
  label,
  unit = 'number',
  size = 'md',
  className,
  overageCostUsd,
}: UsageGaugeProps) {
  const sizeMap = {
    sm: { px: 80, stroke: 6 },
    md: { px: 120, stroke: 8 },
    lg: { px: 160, stroke: 10 },
  };

  const { px, stroke } = sizeMap[size];

  const formatUsedValue = (val: number, max: number): string => {
    switch (unit) {
      case 'bytes':
        return formatBytes(val);
      case 'hours':
        return `${val.toFixed(1)}h`;
      case 'requests':
        return formatNumber(val);
      case 'percentage':
        return `${Math.round((val / max) * 100)}%`;
      default:
        return formatNumber(val);
    }
  };

  return (
    <div className={cn('flex flex-col items-center gap-1', className)}>
      <CircularGauge
        value={used}
        max={limit}
        size={px}
        strokeWidth={stroke}
        formatValue={formatUsedValue}
        variant="auto"
        showPercentage={false}
      />
      <span className="text-xs text-muted-foreground font-medium">{label}</span>
      {(() => {
        // Show real percentage when over limit instead of clamping at 100%.
        // 100% red rings hide actual overage like 1999% Build Minutes
        // (9999/500), which obscures genuine billing surprises (audit
        // 2026-04-29 found $310/mo overage invisible behind clamped bars).
        const isOver = limit > 0 && used > limit;
        const realPct = limit > 0 ? Math.round((used / limit) * 100) : 0;
        if (isOver) {
          return (
            <>
              <span className="text-[10px] font-mono font-semibold text-red-500 dark:text-red-400">
                {realPct}% — {(unit === 'bytes' ? formatBytes(used - limit) : formatNumber(used - limit))} over {(unit === 'bytes' ? formatBytes(limit) : formatNumber(limit))}
              </span>
              {overageCostUsd != null && overageCostUsd > 0 && (
                <span className="text-[10px] font-mono text-red-500 dark:text-red-400">
                  +${overageCostUsd.toFixed(2)} this period
                </span>
              )}
            </>
          );
        }
        return (
          <span className="text-[10px] text-muted-foreground/70">
            {unit === 'bytes' ? formatBytes(limit) : formatNumber(limit)} limit
          </span>
        );
      })()}
    </div>
  );
}

// =============================================================================
// GAUGE GRID (For dashboard overview)
// =============================================================================

interface GaugeGridProps {
  children: React.ReactNode;
  columns?: 2 | 3 | 4 | 5;
  className?: string;
}

export function GaugeGrid({ children, columns = 4, className }: GaugeGridProps) {
  const gridClasses = {
    2: 'grid-cols-2',
    3: 'grid-cols-3',
    4: 'grid-cols-2 sm:grid-cols-4',
    5: 'grid-cols-2 sm:grid-cols-3 lg:grid-cols-5',
  };

  return (
    <div className={cn('grid gap-6', gridClasses[columns], className)}>{children}</div>
  );
}

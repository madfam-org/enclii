'use client';

import * as React from 'react';
import Image from 'next/image';
import { cn } from '@/lib/utils';
import { GeometricSeed, InitialsAvatar } from './GeometricSeed';

// =============================================================================
// AUTHOR AVATAR - Smart Avatar with Multiple Fallback Strategies
//
// Priority Order:
// 1. Provided avatarUrl (from API/GitHub webhook data)
// 2. GitHub avatar (if username looks like GitHub handle)
// 3. Gravatar (if email provided)
// 4. GeometricSeed (deterministic pattern based on identifier)
// 5. InitialsAvatar (text-based fallback)
// =============================================================================

interface AuthorAvatarProps {
  /** Display name of the author */
  name: string;
  /** GitHub username (optional) */
  username?: string;
  /** Email address for Gravatar (optional) */
  email?: string;
  /** Direct avatar URL (highest priority) */
  avatarUrl?: string;
  /** Size in pixels */
  size?: 'xs' | 'sm' | 'md' | 'lg';
  /** Show name alongside avatar */
  showName?: boolean;
  /** Link to GitHub profile */
  linkToProfile?: boolean;
  /** Additional class names */
  className?: string;
}

const SIZE_MAP = {
  xs: 20,
  sm: 24,
  md: 32,
  lg: 40,
};

const TEXT_SIZE_MAP = {
  xs: 'text-[10px]',
  sm: 'text-xs',
  md: 'text-sm',
  lg: 'text-sm',
};

/**
 * Generate MD5 hash for Gravatar
 * Note: In production, you'd want a proper MD5 implementation
 * This is a simplified version for demo purposes
 */
function simpleHash(str: string): string {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    const char = str.charCodeAt(i);
    hash = ((hash << 5) - hash) + char;
    hash = hash & hash;
  }
  return Math.abs(hash).toString(16).padStart(32, '0');
}

function getGravatarUrl(email: string, size: number): string {
  const hash = simpleHash(email.toLowerCase().trim());
  return `https://www.gravatar.com/avatar/${hash}?s=${size * 2}&d=404`;
}

function getGitHubAvatarUrl(username: string, size: number): string {
  return `https://github.com/${username}.png?size=${size * 2}`;
}

export function AuthorAvatar({
  name,
  username,
  email,
  avatarUrl,
  size = 'md',
  showName = false,
  linkToProfile = false,
  className,
}: AuthorAvatarProps) {
  const [imageError, setImageError] = React.useState(false);
  const [currentSrc, setCurrentSrc] = React.useState<string | null>(null);
  const pixelSize = SIZE_MAP[size];

  // Determine the best avatar source
  React.useEffect(() => {
    setImageError(false);

    if (avatarUrl) {
      setCurrentSrc(avatarUrl);
    } else if (username) {
      setCurrentSrc(getGitHubAvatarUrl(username, pixelSize));
    } else if (email) {
      setCurrentSrc(getGravatarUrl(email, pixelSize));
    } else {
      setCurrentSrc(null);
    }
  }, [avatarUrl, username, email, pixelSize]);

  const handleImageError = () => {
    // If current source failed, try next fallback
    if (currentSrc === avatarUrl && username) {
      setCurrentSrc(getGitHubAvatarUrl(username, pixelSize));
    } else if (currentSrc?.includes('github.com') && email) {
      setCurrentSrc(getGravatarUrl(email, pixelSize));
    } else {
      setImageError(true);
    }
  };

  const avatarContent = (
    <div
      className={cn(
        'author-avatar flex-shrink-0',
        'ring-1 ring-border/50',
        className
      )}
      style={{ width: pixelSize, height: pixelSize }}
    >
      {currentSrc && !imageError ? (
        <Image
          src={currentSrc}
          alt={`${name}'s avatar`}
          width={pixelSize}
          height={pixelSize}
          className="rounded-full object-cover"
          onError={handleImageError}
          unoptimized // External images
        />
      ) : email || username ? (
        <GeometricSeed
          seed={email || username || name}
          size={pixelSize}
          className="rounded-full"
        />
      ) : (
        <InitialsAvatar name={name} size={pixelSize} />
      )}
    </div>
  );

  const profileUrl = username ? `https://github.com/${username}` : null;

  if (showName) {
    const content = (
      <div className="inline-flex items-center gap-1.5">
        {avatarContent}
        <span className={cn(TEXT_SIZE_MAP[size], 'text-muted-foreground truncate max-w-[120px]')}>
          {name}
        </span>
      </div>
    );

    if (linkToProfile && profileUrl) {
      return (
        <a
          href={profileUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1.5 hover:text-foreground transition-colors"
        >
          {avatarContent}
          <span className={cn(TEXT_SIZE_MAP[size], 'hover:underline truncate max-w-[120px]')}>
            {name}
          </span>
        </a>
      );
    }

    return content;
  }

  if (linkToProfile && profileUrl) {
    return (
      <a
        href={profileUrl}
        target="_blank"
        rel="noopener noreferrer"
        title={`View ${name}'s GitHub profile`}
        className="inline-block hover:ring-primary transition-all"
      >
        {avatarContent}
      </a>
    );
  }

  return avatarContent;
}

// =============================================================================
// COMMIT LINK - Clickable SHA with external link to GitHub
// =============================================================================

interface CommitLinkProps {
  /** Full commit SHA */
  sha: string;
  /** Repository URL (e.g., https://github.com/org/repo) */
  repoUrl?: string;
  /** Show external link icon */
  showIcon?: boolean;
  /** Commit message for tooltip */
  message?: string;
  /** Additional class names */
  className?: string;
}

export function CommitLink({
  sha,
  repoUrl,
  showIcon = true,
  message,
  className,
}: CommitLinkProps) {
  const shortSha = sha.substring(0, 7);
  const commitUrl = repoUrl ? `${repoUrl}/commit/${sha}` : null;

  if (!commitUrl) {
    return (
      <span
        className={cn('commit-link cursor-default', className)}
        title={message || `Commit ${shortSha}`}
      >
        <CommitIcon className="h-3 w-3" />
        <span className="font-mono">{shortSha}</span>
      </span>
    );
  }

  return (
    <a
      href={commitUrl}
      target="_blank"
      rel="noopener noreferrer"
      className={cn('commit-link group', className)}
      title={message || `View commit ${shortSha} on GitHub`}
    >
      <CommitIcon className="h-3 w-3" />
      <span className="font-mono">{shortSha}</span>
      {showIcon && (
        <ExternalLinkIcon className="h-2.5 w-2.5 opacity-0 group-hover:opacity-100 transition-opacity" />
      )}
    </a>
  );
}

// =============================================================================
// ICONS (inline to avoid additional imports)
// =============================================================================

function CommitIcon({ className }: { className?: string }) {
  return (
    <svg
      aria-hidden="true"
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <circle cx="12" cy="12" r="4" />
      <line x1="1.05" y1="12" x2="7" y2="12" />
      <line x1="17.01" y1="12" x2="22.96" y2="12" />
    </svg>
  );
}

function ExternalLinkIcon({ className }: { className?: string }) {
  return (
    <svg
      aria-hidden="true"
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
      <polyline points="15 3 21 3 21 9" />
      <line x1="10" y1="14" x2="21" y2="3" />
    </svg>
  );
}

// =============================================================================
// EXPORTS
// =============================================================================

export { GeometricSeed, InitialsAvatar } from './GeometricSeed';

import * as React from 'react';
import { cn } from '@enclii/shared-lib/utils';

const STATION = String.raw`         +----------------+
        /   E N C L I I   /|
       +----------------+ |
       |  []  []  []    | |
       |     +----+     | +
       |_____|    |_____|/
  +----------+----+----------+
 /   ||   ||   ||   ||   || /
+-------------------------+`;

const TRACKS = '==|==|==|==[ + ]==|==|==|==';

export interface StationIdentityProps {
  surface: 'switchyard' | 'dispatch';
  variant?: 'hero' | 'compact';
  className?: string;
}

/** Enclii's station/enclave identity. ASCII is decorative, never live telemetry. */
export function StationIdentity({ surface, variant = 'hero', className }: StationIdentityProps) {
  const dispatch = surface === 'dispatch';

  if (variant === 'compact') {
    return (
      <span className={cn('inline-flex min-w-0 items-center gap-2', className)}>
        <span aria-hidden="true" className="shrink-0 rounded border border-primary/30 bg-primary/5 px-1.5 py-1 font-mono text-xs leading-none text-primary">
          [=]
        </span>
        <span className="min-w-0 font-mono text-sm font-semibold tracking-tight text-foreground">
          {dispatch ? 'ENCLII ADMIN' : 'Enclii'}
          <span className="ml-2 hidden font-sans text-xs font-normal text-muted-foreground sm:inline">
            {dispatch ? 'Dispatch' : 'Switchyard'}
          </span>
        </span>
      </span>
    );
  }

  return (
    <div className={cn('space-y-5 text-center', className)}>
      <div>
        <p className="mb-2 font-mono text-[10px] uppercase tracking-[0.25em] text-muted-foreground">
          Enclii / {dispatch ? 'Dispatch' : 'Switchyard'}
        </p>
        <h1 className="font-mono text-3xl font-semibold tracking-tight text-foreground">
          {dispatch ? 'DISPATCH' : 'Enclii'}
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {dispatch ? 'Infrastructure Control Tower' : 'Switchyard Platform'}
        </p>
      </div>
      <div className="rounded-xl border border-border bg-muted/30 px-3 py-5">
        <pre aria-hidden="true" className="mx-auto w-fit select-none whitespace-pre font-mono text-[10px] leading-[1.35] text-primary sm:text-xs">
          {STATION}
        </pre>
        <div aria-hidden="true" className="mt-4 border-t border-border pt-3">
          <pre className="select-none whitespace-pre font-mono text-[10px] tracking-wider text-muted-foreground">
            {TRACKS}
          </pre>
          <p className="mt-2 font-mono text-[9px] uppercase tracking-[0.2em] text-muted-foreground">
            Station / Enclave
          </p>
        </div>
      </div>
    </div>
  );
}

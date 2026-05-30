'use client'

import { useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@enclii/ui-components/dialog'
import { Button } from '@enclii/ui-components/button'
import { Input } from '@enclii/ui-components/input'
import { Label } from '@enclii/ui-components/label'
import { Loader2, CheckCircle2, AlertTriangle } from 'lucide-react'
import { providerApi, opsApi } from '@/lib/provider-api'
import type { OperatorResponse } from '@/types/providers'

type OperationPlanDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  provider: string
  action: string
  /** When set to ops, calls /api/ops instead of /api/providers */
  contract?: 'providers' | 'ops'
  requestBody: Record<string, unknown>
  onSuccess?: (resp: OperatorResponse) => void
}

export function OperationPlanDialog({
  open,
  onOpenChange,
  title,
  description,
  provider,
  action,
  contract = 'providers',
  requestBody,
  onSuccess,
}: OperationPlanDialogProps) {
  const [phase, setPhase] = useState<'idle' | 'planning' | 'planned' | 'applying' | 'done'>('idle')
  const [plan, setPlan] = useState<OperatorResponse | null>(null)
  const [result, setResult] = useState<OperatorResponse | null>(null)
  const [reason, setReason] = useState('')
  const [error, setError] = useState<string | null>(null)

  const reset = () => {
    setPhase('idle')
    setPlan(null)
    setResult(null)
    setReason('')
    setError(null)
  }

  const handleOpenChange = (next: boolean) => {
    if (!next) reset()
    onOpenChange(next)
  }

  const call = (body: Record<string, unknown>) =>
    contract === 'ops'
      ? opsApi.operation(provider, action, body)
      : providerApi.operation(provider, action, body)

  const runPlan = async () => {
    setPhase('planning')
    setError(null)
    try {
      const resp = await call({
        dry_run: true,
        ...requestBody,
      })
      setPlan(resp)
      setPhase('planned')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Plan failed')
      setPhase('idle')
    }
  }

  const runApply = async () => {
    if (!reason.trim()) {
      setError('Reason is required for apply')
      return
    }
    setPhase('applying')
    setError(null)
    try {
      const resp = await call({
        dry_run: false,
        reason: reason.trim(),
        ...requestBody,
      })
      setResult(resp)
      setPhase('done')
      onSuccess?.(resp)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Apply failed')
      setPhase('planned')
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-lg bg-card border-border">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>

        {error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
            {error}
          </div>
        )}

        {phase === 'idle' && (
          <p className="text-sm text-muted-foreground">
            Run a dry-run plan first. Review steps, then apply with an audit reason.
          </p>
        )}

        {(phase === 'planned' || phase === 'applying' || phase === 'done') && plan && (
          <div className="space-y-3 text-sm">
            <p className="font-medium">{plan.summary}</p>
            {plan.steps?.map((step) => (
              <div key={step.name} className="flex gap-2 text-muted-foreground">
                <span className="font-mono text-xs uppercase">{step.name}</span>
                <span>{step.detail || step.status}</span>
              </div>
            ))}
            {plan.warnings?.map((w) => (
              <p key={w} className="text-amber-500 flex items-center gap-1">
                <AlertTriangle className="size-3" /> {w}
              </p>
            ))}
          </div>
        )}

        {phase === 'done' && result && (
          <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm">
            <div className="flex items-center gap-2 text-emerald-400">
              <CheckCircle2 className="size-4" />
              {result.summary}
            </div>
            {result.operation_id && (
              <p className="mt-1 font-mono text-xs text-muted-foreground">
                operation_id: {result.operation_id}
              </p>
            )}
          </div>
        )}

        {(phase === 'planned' || phase === 'applying') && (
          <div className="space-y-2">
            <Label htmlFor="reason">Audit reason</Label>
            <Input
              id="reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Why is this change needed?"
            />
          </div>
        )}

        <DialogFooter className="gap-2">
          {phase === 'idle' && (
            <Button onClick={runPlan} className="gap-2">
              Dry-run plan
            </Button>
          )}
          {(phase === 'planned' || phase === 'applying') && (
            <Button onClick={runApply} disabled={phase === 'applying' || !reason.trim()} className="gap-2">
              {phase === 'applying' && <Loader2 className="size-4 animate-spin" />}
              Apply
            </Button>
          )}
          {phase === 'done' && (
            <Button onClick={() => handleOpenChange(false)}>Close</Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

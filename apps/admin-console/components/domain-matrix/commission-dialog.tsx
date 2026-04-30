'use client'

import { useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@enclii/ui-components/dialog"
import { Button } from "@enclii/ui-components/button"
import { Input } from "@enclii/ui-components/input"
import { Label } from "@enclii/ui-components/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@enclii/ui-components/select"
import { Globe, Copy, CheckCircle2, Loader2, ArrowRight } from 'lucide-react'
import { copyToClipboard } from '@/lib/utils'
import type { CommissionDomainResponse, EcosystemTenant } from '@/types/cloudflare'

interface CommissionDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}

type CommissionStep = 'input' | 'processing' | 'success'

export function CommissionDialog({ open, onOpenChange, onSuccess }: CommissionDialogProps) {
  const [step, setStep] = useState<CommissionStep>('input')
  const [domain, setDomain] = useState('')
  const [tenant, setTenant] = useState<EcosystemTenant>('other')
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<CommissionDomainResponse | null>(null)
  const [copiedNs, setCopiedNs] = useState(false)

  const handleSubmit = async () => {
    if (!domain.trim()) {
      setError('Domain is required')
      return
    }

    setStep('processing')
    setError(null)

    try {
      const response = await fetch('/api/domains', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ domain: domain.trim(), tenant }),
      })

      const data = await response.json()

      if (data.success) {
        setResult(data.data)
        setStep('success')
      } else {
        setError(data.error || 'Failed to commission domain')
        setStep('input')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to commission domain')
      setStep('input')
    }
  }

  const handleCopyNameservers = async () => {
    if (result?.nameservers) {
      await copyToClipboard(result.nameservers.join('\n'))
      setCopiedNs(true)
      setTimeout(() => setCopiedNs(false), 2000)
    }
  }

  const handleClose = () => {
    if (step === 'success') {
      onSuccess()
    }
    setStep('input')
    setDomain('')
    setTenant('other')
    setError(null)
    setResult(null)
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-lg bg-card border-border">
        <DialogHeader>
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-primary/10 border border-primary/20">
              <Globe className="size-5 text-primary" />
            </div>
            <div>
              <DialogTitle className="text-foreground">Commission Domain</DialogTitle>
              <DialogDescription className="text-muted-foreground">
                {step === 'input' && 'Add a new domain to the ecosystem'}
                {step === 'processing' && 'Creating zone in Cloudflare...'}
                {step === 'success' && 'Domain commissioned successfully'}
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>

        {/* Input Step */}
        {step === 'input' && (
          <>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="domain">Domain Name</Label>
                <Input
                  id="domain"
                  placeholder="example.com"
                  value={domain}
                  onChange={(e) => setDomain(e.target.value)}
                  className="font-mono bg-background"
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="tenant">Tenant</Label>
                <Select value={tenant} onValueChange={(v) => setTenant(v as EcosystemTenant)}>
                  <SelectTrigger className="bg-background">
                    <SelectValue placeholder="Select tenant" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="madfam">MADFAM</SelectItem>
                    <SelectItem value="suluna">SuLuna</SelectItem>
                    <SelectItem value="primavera">Primavera</SelectItem>
                    <SelectItem value="janua">Janua</SelectItem>
                    <SelectItem value="enclii">Enclii</SelectItem>
                    <SelectItem value="other">Other</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              {error && (
                <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-3">
                  <p className="text-sm text-destructive">{error}</p>
                </div>
              )}
            </div>

            <DialogFooter>
              <Button variant="outline" onClick={handleClose}>
                Cancel
              </Button>
              <Button onClick={handleSubmit} className="gap-2">
                Commission
                <ArrowRight className="size-4" />
              </Button>
            </DialogFooter>
          </>
        )}

        {/* Processing Step */}
        {step === 'processing' && (
          <div className="py-12 flex flex-col items-center justify-center gap-4">
            <Loader2 className="size-8 text-primary animate-spin" />
            <div className="text-center">
              <p className="font-medium text-foreground">Creating Cloudflare Zone</p>
              <p className="text-sm text-muted-foreground">{domain}</p>
            </div>
          </div>
        )}

        {/* Success Step */}
        {step === 'success' && result && (
          <>
            <div className="space-y-4 py-4">
              <div className="flex items-center gap-3 p-3 rounded-lg bg-status-success-muted border border-status-success/20">
                <CheckCircle2 className="size-5 text-status-success" />
                <div>
                  <p className="font-medium text-foreground">Zone Created</p>
                  <p className="text-sm text-muted-foreground">{result.zone.name}</p>
                </div>
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label>Nameservers</Label>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={handleCopyNameservers}
                    className="gap-2 text-xs"
                  >
                    {copiedNs ? (
                      <CheckCircle2 className="size-3 text-status-success" />
                    ) : (
                      <Copy className="size-3" />
                    )}
                    {copiedNs ? 'Copied!' : 'Copy All'}
                  </Button>
                </div>
                <div className="rounded-lg border border-border bg-background p-3 space-y-1">
                  {result.nameservers.map((ns, i) => (
                    <div key={i} className="font-mono text-sm text-primary">
                      {ns}
                    </div>
                  ))}
                </div>
              </div>

              <div className="space-y-2">
                <Label>Next Steps</Label>
                <div className="rounded-lg border border-border bg-background p-3 space-y-2 text-sm text-muted-foreground">
                  {result.instructions.map((instruction, i) => (
                    <p key={i}>{instruction}</p>
                  ))}
                </div>
              </div>
            </div>

            <DialogFooter>
              <Button onClick={handleClose}>Done</Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

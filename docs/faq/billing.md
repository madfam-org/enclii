---
title: Billing FAQ
description: Pricing, costs, and billing questions
sidebar_position: 3
tags: [faq, billing, pricing, cost]
---

# Billing & Pricing FAQ

Questions about Enclii costs, pricing structure, and billing.

## Pricing Model

### How much does Enclii cost?

Enclii runs on fixed-cost infrastructure rather than usage-based billing. Current infrastructure costs:

See internal-devops for cost breakdown. Enclii runs on fixed-cost self-hosted infrastructure (Hetzner dedicated server + Cloudflare) at significant savings vs traditional SaaS stacks (Railway, Auth0, etc.).

### What's included in the base cost?

- **Compute**: Shared access to dedicated server resources
- **Storage**: Persistent volumes for databases and files
- **Networking**: Unlimited bandwidth (fair use policy)
- **SSL Certificates**: Automatic Let's Encrypt certificates
- **Custom Domains**: Unlimited custom domains
- **Build Pipeline**: Unlimited builds
- **Logs & Metrics**: Built-in observability

### Are there any hidden fees?

No hidden fees. The only additional costs might be:
- **Egress from R2**: First 10GB/month free, then $0.015/GB
- **Premium support**: Available for enterprise customers
- **Additional regions**: Custom pricing

### Is there a free tier?

Enclii is designed for organizations rather than individual hobbyists. We don't offer a free tier, but:
- No minimum commitment
- Pay only for infrastructure used
- Scale up or down as needed

Contact us for startup pricing if you're early-stage.

## Cost Comparison

### How does Enclii compare to Railway?

**Railway pricing example** (typical startup):
- 2 services × $20/month = $40
- Database: $20/month
- Bandwidth: $50/month (variable)
- Build minutes: $30/month
- **Total: ~$140-200/month** (small scale)
- At scale: $2,000+/month

**Enclii**: Fixed-cost self-hosted infrastructure, regardless of number of services. See internal-devops for cost breakdown.

### How does Enclii compare to Vercel?

Vercel is optimized for frontend/edge. Enclii is for full-stack. Enclii makes sense when you need backend services, databases, or have multiple apps, with significant cost advantages at scale.

### What about long-term savings?

Enclii's self-hosted model provides significant savings over traditional SaaS stacks (Railway + Auth0) over multi-year periods. See internal-devops for detailed cost analysis.

## Subscription Tiers

Enclii offers the following tiers via [Dhanam](https://app.dhan.am) billing:

| Tier | Price | Projects | Services | Description |
|------|-------|----------|----------|-------------|
| Community | Free | 1 | 3 | Self-hosted open-source users |
| Essentials (**Sovereign** on landing) | $20/mo | 1 | 3 | Managed service with support — public marketing name **Sovereign** |
| Pro | $49/mo | 10 | Unlimited | Premium features, priority support |
| MADFAM Bundle | TBD | Unlimited | Unlimited | Ecosystem bundle (coming soon) |

**Sovereign = Essentials:** The [enclii.dev](https://enclii.dev) landing uses **Sovereign** at $20/mo for the same managed tier documented here as Essentials (1 project, 3 services, Dhanam checkout). Use this table as canonical for GA.

Community and Essentials have identical feature limits — the value of Essentials/Sovereign is the **managed service** (hosting, uptime SLA, support, backups). Only Pro and above unlock additional feature limits.

Upgrade via Dhanam checkout: `https://app.dhan.am/checkout?plan=enclii_pro&product=enclii`

## Billing Details

### How am I billed?

Currently, Enclii infrastructure is managed directly:
- Pay for your own Hetzner server
- Or share resources on MADFAM infrastructure

Subscription billing is handled through [Dhanam](https://app.dhan.am), our billing platform.

### Can I bring my own infrastructure?

Yes! Enclii can run on:
- Your own Hetzner server
- Any Kubernetes cluster
- Cloud providers (AWS EKS, GCP GKE, etc.)

You control the infrastructure, we provide the platform layer.

### What if I exceed resource limits?

Unlike usage-based platforms, there are no surprise bills. If you hit resource limits:
1. You'll see performance degradation
2. We'll notify you
3. You can upgrade your infrastructure
4. No automatic overage charges

### Are there per-user costs?

No per-seat pricing. Your entire team can use Enclii at no additional cost:
- Unlimited users
- Unlimited projects
- Unlimited services
- Unlimited deployments

## Resource Allocation

### How are resources shared?

On shared infrastructure, resources are allocated via Kubernetes:

| Resource | Guaranteed | Burstable |
|----------|-----------|-----------|
| CPU | 100m (0.1 core) | Up to available |
| Memory | 128Mi | Up to limit |
| Storage | Per PVC | - |

You can request higher allocations for production workloads.

### What if I need dedicated resources?

Options for dedicated resources:
1. **Dedicated namespace**: Isolated from other tenants
2. **Dedicated node**: Your own server in our cluster
3. **Dedicated cluster**: Complete isolation

Contact us for dedicated pricing.

### Can I set spending limits?

Yes. Configure alerts and limits per project:

```yaml
# Service configuration
budget:
  monthlyLimit: 100  # Alert at $100/month
  hardLimit: 150     # Throttle at $150/month
```

Budgets apply to resource usage metrics, not actual billing.

## Enterprise

### Is there enterprise pricing?

Yes. Enterprise features include:
- Dedicated infrastructure
- SLA guarantees (99.95%+)
- Priority support
- Custom integrations
- Compliance certifications
- On-premises deployment option

Contact sales@enclii.dev for enterprise pricing.

### Do you offer volume discounts?

For organizations running multiple environments or large workloads:
- Custom infrastructure sizing
- Bulk pricing for multi-year commitments
- Partner discounts for agencies/consultancies

### Can I get an invoice?

Enterprise customers receive proper invoicing with:
- Net-30 payment terms
- PO/requisition support
- Custom billing cycles

## Refunds & Cancellation

### What's your refund policy?

Since infrastructure is managed directly:
- No contracts or commitments
- Cancel anytime
- Pay only for what you use
- No cancellation fees

### How do I cancel?

1. Export your data (see migration docs)
2. Delete your projects
3. Contact support to offboard
4. Stop paying for infrastructure

We provide a full offboarding guide to ensure smooth transitions.

## Related Documentation

- **Cost Analysis**: [Production Deployment Roadmap](/production/PRODUCTION_DEPLOYMENT_ROADMAP)
- **Infrastructure**: [Infrastructure Anatomy](/infrastructure/INFRA_ANATOMY)
- **Migration**: [Migration FAQ](/faq/migration)
- **Security**: [Security FAQ](/faq/security)

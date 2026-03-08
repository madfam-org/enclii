# Enclii Blue Ocean Features Roadmap

## 🌊 Strategic Positioning: "Operational Sovereignty Platform"

**Market Gap**: Startups hit "Compliance Panic" when pursuing SOC 2 certification and discover their PaaS (Heroku, Railway, Render) lacks compliance features. Enterprise alternatives (Qovery, Porter) gate these features behind expensive tiers.

**Enclii's Blue Ocean**: **Ungated compliance features at every tier** + **Provenance tracking** (unique!)

---

## 📊 Current vs Target State

### Current Positioning (Before)
```
Enclii = Heroku Alternative
Competes with: Railway, Render, Fly.io
Differentiation: None
Win Rate: Low (price-driven commodity)
```

### Target Positioning (After Blue Ocean Features)
```
Enclii Switchyard = Operational Sovereignty Platform
Competes with: Qovery, Porter, Flightcontrol
Differentiation: Ungated compliance + Provenance tracking
Win Rate: High (compliance-driven, Series B scale-ups)
Target: Companies 3-6 months before SOC 2 audit
```

---

## 🏆 Competitive Feature Matrix

| Feature | Enclii (Now) | Enclii (Target) | Qovery | Porter | Flightcontrol |
|---------|--------------|-----------------|--------|--------|---------------|
| **SSO (SAML/OIDC)** | ✅ All tiers | ✅ All tiers | 💰 Enterprise | 💰 Enterprise | 💰 Contact Sales |
| **Granular RBAC** | ✅ Environment-level | ✅ Environment-level | 💰 Enterprise | ❌ Basic | 💰 Enterprise |
| **Audit Logging** | ✅ Immutable | ✅ Immutable | 💰 Enterprise | ❌ Basic | 💰 Contact Sales |
| **Build Pipeline** | ✅ Complete | ✅ Complete | ✅ Yes | ✅ Yes | ✅ Yes |
| **SBOM Generation** | ⏳ Week 3 | ✅ All tiers | ❌ None | ❌ None | ❌ None |
| **Image Signing** | ⏳ Week 3 | ✅ All tiers | ❌ None | ❌ None | ❌ None |
| **🌊 PR Approval Tracking** | ❌ None | ✅ All tiers | ❌ None | ❌ None | ❌ None |
| **🌊 Compliance Webhooks** | ❌ None | ✅ Vanta/Drata | ❌ None | ❌ None | ❌ None |
| **🌊 Secret Rotation** | ⚠️ Manual | ✅ Zero-downtime | ⚠️ Manual | ⚠️ Manual | ⚠️ Manual |
| **🌊 Subway Map View** | ❌ List | ✅ Railroad theme | ✅ Generic graph | ❌ List | ✅ Generic graph |

**Legend**:
- ✅ Available
- ❌ Not available
- 💰 Gated behind expensive "Enterprise" tier
- ⚠️ Available but manual/requires downtime
- 🌊 **Blue Ocean** (No competitor has this!)

---

## 🌊 Blue Ocean Features (The Moat)

### **1. PR Approval Tracking** (Roundhouse Provenance)
**Status**: 🔴 Not Started
**Effort**: 4-5 days
**Priority**: P0 (Highest differentiator)
**Competitive Advantage**: **UNIQUE - No competitor has this!**

#### What It Does
Enforces "no unapproved code in production" by verifying GitHub PR approvals before deployment.

#### Why It Matters
- **Compliance**: SOC 2 CC8.1 requires "monitoring for security events"
- **Safety**: Prevents accidental/malicious deployments
- **Audit Trail**: Full provenance from code review → deployment
- **Differentiation**: This is table-stakes for regulated industries, but no PaaS offers it

#### Implementation
```go
// Before production deployment
func (h *Handler) DeployService(c *gin.Context) {
    // 1. Get release and git SHA
    release := getReleaseFromContext(c)

    // 2. Check PR approval (NEW!)
    prStatus := h.provenance.CheckPRApproval(release.GitRepo, release.GitSHA)

    if !prStatus.Approved {
        return gin.H{
            "error": "Deployment blocked: PR not approved",
            "pr_url": prStatus.PRURL,
            "policy": "Production deployments require PR approval",
        }
    }

    // 3. Store approver in deployment record
    deployment.ApprovedBy = prStatus.Approver
    deployment.ApprovedAt = prStatus.ApprovedAt
    deployment.PRURL = prStatus.PRURL
    deployment.CIStatus = prStatus.CIStatus

    // 4. Generate signed receipt for auditors
    deployment.ComplianceReceipt = h.provenance.SignReceipt(deployment)

    // 5. Proceed with deployment
    h.k8sClient.Deploy(deployment)
}
```

#### Database Schema (Already Exists!)
```sql
-- From 002_compliance_schema.up.sql
CREATE TABLE approval_records (
    id UUID PRIMARY KEY,
    deployment_id UUID REFERENCES deployments(id),
    pr_url VARCHAR(500),
    pr_number INTEGER,
    approver_email VARCHAR(255),
    approver_name VARCHAR(255),
    approved_at TIMESTAMP,
    ci_status VARCHAR(50),
    change_ticket_url VARCHAR(500),
    compliance_receipt TEXT  -- Cryptographically signed proof
);
```

#### Files to Create
- `internal/provenance/github.go` - GitHub API client
- `internal/provenance/checker.go` - PR approval verification
- `internal/provenance/policy.go` - Deployment policies
- `internal/provenance/receipt.go` - Signed compliance receipts

#### Testing Strategy
```bash
# Test 1: Deploy without PR approval (should block)
curl -X POST /v1/services/$ID/deploy
# Expected: 403 Forbidden - "PR not approved"

# Test 2: Deploy with approved PR (should succeed)
# 1. Create PR on GitHub
# 2. Get approval from reviewer
# 3. Merge PR
# 4. Deploy
# Expected: 200 OK with approver info in deployment record

# Test 3: Verify compliance receipt
curl /v1/deployments/$ID
# Expected: Response includes signed compliance receipt with:
# - PR URL
# - Approver email
# - Approval timestamp
# - CI check status
# - Cryptographic signature
```

---

### **2. Vanta/Drata Compliance Webhooks**
**Status**: 🔴 Not Started
**Effort**: 2-3 days
**Priority**: P1 (High value, low effort)
**Competitive Advantage**: **FIRST-MOVER - Saves customers hours of manual work**

#### What It Does
Automatically sends deployment evidence to Vanta/Drata, eliminating manual screenshot collection.

#### Why It Matters
- **Pain Point**: Compliance managers manually screenshot every deployment
- **Time Savings**: Eliminates 2-4 hours/week of manual work
- **Accuracy**: No missed deployments or incorrect timestamps
- **Differentiation**: Makes SOC 2 audits 10x easier

#### Implementation
```go
// After successful deployment
func (h *Handler) onDeploymentComplete(deployment *types.Deployment) {
    if !h.config.ComplianceWebhooksEnabled {
        return
    }

    // Generate compliance receipt
    receipt := ComplianceReceipt{
        EventType:    "deployment",
        Timestamp:    deployment.CreatedAt,
        Service:      deployment.Service.Name,
        Environment:  "production",
        GitSHA:       deployment.Release.GitSHA,
        ImageURI:     deployment.Release.ImageURI,
        DeployedBy:   deployment.DeployedBy.Email,
        ApprovedBy:   deployment.ApprovedBy.Email,
        PRURL:        deployment.PRURL,
        ChangeTicket: deployment.ChangeTicketURL,
        Signature:    crypto.Sign(deployment),  // Tamper-proof
    }

    // Send to Vanta
    if h.config.VantaWebhook != "" {
        h.compliance.SendToVanta(receipt)
    }

    // Send to Drata
    if h.config.DrataWebhook != "" {
        h.compliance.SendToDrata(receipt)
    }
}
```

#### Configuration
```bash
# .env
COMPLIANCE_WEBHOOKS_ENABLED=true
VANTA_WEBHOOK_URL=https://api.vanta.com/webhooks/...
DRATA_WEBHOOK_URL=https://api.drata.com/webhooks/...
```

#### Files to Create
- `internal/compliance/exporter.go` - Generic webhook sender
- `internal/compliance/vanta.go` - Vanta-specific formatting
- `internal/compliance/drata.go` - Drata-specific formatting
- `internal/compliance/signer.go` - Cryptographic signatures

#### Marketing Message
> "Stop screenshotting deployments. Enclii automatically sends compliance evidence to Vanta/Drata with cryptographic proof."

---

### **3. Zero-Downtime Secret Rotation**
**Status**: 🔴 Not Started
**Effort**: 3-4 days
**Priority**: P1 (Security best practice + competitive edge)
**Competitive Advantage**: **BETTER - Competitors require downtime**

#### What It Does
Rotates secrets (database passwords, API keys) without downtime using dual-write strategy.

#### Why It Matters
- **Security**: SOC 2 CC6.6 requires periodic credential rotation
- **Safety**: Rolling restart with health checks prevents outages
- **Differentiation**: Competitors require manual downtime windows

#### Implementation Strategy
```go
// Dual-write secret rotation
func (s *SecretManager) RotateSecret(secretKey, newValue string) error {
    // Phase 1: Inject NEW secret alongside OLD secret
    // Apps read REDIS_PASSWORD or REDIS_PASSWORD_NEW
    s.k8s.InjectSecret(secretKey+"_NEW", newValue)

    // Phase 2: Rolling restart pods (one at a time)
    for _, pod := range s.k8s.GetPods(serviceID) {
        s.k8s.DeletePod(pod.Name)

        // Wait for pod health check
        if !s.k8s.WaitForHealthy(pod.Name, 2*time.Minute) {
            // ROLLBACK: Health check failed
            s.k8s.InjectSecret(secretKey, oldValue)
            return errors.New("rollback: health check failed")
        }
    }

    // Phase 3: All pods healthy with new secret
    s.k8s.InjectSecret(secretKey, newValue)
    s.k8s.DeleteSecret(secretKey+"_NEW")

    // Phase 4: Audit trail
    s.audit.LogSecretRotation(secretKey, time.Now())

    return nil
}
```

#### Files to Create
- `internal/secrets/rotation.go` - Rotation orchestration
- `internal/secrets/validator.go` - Health check validation
- `internal/secrets/rollback.go` - Automatic rollback logic

---

### **4. Subway Map Topology View** (Railroad Theme)
**Status**: 🔴 Not Started
**Effort**: 4-5 days
**Priority**: P2 (Visual differentiation)
**Competitive Advantage**: **UNIQUE AESTHETIC - Railroad metaphor**

#### What It Does
Visualizes service dependencies as a "railroad switchyard" with stations and tracks.

#### Why It Matters
- **Usability**: Easier to understand service architecture
- **Branding**: Reinforces "Switchyard" brand identity
- **Differentiation**: Unique visual style (competitors use generic graphs)

#### Visual Design
```
🚉 API Service          🚉 Auth Service
   │                       │
   ├──── track ────────────┤
   │                       │
   └──── track ──── 🚉 Database Service
```

#### Implementation
```tsx
// React component with React Flow
import ReactFlow from 'reactflow';

export function TopologyMap({ services, dependencies }) {
    const nodes = services.map(svc => ({
        id: svc.id,
        type: 'station',  // Custom node type
        data: {
            name: svc.name,
            health: svc.health,  // green/yellow/red signal
            version: svc.version,
            replicas: svc.replicas,
        },
        position: calculatePosition(svc),
    }));

    const edges = dependencies.map(dep => ({
        source: dep.from,
        target: dep.to,
        type: 'smoothstep',  // Orthogonal routing (like tracks)
        animated: dep.type === 'realtime',
    }));

    return (
        <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={{ station: StationNode }}
            fitView
        >
            <Background pattern="dots" />
            <Controls />
        </ReactFlow>
    );
}
```

#### Files to Create
- `apps/switchyard-ui/app/topology/page.tsx` - Main view
- `apps/switchyard-ui/components/StationNode.tsx` - Service node
- `apps/switchyard-ui/components/TrackEdge.tsx` - Dependency edge
- `apps/switchyard-ui/styles/railroad-theme.css` - Visual style

---

## 📈 Implementation Roadmap

### **Sprint 1: Provenance Engine** (5-7 days)
**Goal**: Ship PR approval tracking (biggest differentiator)

- [x] Week 1-2: Foundation (auth, audit, build) - **COMPLETE**
- [ ] Day 1-2: GitHub API integration (`internal/provenance/github.go`)
- [ ] Day 3-4: PR approval checking (`internal/provenance/checker.go`)
- [ ] Day 5: Deployment policy enforcement
- [ ] Day 6: Compliance receipt generation
- [ ] Day 7: Testing and documentation

**Deliverable**: "No unapproved code in prod" policy enforcement

---

### **Sprint 2: Compliance Automation** (3-4 days)
**Goal**: Ship Vanta/Drata webhooks (high value, low effort)

- [ ] Day 1: Webhook infrastructure (`internal/compliance/exporter.go`)
- [ ] Day 2: Vanta integration (`internal/compliance/vanta.go`)
- [ ] Day 3: Drata integration (`internal/compliance/drata.go`)
- [ ] Day 4: Testing with real Vanta/Drata accounts

**Deliverable**: Automatic compliance evidence submission

---

### **Sprint 3: Supply Chain Security** (3-4 days)
**Goal**: Complete SBOM + signing (was already planned for Week 3)

- [ ] Day 1-2: SBOM generation with Syft
- [ ] Day 3-4: Image signing with Cosign
- [ ] Testing and validation

**Deliverable**: Full supply chain provenance

---

### **Sprint 4: Secret Rotation** (3-4 days)
**Goal**: Zero-downtime secret rotation

- [ ] Day 1-2: Dual-write strategy implementation
- [ ] Day 3: Rolling restart with health checks
- [ ] Day 4: Automatic rollback + testing

**Deliverable**: Safe, automated secret rotation

---

### **Sprint 5: Visual Polish** (4-5 days)
**Goal**: Subway map topology view

- [ ] Day 1-2: React Flow integration
- [ ] Day 3-4: Railroad-themed components
- [ ] Day 5: Polish and animations

**Deliverable**: Distinctive visual identity

---

## 🎯 Prioritization Matrix

| Feature | Effort | Impact | Uniqueness | Priority |
|---------|--------|--------|------------|----------|
| **PR Approval Tracking** | Medium | **Very High** | 🌊 **UNIQUE** | **P0** |
| **Vanta/Drata Webhooks** | Low | High | 🌊 **FIRST** | **P1** |
| **SBOM + Signing** | Low | High | Competitive | P1 |
| **Secret Rotation** | Medium | Medium | **Better** | P1 |
| **Subway Map View** | Medium | Medium | Aesthetic | P2 |

---

## 💰 Go-to-Market Messaging

### Current Positioning (Generic)
> "Enclii is a deployment platform that makes it easy to ship your code."

**Problem**: Sounds like Railway/Render/Fly.io (commoditized)

### New Positioning (Blue Ocean)
> "Enclii Switchyard is the **Operational Sovereignty Platform** for Series B scale-ups. We give you SSO, audit logs, and **deployment provenance**—ungated and included—so you can pass SOC 2 **without begging for enterprise pricing**."

**Why This Works**: Targets "Compliance Panic" moment (3-6 months before audit)

### Key Messages
1. **"No Enterprise Tax"** - All compliance features included
2. **"Provenance, Not Just Logs"** - Track who approved what, when, why
3. **"Built for Auditors"** - One-click evidence export to Vanta/Drata
4. **"No Unapproved Code in Prod"** - Policy enforcement (unique!)

---

## 📊 Success Metrics

### Technical Metrics
- PR approval coverage: 0% → 100% of production deploys
- Manual compliance work: 4 hours/week → 0 hours/week
- Secret rotation downtime: 5-10 minutes → 0 minutes
- Audit preparation time: 40 hours → 4 hours

### Business Metrics
- **Target ICP**: Series B scale-ups (50-200 employees, pursuing SOC 2)
- **Win Rate vs Qovery/Porter**: Track "ungated compliance" message
- **Time-to-SOC-2**: Measure from signup to certification
- **Feature Requests**: Track if customers request features we've ungated

---

## 🚀 Quick Start (Next Steps)

**Option A: Start with Biggest Differentiator** (Recommended!)
```bash
# Sprint 1: PR Approval Tracking (5-7 days)
1. Create internal/provenance/ module
2. Integrate GitHub API
3. Add pre-deploy approval checks
4. Generate signed compliance receipts
```

**Option B: Quick Win** (Get something shipped fast)
```bash
# Sprint 2: Vanta/Drata Webhooks (2-3 days)
1. Create internal/compliance/ module
2. Add webhook infrastructure
3. Integrate with Vanta API
4. Test with real customer
```

**Option C: Complete Original Plan**
```bash
# Sprint 3: SBOM + Signing (3-4 days)
1. Integrate Syft for SBOM
2. Integrate Cosign for signing
3. Week 3 objectives complete
```

---

## 🏆 Expected Outcomes

### After Sprint 1 (PR Approval Tracking)
- **Positioning**: "Only PaaS that enforces code review before production"
- **Competitive Moat**: UNIQUE feature, 12-18 month lead
- **Customer Value**: Prevents unauthorized deployments
- **SOC 2 Impact**: Covers CC8.1 "Detect security events"

### After Sprint 2 (Compliance Webhooks)
- **Positioning**: "SOC 2 on autopilot"
- **Competitive Moat**: FIRST-MOVER, 6-12 month lead
- **Customer Value**: Eliminates 2-4 hours/week manual work
- **Sales Enablement**: "We integrate with your compliance tools"

### After All Sprints Complete
- **Market Position**: Blue Ocean (no direct competitors)
- **Pricing Power**: Premium pricing justified by unique features
- **Customer Retention**: High switching costs (compliance integrations)
- **Competitive Advantage**: 12-24 month lead on differentiated features

---

## 🔗 Related Documents

- [Architecture](./ARCHITECTURE.md) - System architecture
- Software Specification: `SOFTWARE_SPEC.md` (repo root)

---

**Last Updated**: 2025-01-19
**Status**: Ready for implementation
**Next Action**: Choose Sprint 1, 2, or 3 and begin!

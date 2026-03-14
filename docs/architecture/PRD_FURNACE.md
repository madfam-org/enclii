# PRD: Furnace - GPU Infrastructure Layer for Enclii

> **Version**: 1.0.0  
> **Status**: Draft  
> **Author**: MADFAM Engineering  
> **Created**: 2025-12-10  
> **Last Updated**: 2025-12-10

---

## Executive Summary

**Furnace** extends Enclii with GPU compute infrastructure, transforming it from a CPU-only PaaS into a full-stack platform capable of running AI/ML workloads. Furnace provides the low-level GPU scheduling, billing, and worker management that powers higher-level applications like **ceq** (our ComfyUI-based creative platform).

### Key Value Proposition

| Current State | With Furnace |
|---------------|--------------|
| CPU-only workloads | GPU + CPU workloads |
| No serverless GPU | Scale-to-zero GPU endpoints |
| Basic billing | Per-second GPU metering |
| Self-hosted infra | ~$200-500/month with GPU |

---

## Problem Statement

### Current Limitations

1. **No GPU Support**: Enclii currently runs on a dedicated server (no GPU)
2. **No Serverless Model**: All deployments are always-on, no scale-to-zero
3. **Limited Billing Granularity**: Waybill tracks GB-hours, not GPU-seconds
4. **No AI/ML Optimization**: No model caching, cold start optimization, or GPU scheduling

### Market Context

| Provider | GPU | Monthly Cost | Model |
|----------|-----|--------------|-------|
| RunPod | RTX 4090 | ~$316 (24/7) | Serverless + Pods |
| Lambda Labs | A100 | ~$929 (24/7) | VMs |
| AWS p4d | A100 | ~$23,000+ | Managed |
| **Hetzner GEX44** | RTX 4000 | **~$220** | Dedicated |

**Opportunity**: 30-40% cost savings vs RunPod for dedicated GPU workloads.

---

## Goals & Non-Goals

### Goals

1. **G1**: Add GPU node pool support to Enclii's K3s cluster
2. **G2**: Implement serverless GPU endpoints with scale-to-zero (KEDA)
3. **G3**: Extend Waybill with per-second GPU billing
4. **G4**: Provide RunPod-compatible handler SDK for easy migration
5. **G5**: Enable ceq (and other apps) to deploy GPU workloads via Enclii

### Non-Goals

- ❌ Building a public GPU cloud (internal MADFAM use only initially)
- ❌ Multi-GPU training (focus on inference and generation)
- ❌ Community cloud model (future consideration)
- ❌ Custom GPU hardware (Hetzner GEX44 only initially)

---

## Architecture

### High-Level Design

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Enclii + Furnace                             │
├─────────────────────────────────────────────────────────────────────┤
│                                                                       │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                  │
│  │ Switchyard  │  │  Waybill    │  │   Lockbox   │                  │
│  │ (Extended)  │  │ (Extended)  │  │  (Existing) │                  │
│  └─────────────┘  └─────────────┘  └─────────────┘                  │
│         │                │                                            │
│  ┌──────▼────────────────▼──────────────────────────────────────┐   │
│  │                  Furnace Components (NEW)                      │   │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐   │   │
│  │  │  Gateway  │  │ Scheduler │  │  Worker   │  │ Registry  │   │   │
│  │  │  (API)    │  │  (Queue)  │  │  Manager  │  │ (Models)  │   │   │
│  │  └───────────┘  └───────────┘  └───────────┘  └───────────┘   │   │
│  └────────────────────────────────────────────────────────────────┘   │
│                                │                                       │
│  ┌────────────────────────────▼────────────────────────────────────┐ │
│  │                     GPU Node Pool (K3s)                          │ │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐                  │ │
│  │  │  GEX44-1   │  │  GEX44-2   │  │  GEX44-N   │  ...             │ │
│  │  │ RTX 4000   │  │ RTX 4000   │  │ RTX 4000   │                  │ │
│  │  │ 20GB VRAM  │  │ 20GB VRAM  │  │ 20GB VRAM  │                  │ │
│  │  └────────────┘  └────────────┘  └────────────┘                  │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                       │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │                    Infrastructure Layer                           │ │
│  │  NVIDIA GPU Operator │ KEDA │ Redis │ PostgreSQL │ R2 Storage    │ │
│  └──────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────┘
```

### Component Breakdown

#### 1. Furnace Gateway (`apps/furnace-gateway/`)

**Technology**: Go (consistent with Switchyard)  
**Purpose**: HTTP API for serverless endpoint management

**Responsibilities**:
- Endpoint CRUD (create, update, delete serverless functions)
- Request routing to appropriate workers
- WebSocket for real-time job status
- Integration with Janua for authentication
- Rate limiting and quota enforcement

**API Endpoints**:
```
POST   /v1/endpoints           # Create serverless endpoint
GET    /v1/endpoints           # List endpoints
GET    /v1/endpoints/{id}      # Get endpoint details
DELETE /v1/endpoints/{id}      # Delete endpoint
POST   /v1/endpoints/{id}/run  # Invoke endpoint (sync)
POST   /v1/endpoints/{id}/runsync  # Invoke with wait
GET    /v1/jobs/{id}           # Get job status
GET    /v1/jobs/{id}/stream    # Stream job progress (WebSocket)
```

#### 2. Furnace Scheduler (`apps/furnace-scheduler/`)

**Technology**: Go  
**Purpose**: Job queue management and GPU scheduling

**Responsibilities**:
- Redis-based job queue (compatible with existing Enclii patterns)
- GPU-aware pod scheduling (node affinity, resource requests)
- KEDA ScaledObject management for auto-scaling
- Cold start optimization (pre-warmed container pools)
- Job state management and failure handling

**Key Features**:
- Priority queuing (premium users get faster execution)
- GPU type affinity (schedule to specific GPU types)
- Batch job support (multiple inputs, single container)
- Timeout handling and automatic retries

#### 3. Furnace Worker (`apps/furnace-worker/`)

**Technology**: Python (for ML/AI compatibility)  
**Purpose**: GPU container execution runtime

**Responsibilities**:
- Handler execution model (RunPod-compatible)
- Model loading and caching
- GPU memory management
- Health reporting and metrics

**Handler SDK Example**:
```python
# User's handler.py
import furnace

def handler(event):
    """Process a single request"""
    input_data = event["input"]
    
    # Your GPU code here
    result = run_inference(input_data)
    
    return {"output": result}

# Optional: generator for streaming
def generator_handler(event):
    for chunk in stream_inference(event["input"]):
        yield chunk

furnace.serverless.start({
    "handler": handler,
    # Optional: "generator": generator_handler
})
```

#### 4. Furnace Registry (`apps/furnace-registry/`)

**Technology**: Go  
**Purpose**: Template and model management

**Responsibilities**:
- Endpoint template storage and versioning
- Model checkpoint references (stored in R2)
- Pre-built template marketplace (internal)
- Model caching policies

---

## Technical Requirements

### TR1: GPU Node Pool Management

**Extend** `switchyard-api/internal/k8s/client.go`:

```go
// New GPU-aware deployment spec
type GPUDeploymentSpec struct {
    DeploymentSpec
    GPUType     string  // "rtx4000", "rtx4090", etc.
    GPUCount    int     // Number of GPUs requested
    VRAMMinGB   int     // Minimum VRAM required
}

// GPU node affinity
func (c *Client) DeployGPUService(ctx context.Context, spec *GPUDeploymentSpec) error {
    // Add nvidia.com/gpu resource requests
    // Add node affinity for GPU nodes
    // Add tolerations for GPU taints
}
```

**Infrastructure** (`infrastructure/k8s/gpu-operator/`):
- NVIDIA GPU Operator deployment
- NVIDIA Device Plugin DaemonSet
- GPU node labels and taints

### TR2: Serverless Scaling with KEDA

**Infrastructure** (`infrastructure/k8s/keda/`):
- KEDA Operator deployment
- ScaledObject templates for GPU workloads
- HTTP trigger for serverless endpoints

**Scaling Configuration**:
```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: furnace-worker
spec:
  scaleTargetRef:
    name: furnace-worker
  minReplicaCount: 0  # Scale to zero!
  maxReplicaCount: 10
  triggers:
    - type: redis
      metadata:
        listName: furnace:jobs:pending
        listLength: "1"
```

### TR3: Waybill GPU Billing Extension

**Extend** `waybill/internal/events/types.go`:

```go
const (
    // Existing metrics...
    
    // NEW GPU metrics
    MetricGPUSeconds        MetricType = "gpu_seconds"
    MetricGPUVRAMGBHours    MetricType = "gpu_vram_gb_hours"
    MetricModelStorageGB    MetricType = "model_storage_gb"
    MetricServerlessInvokes MetricType = "serverless_invocations"
    MetricColdStartSeconds  MetricType = "cold_start_seconds"
)

// GPU-specific event types
const (
    EventGPUAllocated    EventType = "gpu.allocated"
    EventGPUReleased     EventType = "gpu.released"
    EventServerlessStart EventType = "serverless.start"
    EventServerlessEnd   EventType = "serverless.end"
)
```

**Extend** `waybill/internal/billing/calculator.go`:

```go
type GPUPricing struct {
    GPUSecondRTX4000    float64 // $0.00006/second (~$0.22/hour)
    GPUSecondRTX4090    float64 // $0.00012/second (~$0.43/hour)
    WarmWorkerDiscount  float64 // 0.30 (30% discount for always-on)
    ModelStoragePerGB   float64 // $0.02/GB-month
    InvocationBase      float64 // $0.0001/request
}

func DefaultGPUPricing() *GPUPricing {
    return &GPUPricing{
        GPUSecondRTX4000:   0.00006,  // ~$0.22/hour
        GPUSecondRTX4090:   0.00012,  // ~$0.43/hour
        WarmWorkerDiscount: 0.30,
        ModelStoragePerGB:  0.02,
        InvocationBase:     0.0001,
    }
}
```

### TR4: Handler SDK

**Package** (`packages/furnace-handler/`):

Python SDK (RunPod-compatible):
```python
# furnace/serverless.py
class ServerlessWorker:
    def __init__(self, config: dict):
        self.handler = config.get("handler")
        self.generator = config.get("generator")
        
    def start(self):
        """Main worker loop"""
        while True:
            job = self._fetch_job()
            if job:
                self._process_job(job)
            else:
                time.sleep(0.1)
    
    def _process_job(self, job):
        try:
            if self.generator:
                for chunk in self.generator(job):
                    self._send_stream(job.id, chunk)
            else:
                result = self.handler(job)
                self._send_result(job.id, result)
        except Exception as e:
            self._send_error(job.id, str(e))

def start(config: dict):
    worker = ServerlessWorker(config)
    worker.start()
```

---

## Database Schema Extensions

### Furnace Tables

```sql
-- Serverless endpoints
CREATE TABLE furnace_endpoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id),
    name VARCHAR(255) NOT NULL,
    template_id UUID REFERENCES furnace_templates(id),
    
    -- Configuration
    gpu_type VARCHAR(50) DEFAULT 'rtx4000',
    gpu_count INT DEFAULT 1,
    timeout_seconds INT DEFAULT 300,
    max_workers INT DEFAULT 10,
    min_workers INT DEFAULT 0,  -- 0 = scale to zero
    
    -- Container
    image_uri TEXT NOT NULL,
    handler_path VARCHAR(255) DEFAULT 'handler.py',
    env_vars JSONB DEFAULT '{}',
    
    -- Status
    status VARCHAR(50) DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    
    UNIQUE(project_id, name)
);

-- Job queue (backed by Redis, persisted for audit)
CREATE TABLE furnace_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id UUID NOT NULL REFERENCES furnace_endpoints(id),
    
    -- Input/Output
    input JSONB NOT NULL,
    output JSONB,
    error TEXT,
    
    -- Timing
    queued_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    
    -- Billing
    gpu_seconds FLOAT DEFAULT 0,
    cold_start_ms INT DEFAULT 0,
    
    -- Status
    status VARCHAR(50) DEFAULT 'queued',
    worker_id VARCHAR(255),
    retry_count INT DEFAULT 0
);

-- Template registry
CREATE TABLE furnace_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    
    -- Container defaults
    base_image TEXT NOT NULL,
    default_gpu_type VARCHAR(50) DEFAULT 'rtx4000',
    default_timeout INT DEFAULT 300,
    
    -- Metadata
    category VARCHAR(100),
    tags TEXT[],
    is_public BOOLEAN DEFAULT false,
    
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Model registry
CREATE TABLE furnace_models (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    version VARCHAR(50) NOT NULL,
    
    -- Storage
    storage_uri TEXT NOT NULL,  -- R2 path
    size_bytes BIGINT NOT NULL,
    checksum VARCHAR(64) NOT NULL,
    
    -- Metadata
    model_type VARCHAR(100),  -- 'checkpoint', 'lora', 'vae', etc.
    tags TEXT[],
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    
    UNIQUE(name, version)
);
```

---

## Port Allocation

Per [solarpunk-foundry/docs/PORT_ALLOCATION.md](https://github.com/madfam-org/solarpunk-foundry/blob/main/docs/PORT_ALLOCATION.md), Furnace uses internal ports within Enclii's infrastructure:

| Port | Service | Purpose |
|------|---------|---------|
| 4210 | Furnace Gateway | Serverless endpoint API |
| 4211 | Furnace Scheduler | Job scheduling (internal) |
| 4212 | Furnace Registry | Template/model registry |
| 4215 | Furnace Metrics | Prometheus endpoint |

**Note**: Furnace ports are in the Enclii block (4200-4299) since it's an Enclii extension, not a separate service.

---

## Implementation Roadmap

### Phase 0: Infrastructure Setup (Week 1-2)

- [ ] Order Hetzner GEX44 server
- [ ] Install NVIDIA drivers and Container Toolkit
- [ ] Join GPU node to K3s cluster
- [ ] Deploy NVIDIA GPU Operator
- [ ] Validate GPU workload execution

### Phase 1: Core Components (Week 3-5)

- [ ] Implement Furnace Gateway API
- [ ] Implement Furnace Scheduler with Redis queue
- [ ] Implement basic Worker runtime
- [ ] Add Janua authentication integration
- [ ] Basic endpoint CRUD operations

### Phase 2: Serverless Features (Week 6-8)

- [ ] Deploy KEDA for autoscaling
- [ ] Implement scale-to-zero logic
- [ ] Handler SDK (Python) with RunPod compatibility
- [ ] Cold start optimization
- [ ] WebSocket for real-time status

### Phase 3: Billing Integration (Week 9-10)

- [ ] Extend Waybill with GPU metrics
- [ ] Per-second billing implementation
- [ ] Usage dashboard in Switchyard UI
- [ ] Quota enforcement

### Phase 4: Registry & Templates (Week 11-12)

- [ ] Template registry API
- [ ] Model storage integration (R2)
- [ ] Pre-built templates for common use cases
- [ ] Documentation

---

## Cost Analysis

### Hardware Costs

| Phase | Hardware | Monthly |
|-------|----------|---------|
| Dev | 1x GEX44 + existing | ~$320 |
| Prod | 2x GEX44 + existing | ~$540 |
| Scale | 3-5x GEX44 + existing | $760-1100 |

### Internal Pricing (for MADFAM apps)

Based on hardware costs + 20% margin:

| Resource | Price |
|----------|-------|
| GPU-second (RTX 4000) | $0.00006 (~$0.22/hour) |
| Warm worker (30% discount) | $0.000042/second |
| Model storage | $0.02/GB-month |
| Invocation | $0.0001/request |

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Endpoint provisioning time | < 30 seconds |
| Cold start (small container) | < 5 seconds |
| Cold start (large model) | < 30 seconds |
| GPU utilization | > 60% during active hours |
| Billing accuracy | 99.9% |
| Uptime | 99.9% |

---

## Dependencies

### Internal

- **Switchyard API**: K8s client extensions
- **Waybill**: Billing metric extensions
- **Janua**: Authentication for endpoints

### External

- **Hetzner Cloud**: GEX44 GPU servers
- **NVIDIA GPU Operator**: GPU management
- **KEDA**: Autoscaling
- **Redis**: Job queue
- **PostgreSQL**: Metadata storage
- **Cloudflare R2**: Model storage

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| GPU supply issues | High | Pre-order hardware, maintain spare capacity |
| Cold start latency | Medium | Pre-warmed containers, model caching |
| Billing accuracy | High | Comprehensive testing, audit logs |
| KEDA complexity | Medium | Start simple, iterate |

---

## Open Questions

1. **Multi-GPU Support**: Should we support multi-GPU workloads in v1?
   - **Current answer**: No, focus on single-GPU inference
   
2. **GPU Time-Slicing**: Should we enable GPU sharing?
   - **Current answer**: Evaluate after v1 based on utilization data
   
3. **External Access**: Should Furnace endpoints be accessible externally?
   - **Current answer**: Internal MADFAM use only initially

---

## Appendix

### A. NVIDIA GPU Operator Installation

```bash
# Add NVIDIA Helm repo
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia
helm repo update

# Install GPU Operator
helm install --wait gpu-operator \
  nvidia/gpu-operator \
  --namespace gpu-operator \
  --create-namespace
```

### B. KEDA Installation

```bash
# Add KEDA Helm repo
helm repo add kedacore https://kedacore.github.io/charts
helm repo update

# Install KEDA
helm install keda kedacore/keda \
  --namespace keda \
  --create-namespace
```

### C. Handler SDK Reference

See `packages/furnace-handler/README.md` for complete SDK documentation.

---

**Document Control**

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0.0 | 2025-12-10 | MADFAM Engineering | Initial draft |

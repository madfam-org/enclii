# GPU Support for Enclii

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


This directory contains Kubernetes manifests for GPU node support.

## Prerequisites

Before adding GPU nodes to the cluster:

1. **Install NVIDIA drivers** on the GPU server:
   ```bash
   # Ubuntu/Debian
   sudo apt-get install -y nvidia-driver-535

   # Verify
   nvidia-smi
   ```

2. **Install nvidia-container-toolkit**:
   ```bash
   distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
   curl -s -L https://nvidia.github.io/libnvidia-container/gpgkey | sudo apt-key add -
   curl -s -L https://nvidia.github.io/libnvidia-container/$distribution/libnvidia-container.list | \
     sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list
   sudo apt-get update
   sudo apt-get install -y nvidia-container-toolkit
   sudo nvidia-ctk runtime configure --runtime=containerd
   sudo systemctl restart containerd
   ```

3. **Join the node to k3s cluster**:
   ```bash
   curl -sfL https://get.k3s.io | K3S_URL=https://<master>:6443 K3S_TOKEN=<token> sh -
   ```

## Adding a GPU Node

1. **Label and taint the node**:
   ```bash
   kubectl label node <gpu-node> nvidia.com/gpu=present
   kubectl taint node <gpu-node> nvidia.com/gpu=present:NoSchedule
   ```

2. **Enable the NVIDIA device plugin** in `infra/k8s/production/kustomization.yaml`:
   ```yaml
   resources:
     - ../base/gpu/nvidia-device-plugin.yaml
   ```

3. **Apply the changes**:
   ```bash
   kubectl apply -k infra/k8s/production
   ```

4. **Verify GPU is visible**:
   ```bash
   kubectl describe node <gpu-node> | grep nvidia.com/gpu
   # Should show: nvidia.com/gpu: 1 (or more)
   ```

## Workload Configuration

### GPU Workloads (ML/AI builds)

Add to pod spec:
```yaml
spec:
  nodeSelector:
    nvidia.com/gpu: "present"
  tolerations:
    - key: "nvidia.com/gpu"
      operator: "Exists"
      effect: "NoSchedule"
  containers:
    - name: gpu-workload
      resources:
        limits:
          nvidia.com/gpu: 1  # Request 1 GPU
```

### Non-GPU Workloads (Web apps, APIs)

Add to pod spec to **avoid** GPU nodes:
```yaml
spec:
  affinity:
    nodeAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 100
          preference:
            matchExpressions:
              - key: nvidia.com/gpu
                operator: DoesNotExist
  tolerations: []  # Explicitly empty - won't tolerate GPU taints
```

## ARC Runners with GPU

To enable GPU builds in GitHub Actions:

1. Update `infra/helm/arc/values-runner-set.yaml`:
   ```yaml
   nodeSelector:
     nvidia.com/gpu: "present"
   tolerations:
     - key: "nvidia.com/gpu"
       operator: "Exists"
       effect: "NoSchedule"
   ```

2. Add GPU resources to the runner container:
   ```yaml
   template:
     spec:
       containers:
         - name: runner
           resources:
             limits:
               nvidia.com/gpu: 1
   ```

## Monitoring

GPU metrics are exposed via DCGM Exporter (optional):
```bash
helm repo add gpu-helm-charts https://nvidia.github.io/dcgm-exporter/helm-charts
helm install dcgm-exporter gpu-helm-charts/dcgm-exporter -n monitoring
```

Prometheus will scrape GPU metrics automatically if ServiceMonitor is configured.

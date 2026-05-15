# Koordinator LFX PoC: Kwok-Based Scheduler Scalability Benchmark

This repository contains an initial Proof of Concept (PoC) built for the CNCF Koordinator LFX Mentorship (Term 2, 2026): **End-to-End Performance and Scalability Test Harness for Scheduler**.

The goal of this prototype is to explore lightweight scheduler scalability benchmarking using `kwok` and `client-go` by generating large pod batches and measuring scheduling throughput on a simulated Kubernetes cluster.

## Objective

This PoC focuses on:

- Simulating large Kubernetes clusters efficiently using `kwok`.
- Generating concurrent scheduling workloads using `client-go`.
- Measuring scheduling completion throughput using `PodScheduled` conditions.
- Exploring API-side bottlenecks and workload generation strategies.
- Establishing an initial baseline for future scalability and regression testing.

## Benchmark Design

The benchmark currently uses:

- A local `kind` Kubernetes cluster.
- `kwok` simulated nodes.
- Concurrent pod creation using a bounded worker pool.
- Increased `client-go` QPS/Burst limits to reduce client-side throttling effects.
- Label-based workload isolation for cleaner measurements.

_Note: Scheduling completion is measured using the `PodScheduled` condition instead of the `Running` state to avoid node runtime and image startup overhead, isolating the performance of the scheduler itself._

## Current Benchmark Configuration

| Parameter             | Value      |
| :-------------------- | :--------- |
| **Simulated Nodes**   | 100        |
| **Pod Count**         | 1000       |
| **Concurrency Limit** | 50 workers |
| **`client-go` QPS**   | 100        |
| **`client-go` Burst** | 200        |

## Benchmark Results

### Iteration 1 — Sequential Baseline

Initial sequential pod creation benchmark using default `client-go` rate limits.

| Metric          | Result                                                   |
| :-------------- | :------------------------------------------------------- |
| **Throughput**  | 5.00 pods/sec                                            |
| **Observation** | Limited primarily by default client-side API throttling. |

### Iteration 2 — Concurrent Workload Generation (Current `v2_concurrent_baseline.go`)

Introduced bounded concurrency, increased QPS/Burst settings, and a parallelized pod creation workflow.

| Metric                          | Result         |
| :------------------------------ | :------------- |
| **API Creation Phase**          | 8.01 seconds   |
| **Total Scheduling Completion** | 18.85 seconds  |
| **Throughput**                  | 53.04 pods/sec |

### Iteration 3 — Percentile Tail Latency Tracking (Current `main.go`)
To properly analyze scheduling degradation under load, the PoC now extracts the exact `CreationTimestamp` and `LastTransitionTime` (for the `PodScheduled` condition) from the Kubernetes API to calculate percentile latencies.

* **P50 (Median):** 5s
* **P90 Latency:** 9s
* **P99 Tail Latency:** 10s

*Observation: While average throughput is high, tracking the P99 tail latency reveals how the scheduling queue degrades as API pressure builds (tail latency is double the median). Tracking this will be crucial for regression testing.*

## Architecture Notes

### Concurrent Workload Generation

A bounded worker pool is implemented using `sync.WaitGroup` and a buffered channel semaphore. This avoids spawning unbounded goroutines while still generating sufficient scheduling pressure on the API server.

### Workload Isolation

Benchmark pods are labeled and queried using Kubernetes labels and a `LabelSelector`. This prevents unrelated system workloads from affecting scheduling measurements.

### Scheduling Measurement

Scheduling completion is determined via `PodScheduled == True` rather than `PodRunning` to isolate scheduler placement latency from kubelet/runtime startup behavior.

---

## Environment Setup

### Prerequisites

- Go 1.21+
- Docker
- `kind`
- `kubectl`

### 1. Create Local Cluster

```bash
kind create cluster --name koordinator-test

```

### 2. Install KWOK

```bash
kubectl apply -f https://github.com/kubernetes-sigs/kwok/releases/latest/download/kwok.yaml
kubectl apply -f https://github.com/kubernetes-sigs/kwok/releases/latest/download/stage-fast.yaml

```

### 3. Generate Simulated Nodes

Example: Create 100 fake nodes. Run this loop in your terminal:

```bash
for i in {1..100}; do
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Node
metadata:
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
    kwok.x-k8s.io/node: "fake"
  labels:
    type: kwok
    kubernetes.io/hostname: kwok-node-${i}
  name: kwok-node-${i}
status:
  allocatable:
    cpu: 32
    memory: 256Gi
    pods: 110
  capacity:
    cpu: 32
    memory: 256Gi
    pods: 110
  phase: Running
EOF
done

```

## Run Benchmark

```bash
go mod tidy
go run main.go

```

### Example Output

```text
Starting benchmark: Creating 1000 pods with concurrency 50...
Finished API creation calls in 8.01s.
Waiting for all pods to be Scheduled/Running...

SUCCESS: 1000 pods scheduled!
Total Time: 18.85 seconds
Throughput: 53.04 pods/sec

```


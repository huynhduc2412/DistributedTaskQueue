# Distributed Task Queue

A high-performance, fault-tolerant, cloud-native distributed task queue system built with Go, Redis Streams, Kubernetes, and KEDA.

This system is designed to process asynchronous workloads reliably at scale with automatic retry handling, autoscaling workers, graceful shutdown, and dead-letter recovery mechanisms.

---
# Overview

This project implements a distributed background job processing system using:

- Go for high-performance concurrent workers
- Redis Streams as the message broker
- Kubernetes for orchestration
- KEDA for event-driven autoscaling

The architecture is optimized for:

- Parallel processing
- Horizontal scaling
- Crash recovery
- Retry handling
- Idempotent execution
- Cloud-native deployments

---

# Architecture

```text
                ┌──────────────────────┐
                │   Client / Producer  │
                └──────────┬───────────┘
                           │ HTTP Request
                           ▼
                ┌──────────────────────┐
                │ Ingress / API Layer  │
                │ Go HTTP + UUID       │
                │----------------------│
                │ - Validate request   │
                │ - Generate JobID     │
                │ - Store metadata     │
                └──────────┬───────────┘
                           │ Push Task
                           ▼
                ┌──────────────────────┐
                │     Redis Streams    │
                │  Broker + Queue      │
                │----------------------│
                │ - Consumer Groups    │
                │ - Persistent Stream  │
                │ - Pending Entries    │
                └──────────┬───────────┘
                           │ Pull Task
                           ▼
        ┌────────────────────────────────────┐
        │            Worker Pool             │
        │------------------------------------│
        │ Go Goroutines + automaxprocs       │
        │                                    │
        │ Worker #1  -> Process Task         │
        │ Worker #2  -> Process Task         │
        │ Worker #N  -> Parallel Execution   │
        └──────────┬─────────────────────────┘
                   │
        ┌──────────┴──────────┐
        │                     │
        ▼                     ▼
┌────────────────┐   ┌─────────────────────┐
│ Success        │   │ Failure             │
│----------------│   │---------------------│
│ ACK Stream     │   │ Retry Mechanism     │
└────────────────┘   │ Exponential Backoff │
                     └──────────┬──────────┘
                                │ max retry exceeded
                                ▼
                     ┌─────────────────────┐
                     │ Dead Letter Queue   │
                     │        (DLQ)        │
                     └─────────────────────┘
```

# Autoscaling Flow
```text
        Redis Queue Length
                 │
                 ▼
        ┌──────────────────┐
        │       KEDA       │
        │------------------│
        │ Monitor Streams  │
        │ Queue Lag        │
        │ Pending Messages │
        └────────┬─────────┘
                 │ scale decision
                 ▼
        ┌──────────────────┐
        │    Kubernetes    │
        │------------------│
        │ Scale Worker Pod │
        └──────────────────┘
```

RELIABILITY & SAFETY

1. Failure

- Each task has a unique JobID

- Workers check the processing status before running

- Avoid duplicate processing during retries/crashes

2. Retry

- Task fails → request

- Retry with exponentially backward time

3. DLQ

- Retry threshold → add to Dead Mail Queue

- Manual debugging/replay service

4. Gentle Shutdown

- Kubernetes sends SIGTERM

- Workers:

    a. Stop accepting new tasks
    
    b. End running tasks
    
    c. Task ACK
    
    d. Clean shutdown

- No task loss when restarting the group/scale down

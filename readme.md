# Sync-to-Async REST API Pattern

## Purpose – Proof of Concept

This project demonstrates a scalable architectural pattern for REST APIs that return synchronous responses to clients while handling processing asynchronously in the background.

It addresses several limitations of traditional worker-based systems with internal buffers:

- Long-running tasks block shorter ones (no reordering or prioritization)
- Worker-local queues limit burst-handling capabilities
- Graceful degradation is difficult due to isolated overflow logic
- Centralized QoS, SLA-aware routing, and retries are impractical
- Observability is fragmented and debugging becomes harder

To overcome these issues, this pattern introduces a **centralized queue** between the REST layer and workers. This decouples request handling from processing, allowing elastic scaling, fine-grained control, and robust fault tolerance — all while maintaining a synchronous API surface.

### Core Components

1. **Scalable REST API layer** – Accepts requests and holds client connections while awaiting results.
2. **Centralized queue** – Stores jobs, supports backpressure, and allows prioritization (in future iterations).
3. **Workers pool** – Consumes and processes jobs asynchronously, independent of client connections.

## Benefits

- **Buffering Under Pressure** – Queue absorbs spikes, preserving responsiveness.
- **Optimized Load Distribution** – Short tasks can bypass long ones.
- **Graceful Degradation** – Queue TTLs and timeouts preserve system health.
- **Elastic Worker Scaling** – Scale workers based on demand and queue size.
- **Fault Isolation** – REST remains functional even if workers crash.
- **Efficient Resource Use** – REST handles I/O; workers handle compute.
- **QoS-Ready** – Future support for SLA-aware queues and priorities.
- **Centralized Retry Logic** – Retries managed server-side, not by clients.
- **Loosely Coupled** – Any service can be upgraded/replaced independently.
- **Deep Observability** – Latency, throughput, and errors measurable at each stage.
- **Mixed Workloads** – Lightweight requests can return fast; heavy ones queue.
- **Simple API** – Clients interact with a traditional REST interface.

## Architecture Overview

![Sync-to-Async Flow](https://raw.githubusercontent.com/intelligent002/sync-to-async/refs/heads/main/charts/flow.png)

### Flow Summary

1. **Client** sends a request via **Balancer** to a **REST** instance.
2. **REST** pushes a job into a **Queue** and waits for a response.
3. **Worker** pops the job, processes it, and pushes the result to a dedicated queue key.
4. **REST** receives the result and responds to the **Client**.

> The system is backend-agnostic and supports queue implementations like Redis, Kafka, RabbitMQ, etc.

## Implementation Details

### REST API – Go

The REST service is built in **Go**, utilizing goroutines and non-blocking I/O to efficiently hold thousands of simultaneous connections. This enables REST instances to serve as "connection waiters" while the actual work is offloaded.

### Queue – Redis

This PoC uses **Redis Lists** for simplicity and performance. Core operations:

- `RPUSH` – REST pushes jobs to `validate:queue`
- `BLPOP` – Workers pull jobs
- `RPUSH` – Workers push results to `validate:response:<request_id>`
- `BLPOP` – REST pulls result by request ID

Each result key has a **TTL** (default: 1 hour) to prevent memory leaks in case of dropped connections.

> The queue layer is abstracted, so Redis can be swapped for:
> - **Kafka** (keyed topics)
> - **RabbitMQ** (`correlation_id`)
> - **Amazon SQS**, **NATS**, etc.

#### Redis Job Flow

| Action               | Redis Command                          | Key Pattern                   |
|----------------------|----------------------------------------|-------------------------------|
| Enqueue job          | `RPUSH validate:queue`                 | Shared job queue              |
| Worker fetch job     | `BLPOP validate:queue`                 | Blocking consumer             |
| Push result          | `RPUSH validate:response:<request_id>` | One key per request           |
| REST wait for result | `BLPOP validate:response:<request_id>` | Blocking until worker replies |

## Setup & Deployment

### Requirements

- Docker + Docker Swarm
- Internet access to pull docker base images:
  - `traefik:v3.3` – used as the reverse proxy and load balancer
  - `golang:1.24.2-alpine3.21` – for building the REST and Worker services
  - `redis:7` – used as the message queue
  - `prom/prometheus:v2.53.4` – metrics collection
  - `grafana/grafana:11.6.0` – metrics visualization

### Why Docker Swarm?

Prometheus requires **DNS-based service discovery** (e.g. `tasks.rest`) to scrape metrics from all REST replicas. This feature is **not supported** in Docker Compose. Swarm also simplifies multi-replica networking and Traefik routing.

#### Prometheus config snippet:

```yaml
  - job_name: 'rest'
    dns_sd_configs:
      - names: ['tasks.rest']
        type: A
        port: 3000
```

## Deployment Steps

### 1. Initialize Docker Swarm
Enabling Docker Swarm mode activates additional orchestration features on your local Docker environment, such as service scaling, load balancing, and DNS-based service discovery.
  ```bash
  ./swarm-init.sh
```

### 2. Create the external network
The external overlay network is created once and reused. This is to avoid issues from Docker’s async network operations, such as race conditions or leftover resources.
```bash
  ./network-create.sh
```

### 3. Build & Deploy
After making changes to the source code (e.g. REST or Worker services), you can rebuild and redeploy the updated containers using:
```bash
   ./containers-build.sh
```
```bash
   ./stack-deploy.sh
```

### 4. Cleanup

To fully reset the environment or free up space from unused resources:

Remove the stack:
```bash
   ./stack-remove.sh
```

Remove the network:
```bash
   ./network-remove.sh
```

Leave Swarm mode:
```bash
   ./swarm-leave.sh
```

## Access the Deployment

Once the stack is deployed, the following services will be available:

| Service    | URL                                            | Description                                            |
|------------|------------------------------------------------|--------------------------------------------------------|
| REST API   | [http://localhost:3000](http://localhost:3000) | Main entrypoint for issuing requests to the POC        |
| Prometheus | [http://localhost:9090](http://localhost:9090) | Metrics collection and query engine                    |
| Grafana    | [http://localhost:3001](http://localhost:3001) | Metrics dashboard (credentials: `admin / very-secret`) |
| Traefik    | [http://localhost:8080](http://localhost:8080) | Traefik dashboard and routing debug panel              |

Example API request:

```bash
   curl "http://localhost:3000/validate?content=test"
```

Example API response:

```jsonc
{
    "request_id": "69281e0d-a5e5-4136-af9c-e1dec6e4787b",   // Unique identifier assigned to the request (used for tracing).
    "meta": {
        "proxy_request_received_ns":   1744645157689149983, // When the REST service first received the request.
        "proxy_request_pushed_ns":     1744645157689161384, // When the job was pushed into the queue.
        "worker_request_pulled_ns":    1744645157689906528, // When the worker pulled the job from the queue.
        "worker_response_pushed_ns":   1744645157689910728, // When the worker pushed the result back into the queue.
        "proxy_response_pulled_ns":    1744645157690571568, // When the REST service pulled the result to respond to the client.
        "proxy_roundtrip_duration_ns": 1421585              // Full round-trip duration (from receive to response), in nanoseconds.
    },
    "data": {
        "content": "BALAGAN",                               // Returned result - uppercase of the input.
        "result": true                                      // Result/Error indication.
    }
}
```

## Limitations and Out-of-Scope Items

This Proof of Concept is focused on demonstrating the core sync-to-async architecture. The following features are intentionally omitted:

- **Redis is used in standalone mode** – No clustering, replication, or high-availability setup.
- **No retry logic** – Failed worker executions are not retried.
- **No dead-letter queue** – Expired or dropped jobs are not captured for later inspection.
- **No authentication or rate limiting** – The REST API is open and unauthenticated.
- **No QoS or priority handling** – Queue prioritization, SLA routing, and autoscaling by metrics.

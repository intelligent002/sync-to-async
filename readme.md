# Sync-to-Async REST API Pattern

## Purpose – Proof of Concept

This project demonstrates a scalable architectural pattern for REST APIs that must provide synchronous responses to clients while performing the actual processing asynchronously in the background.

It addresses the limitations of traditional worker-based systems that rely solely on internal buffers. In such architectures:

- Short tasks can be blocked behind long-running ones (no task prioritization or reordering)
- Each worker maintains its own isolated queue, limiting the system’s ability to absorb request spikes
- Overflow handling is local and uncoordinated, making graceful degradation harder to implement
- Prioritization, SLA-based routing, or tiered QoS are impractical without centralized coordination
- Retry logic is bound to the worker state, leading to duplicated or inconsistent strategies
- Observability is fragmented across instances, complicating debugging and performance analysis

This pattern decouples request handling from processing by introducing a centralized queue between the REST layer and the workers. It enables fine-grained control, elastic scaling, and robust fault handling — all while maintaining a synchronous interface for clients.

### The system consists of:

1. **A scalable pool of REST API servers** – Handles I/O, accepts incoming requests, and maintains client connections while awaiting results.
2. **A centralized queue** – Stores incoming jobs, optionally with priority tiers and autoscaling metrics.
3. **A scalable pool of asynchronous workers** – Consumes tasks from the queue and performs the actual processing.

Incoming requests are enqueued and processed asynchronously, while the REST layer waits and synchronously returns the final result to the client.

### Unlocked Benefits list:

- **Buffering Under Pressure** – A shared queue absorbs traffic bursts, preventing request loss during peak load.
- **Optimized Load Distribution** – Short tasks are not blocked behind long ones, improving overall system responsiveness.
- **Graceful Degradation** – Queue-based backpressure and timeouts help the system remain stable under stress.
- **Elastic Worker Scaling** – Worker replicas can scale independently based on demand and queue size.
- **Improved Fault Isolation** – The REST layer remains operational even if workers fail or restart.
- **Better Resource Utilization** – REST nodes handle I/O and waiting; workers are focused on processing.
- **Enables Quality of Service (QoS)** – Supports prioritization, throttling, and SLA-aware task routing.
- **Centralized Retry Logic** – Failures can be retried centrally, without relying on client retries.
- **Loosely Coupled Components** – Each part of the system can be updated or replaced independently.
- **Simplified Observability** – Each processing stage can be monitored for latency, throughput, and errors.
- **Mixed Processing Modes** – Lightweight tasks can be fast-tracked; heavy tasks can be queued.
- **Uniform API Interface** – Clients communicate with a standard REST API, abstracting away the async complexity.

## Architecture Overview

The diagram below illustrates the full lifecycle of a request in the sync-to-async system:

![Sync-to-Async Flow](https://raw.githubusercontent.com/intelligent002/sync-to-async/refs/heads/main/charts/flow.png)

### Flow Description:

This architecture decouples synchronous API request handling from backend processing using a centralized queue.

1. The **Client** sends a request to the **Balancer**, which routes it to a **REST API** instance.
2. The **REST API** enqueues the job in a central **Queue** and waits for a result tied to the request ID.
3. A **Worker** consumes the job from the **Queue**, processes it, and responds to the request-specific response channel in the **Queue**.
4. The **REST API** picks up the result and replies to the client.

This pattern is backend-agnostic and supports various queue implementations such as Redis, Kafka, RabbitMQ, etc.

## Implementation Details:

The current implementation uses **Go** for the REST service and **Redis** as the queue mechanism.

### REST API – Go

The REST layer is implemented in **Go**, taking advantage of its lightweight **goroutines** and non-blocking I/O model.

This makes it particularly well-suited for scenarios where many client connections may remain open simultaneously — as each REST instance can efficiently hold thousands of waiting connections without incurring the heavy overhead of traditional threads.

In this architecture, the REST service receives a request, enqueues it into the queue, and then asynchronously waits for the corresponding result — all while keeping the client connection open in a resource-efficient way.

### Queue – Redis

This PoC uses **Redis lists** as the queuing mechanism.  

Redis was selected for its simplicity, speed, and support for **blocking list operations** (`BLPOP`, `RPUSH`), which are ideal for implementing the core sync-to-async messaging flow.

Each incoming job is pushed to a central queue, and a worker consumes it using `BLPOP`. The worker then publishes the result back to a dedicated key associated with the original request ID.

To prevent stale data accumulation (e.g. due to dropped clients), each result key is assigned a **TTL (time-to-live)** using the `EXPIRE` command. This ensures that even if the REST service disconnects or crashes, the result will be automatically cleaned up after a configurable retention period (default: 1 hour).

However, the system is designed in an abstracted way that allows replacing Redis with other queueing systems such as:

- **Kafka** (via keyed topics or headers)
- **RabbitMQ** (using reply queues and `correlation_id`)
- **Amazon SQS**, **NATS**, etc.

The only requirement is support for:

- Enqueueing jobs
- Blocking consumption
- Response routing based on request ID

### Redis Flow

| Action                  | Redis Command                          | Key Pattern                           |
|-------------------------|-----------------------------------------|----------------------------------------|
| Enqueue job             | `RPUSH validate:queue`                  | Shared job queue                       |
| Worker fetch job        | `BLPOP validate:queue`                  | Blocking consumer                      |
| Push result             | `RPUSH validate:response:<request_id>`  | One key per request                    |
| REST wait for result    | `BLPOP validate:response:<request_id>`  | Blocking until worker replies          |

## Setup & Deployment

### Requirements

- Docker & Docker Swarm
- Internet access to pull base images

### Why Docker Swarm?

This PoC uses **Docker Swarm** instead of plain Docker Compose for the following reason:

- **Prometheus DNS-based Service Discovery**  
  Swarm enables Prometheus to discover container replicas dynamically via internal DNS (e.g. `tasks.rest`), which is **not supported in Compose**. This is essential for collecting metrics from all running REST instances:
  
  ```yaml
  # prometheus/prometheus.yml
  
  - job_name: 'rest'
    dns_sd_configs:
      - names: ['tasks.rest']
        type: A
        port: 3000

The following base images needed:

- `traefik:v3.3` – used as the reverse proxy and load balancer
- `golang:1.24.2-alpine3.21` – for building the REST and Worker services
- `redis:7` – used as the message queue
- `prom/prometheus:v2.53.4` – metrics collection
- `grafana/grafana:11.6.0` – metrics visualization

## Deployment

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

| Service     | URL                                 | Description                                            |
|-------------|--------------------------------------|--------------------------------------------------------|
| REST API    | [http://localhost:3000](http://localhost:3000)             | Main entrypoint for issuing requests to the POC        |
| Prometheus  | [http://localhost:9090](http://localhost:9090)             | Metrics collection and query engine                    |
| Grafana     | [http://localhost:3001](http://localhost:3001)             | Metrics dashboard (credentials: `admin / very-secret`) |
| Traefik     | [http://localhost:8080](http://localhost:8080)             | Traefik dashboard and routing debug panel              |

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

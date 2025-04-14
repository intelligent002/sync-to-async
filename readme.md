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

# Sync-to-Async REST API Pattern

## Purpose – Proof of Concept

This project demonstrates a scalable architectural pattern for REST APIs that must return synchronous responses to clients while performing the actual processing asynchronously behind the scenes.

It addresses the limitations of traditional worker-based systems that rely solely on internal buffers. In such systems:

- Short tasks can get blocked behind long ones (no task reordering)
- Each worker has its own isolated buffer, limiting burst handling capacity
- Queue overflows are handled locally, making graceful degradation difficult
- Prioritization or SLA-based routing is impractical without a shared queue
- Retry logic is tightly coupled to worker state and not centralized
- Observability is fragmented, making performance bottlenecks harder to track

This architecture decouples the request buffering from processing. It introduces a centralized queue between the REST layer and the workers, enabling fine-grained control, scalability, and reliability — while still maintaining a synchronous response model.

### The system consists of:

1. Scalable pool of REST API servers - to offload the IO and hold the connections to the clients while waiting the job to be done
2. Centralized queue - for keeping the jobs list, optionally: tiered queues, metrics for autoscale etc. 
3. Scalable pool of asynchronous workers, that do the work.

Incoming requests are enqueued and processed in the background, while the REST layer synchronously waits for and returns the final result.

### Unlocked Benefits:

- **Buffering Under Pressure** – The queue absorbs bursts, preventing request loss under high load.
- **Optimized Load Distribution** – Decoupling request buffering from execution lets short tasks bypass long ones, keeping workers efficiently utilized.
- **Graceful Degradation** – Enables queue-based backpressure and timeouts to maintain stability under pressure.
- **Elastic Worker Scaling** – Workers can be scaled independently of REST services to match processing demand.
- **Improved Fault Isolation** – The API layer remains responsive even if workers crash or restart.
- **Better Resource Utilization** – REST handles I/O and waiting; workers focus on processing.
- **Enables Quality of Service (QoS)** – Enables traffic prioritization, SLA-aware routing, and resource throttling.
- **Centralized Retry Logic** – Failures can be retried by workers without involving clients.
- **Loosely Coupled Components** – Services can be updated, restarted, or replaced independently without downtime.
- **Simplified Observability** – Each stage can be monitored for latency, throughput, and errors.
- **Mixed Processing Modes** – The system can fast-track simple requests and queue heavier ones.
- **Uniform API Interface** – Clients use a standard synchronous API, with no need to understand internal queuing or async behavior.

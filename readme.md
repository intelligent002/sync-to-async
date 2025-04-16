This project demonstrates a scalable architectural pattern for REST APIs that return synchronous responses to clients
while handling processing asynchronously in the background.

This approach addresses several limitations of traditional worker-based systems with internal buffers:

- Long-running tasks block shorter ones (no reordering or prioritization)
- Worker-local queues limit burst-handling capabilities
- Graceful degradation is difficult due to isolated overflow logic
- Centralized QoS, SLA-aware routing and retries are impractical
- Observability is fragmented and debugging becomes harder

To overcome these issues, this pattern introduces a **centralized queue** between the REST layer and workers. This
decouples request handling from processing, allowing elastic scaling, fine-grained control and robust fault tolerance —
all while maintaining a synchronous API surface.

---

📖 **For setup instructions, architecture diagrams, metrics and load testing — see the full [Project Wiki](https://github.com/intelligent002/sync-to-async/wiki/Architecture-overview).**

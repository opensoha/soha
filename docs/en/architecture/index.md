# Architecture Entry

Repository-level engineering memory, execution baselines, frontend/backend technical design, and functional design are now consolidated into the repository-root `agents.md`.

This directory now keeps only public domain and topic architecture documents. It no longer duplicates:

- overall system engineering memory
- frontend engineering structure memory
- backend engineering structure memory
- change rules and memory update rules

## Current Documentation Boundary

This directory keeps:

- application delivery
- monitoring and alerting
- AI Copilot
- soha AI Gateway
- login and identity flow
- agent protocol
- authorization
- multi-cluster model
- event model
- audit model
- MCP integration

During implementation, prefer the repository-root `agents.md` for engineering execution rules, and use the topic documents in this directory for domain-specific design constraints.

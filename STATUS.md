# Continuity v2 status

## Proven in this workspace

- Open Workflow 1.0.3 plan -> deterministic Continuity ExecutionBundle.
- 19 pinned OSS source artifacts imported from the user-provided engine bundle.
- All Continuity Go tests pass with no network access.
- SQLite effect journal persists the effect lifecycle across process restarts.
- An in-flight/uncertain effect is not automatically replayed after restart.
- OPA HTTP client implemented against the standard OPA data API.
- NATS core-protocol smoke publisher implemented for engine bootstrap/health tests.
- open62541 builds locally and a real OPC UA server/client round-trip succeeds.
- Eclipse Paho MQTT C builds locally.
- libmodbus builds locally.

## Remaining external runtime artifacts

The source trees for Temporal, OPA, NATS, and CUE are present, but their pinned releases require newer Go toolchains/transitive module caches than this sandbox has. Continuity therefore treats them as external engines and installs their official release executables under `engines/runtime/bin`.

Fetch them on an internet-connected macOS/Linux machine:

```sh
make runtime-fetch
```

Then bring back `engines/continuity-runtime-binaries.tar.gz`, or install it in-place and run:

```sh
make dev-smoke
```

The live smoke starts real NATS, OPA, and Temporal processes, asks OPA for a policy decision, publishes a real NATS message, and checks the Temporal frontend.

## Next implementation slice

After the live-engine smoke passes:

1. replace the bootstrap NATS protocol client with the official NATS client for JetStream;
2. add the official Temporal SDK and generic bundle worker;
3. add the official MCP SDK and `service.status` / `service.restart` agent tools;
4. connect the effect journal to that real MCP dispatch path;
5. force-kill the agent after dispatch and prove recovery by independent verification.

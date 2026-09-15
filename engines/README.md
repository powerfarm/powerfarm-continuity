# Continuity engine fetch kit

The ChatGPT build container used to prepare this repository has no outbound DNS, so the engine sources cannot be fetched here. This directory is deliberately executable rather than being a prose shopping list.

## Fast path

On a Mac or Linux machine with internet access:

```bash
cd v2/engines
./fetch-engines.sh all
./pack-engines.sh
```

Upload `continuity-engines.tar.gz` back into the conversation. The archive contains the pinned source trees/packages plus `fetched.tsv` with checksums.

If you want the smallest useful first delivery, run:

```bash
./fetch-engines.sh core
./pack-engines.sh continuity-core-engines.tar.gz
```

`core` currently fetches Open Workflow 1.0.3, the official MCP Go SDK, Temporal, OPA, NATS Server, and CUE.

## Profiles

- `core`: workflow/orchestration/policy/transport/schema pieces needed first.
- `world`: Eclipse Ditto, Crossplane, and node-wot packages.
- `identity`: SPIRE.
- `packaging`: ORAS and Cosign.
- `sandbox`: Wasmtime.
- `observability`: OpenTelemetry Collector release repository.
- `industrial`: open62541, Eclipse Paho MQTT C, and libmodbus.
- `all`: everything above.

Each GitHub dependency is fetched as a release-tag source archive, not a moving branch. npm dependencies are packed as `.tgz` files. The Open Workflow schema is fetched from its versioned schema URL.

## Before downloading

The script needs:

- `curl`
- `tar`
- `npm` only for the `world` profile / node-wot packages
- either `sha256sum` (Linux) or `shasum` (macOS)

You can inspect exactly what will happen without downloading anything:

```bash
./fetch-engines.sh --dry-run all
```

## Why sources instead of giant prebuilt binaries?

The returned bundle needs to be portable between macOS, Linux, ARM64, and x86-64 while we are still deciding the actual deployment topology. Pinned source trees and package tarballs give Continuity the engines and interfaces without locking the project to this ChatGPT container's architecture. Once the runtime topology is fixed, we can replace selected source entries with signed release binaries/container digests.

## Important

This lockfile is a bootstrap lock, not an automatic update policy. Updating an engine version should be an explicit commit with compatibility tests.

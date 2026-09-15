# Continuity runtime binaries

The source bundle is already vendored under `downloaded/`. These four executables are fetched separately because the pinned upstream projects require newer Go toolchains and transitive module caches than the current sandbox provides.

Run on any internet-connected macOS or Linux machine:

```sh
cd v2/engines
bash RUN_RUNTIME_BINARIES.command
```

It auto-detects OS/architecture and creates:

```text
continuity-runtime-binaries.tar.gz
continuity-runtime-binaries.tar.gz.sha256
```

The archive contains only:

- OPA 1.20.2
- NATS Server 2.14.6
- Temporal CLI 1.8.1, used only for a local Temporal development service (production server target remains 1.32.0)
- CUE 0.17.1

Upload the `.tar.gz` back into the Continuity workspace. It unpacks directly into `v2/engines/runtime/`.

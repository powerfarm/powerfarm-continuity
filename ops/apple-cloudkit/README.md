# CloudKit Provisioner

Continuity adapter for `iCloud.app.powerfarm`.

## Why this exists

Powerfarm uses CloudKit only as app/engine-owned operational storage. Private namespaces are CloudKit custom record zones in the Director's private database.

Apple's 2026 automation model separates:

- **management token**: long-lived container/schema management; no private data access;
- **user token**: short-lived user-authenticated data access; required for private database operations.

The user token is intentionally not fully automatable by Apple. Remote provisioning is programmatic **after** a current user-authenticated token exists. Token renewal remains an explicit human authentication boundary.

## Bootstrap on an ecosystem LAB

Requirements:

- macOS with Xcode/cktool;
- Node.js;
- Director authenticated to the appropriate Apple Developer/CloudKit Console account.

Run locally when authentication is needed:

```sh
./bootstrap-auth.sh
```

Both tokens are stored by `cktool` in the macOS login Keychain under `com.apple.icloud.cktool`. Do not copy them into Git, Registry, Search, receipts, logs or command-line arguments.

Install the exact Apple packages:

```sh
npm install
```

## Reset historical development state

```sh
./reset-development.sh
```

This exports production/development schema evidence, resets Development to Production, then exports Development again and writes SHA-256 receipts.

This is destructive to Development data and is only appropriate because the Director classified the historical CloudKit state as disposable test material.

## Provision a private namespace

```sh
node provision-private-zone.js pf.app.example
```

The operation:

1. obtains the short-lived user token from the login Keychain without printing it;
2. creates a `REGULAR_CUSTOM_ZONE` in the private database;
3. verifies the zone if creation reports an error, making repeated requests idempotent at the namespace level;
4. prints only non-secret provisioning evidence.

Default environment is Development. Set `POWERFARM_CLOUDKIT_ENVIRONMENT=production` only through an adopted onboarding/effect path.

## Durable boundary

The provisioner is a provider adapter, not authority.

Identity/contracts decide:

- whether the app/engine exists;
- whether it owns a CloudKit store;
- the stable store identity and namespace;
- environment and lifecycle.

Continuity performs the provider operation and records the receipt.

CloudKit owns ordinary operational rows inside the resulting namespace.

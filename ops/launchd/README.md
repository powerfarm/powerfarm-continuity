# Keeping the circuit present

Two macOS user agents, one for the Heartime ledger's delivery loop and one for the Continuity ingress.

**This is process supervision and nothing else.** launchd keeps two processes present. It makes no statement about whether obligations are evaluated on time, and it is **not** the independent Heartime watchdog: Heartime still cannot certify its own availability, and a supervisor that restarts it cannot certify it either.

Nothing in Powerfarm is a supervisor, a deployment controller or a daemon manager. The operating system already does this, so the operating system does it.

```sh
ops/launchd/install.sh /Users/you/powerfarm-unattended
```

Both processes recover from their own durable state when they start: the ledger reads its ledger, and the ingress sweeps its occurrence records. A restart is therefore an ordinary restart. An occurrence already delivered is not delivered again as new work — the ingress deduplicates by occurrence identity before any effect is claimed — and an activation interrupted mid-flight is resolved as uncertain rather than re-run.

```sh
launchctl print gui/$(id -u)/work.minilab.powerfarm.ingress | head -20
launchctl kickstart -k gui/$(id -u)/work.minilab.powerfarm.ingress   # restart it
launchctl bootout gui/$(id -u)/work.minilab.powerfarm.heartime        # stop it
```

## What this does and does not restore

A **user agent** is started when its user logs in, and restarted whenever it exits. That covers a crash, a kill and a logout. It does **not** cover a reboot on a machine that does not log that user in automatically: nothing runs until someone logs in. Making the circuit survive an unattended reboot means a `LaunchDaemon` in `/Library/LaunchDaemons`, which runs as root at boot and is a different authority decision — the ledger, the credentials and the places all currently belong to one user.

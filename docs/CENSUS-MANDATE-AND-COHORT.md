# The census: delegated authority and a cohort it resolves for itself

Two changes make the census institutionally autonomous rather than operationally configured.

1. What it may inventory, under which observability contract, and with which Registry read are **delegated by a mandate**, not chosen on a command line. It is the third real responsibility carried by the same immutable `x-mandate` mechanism as institutional work and planning.
2. It no longer reconciles against a remembered cohort. **Every occurrence resolves the currently recognized cohort under an institutional machine authority and freezes exactly that**, as an immutable manifest referenced by digest.

Normative sources: `powerfarm-specs/specs/HEARTIME_CONTRACT_v0.md` §9 and `specs/EXECUTABILITY_CONTRACT_v0.md` §17.1–17.3.

## The circuit

```text
Registry current recognized topology
  -> census.resolve      one place, one instant, a machine service credential
  -> cohort manifest     occurrence-specific, immutable, referenced by digest
  -> census.probe        authorized read-only inventory of that one place
  -> census.record       Antenna, under the accepted observability contract
  -> census.reconcile    expected vs observed, against the frozen manifest
```

Nothing writes back. The Registry is read at an instant and is never operational state: a later occurrence resolves its own cohort, and the one in flight keeps the one it froze.

## The cohort manifest

`powerfarm.census.cohort/v0`, canonical JSON in content-addressed storage; its digest is the cohort reference for the whole occurrence.

```json
{
  "schema": "powerfarm.census.cohort/v0",
  "occurrence": "sha256:…",
  "resolvedAt": "2026-09-16T14:13:03Z",
  "authority": {
    "source": "powerfarm-registry",
    "endpoint": "https://…/rest/v1/rpc/powerfarm_place_cohort",
    "mechanism": "registry-service-credential",
    "grant": "registry.cohort.read",
    "identity": "<the machine identity the Registry resolved from the credential>"
  },
  "place":   {"id": "pf.app-park.8gb", "path": "…", "machine": "pf.lab-8gb"},
  "members": [{"id": "pf.coloured-places", "kind": "app", "path": "…"}],
  "limitations": ["recognition is declared placement, not liveness"]
}
```

A manifest is refused unless it answers this occurrence, describes this place, names the authority that produced it, and lists each member once with a kind. Recognition is never inferred from filesystem presence: presence is what the probe observed, recognition is what the Registry stated, and reconciliation is the comparison of the two.

The manifest records where the Registry says the place is; the **mandate** bounds what was actually inventoried. When they disagree, the disagreement is written into the manifest's limitations rather than resolved in favour of either.

## There is nothing to fall back to

When no authority can answer, the sweep is **contained**: nothing probed, nothing recorded, nothing reconciled, no institutional state changed — and a `DirectionDecision` is recorded at the `human-only-authority` boundary.

This is deliberate. A census that reconciled against a cohort nobody re-established would be asserting recognition it does not have, and recognition is the Registry's to state. A stale cohort does not fail loudly; it quietly reports a newly admitted place as unrecognized forever. Containment is the honest outcome, the obligation stays covered, and its next occurrence is its future evaluation.

## The Registry authority boundary

**The census cannot yet resolve its cohort, and the reason is precise.**

**The smallest authority required.** For one named place: `slug`, `kind` and `metadata->>'path'` of the identities whose `metadata->>'place'` is that place, plus that place's own `slug`, `path` and `machine`. Nothing else — no grants, no credentials, no artifacts, runs, gadgets, workspaces or deployments. Read-only, one place per call.

**The current mechanism preventing it.** Registry authority is resolved from a Supabase Auth session: `public.identidade_atual()` and `public.has_registry_grant()` both join `identity_links.supabase_user = auth.uid()`, and the `identities` select policy is granted to the role `authenticated`. A `service_credentials` bearer never becomes `authenticated` and never sets `auth.uid()`, so a machine holding one can read nothing through that path. The one machine-authenticated read that exists, `public.powerfarm_antenna_snapshot(p_token text)`, resolves the caller by `sha256(token)` against `service_credentials` and returns Antenna service bindings only.

So the credential mechanism exists — `public.powerfarm_issue_service_credential(p_slug, p_label)` already mints a non-human credential bound to an identity — and the **authorization surface for this read does not**.

**The smallest conforming implementation option.** One `security definer` function in the shape `powerfarm_antenna_snapshot` already proves, granted `execute` to `anon`:

- resolve the caller's identity from `service_credentials` by digest, exactly as the Antenna snapshot does;
- require that identity to hold an unexpired, unrevoked `grants` row for a new action — `registry.cohort.read` — checked by `identity_id`, not by `auth.uid()`, optionally scoped to one place through `grants.resource`;
- return only `{place:{id,path,machine}, members:[{id,kind,path}], resolvedAt, identity}` for that place;
- issue one credential with the existing function and one grant row.

That is one migration in `powerfarm-identity` and a grant. It adds no service account, no new authentication mechanism and no new subsystem, and it does not make the Registry operational state. It is an authority decision, so it is Direction's to make, not this slice's.

## Mandate terms across the three real responsibilities

| Term | institutional work | planning | census |
| --- | --- | --- | --- |
| `contract` (the responsibility) | ✓ | ✓ | ✓ |
| `owner` | ✓ | ✓ | ✓ |
| `objective` | ✓ | ✓ | ✓ |
| `expiresAt` | ✓ | ✓ | ✓ |
| `allowedCapabilities` | ✓ | ✓ | ✓ |
| `timeoutSeconds` | ✓ | ✓ | ✓ |
| `containmentReviewSeconds` | ✓ | ✓ | ✓ |
| `directionBoundaries` | ✓ | ✓ | ✓ |
| `maxInvocations` | ✓ | ✓ | — |
| `periodSeconds`, `planningReviewAfterSeconds` | ✓ prepares | ✓ decides | — |
| `contextBytes` | ✓ | — | — |
| `allowedOutput` | ✓ | — | — |
| `noNewSpending` | ✓ | — | — |
| `census.place`, `placePath`, `placeHost` | — | — | ✓ |
| `census.observabilityContract` | — | — | ✓ |
| `census.cohortGrant` | — | — | ✓ |
| `census.repair` (must be false) | — | — | ✓ |

**Eight terms are common to all three.** They are the same four things every bounded responsibility needs: who it is and who owns it (`contract`, `owner`), what it is for and until when (`objective`, `expiresAt`), what it may do (`allowedCapabilities`), and what it may spend or escalate (`timeoutSeconds`, `containmentReviewSeconds`, `directionBoundaries`).

**`maxInvocations` is common to exactly the two responsibilities with a replaceable occupant.** Work and planning both invoke a route that may be a model; the census has no occupant to bound. It is a budget term, not a core term.

**`periodSeconds` and `planningReviewAfterSeconds` are shared by two responsibilities in opposite directions**: work *prepares* the next period with them, planning *decides* one with them. Same terms, two roles.

### Concrete duplication and awkwardness

- **Three validators over one struct.** `Mandate.Validate`, `Mandate.ValidatePlanning` and `CensusMandate.ValidateCensus` each re-check the same common clauses and differ only in their last few. The common eight are now bound-checked in three places with near-identical code.
- **A validated term nobody reads is false assurance.** The census's first mandate carried `maxInvocations`, `contextBytes`, `allowedOutput`, `noNewSpending`, `periodSeconds` and `planningReviewAfterSeconds` because the shared struct has them. They were removed from its document: a census spends or produces none of them, and bounding a term nothing consumes asserts a limit that limits nothing.
- **The census needed terms the struct cannot express**, so they went into a nested `census` object and a second type that embeds `Mandate`. Notably, the six terms it needed were previously **command-line flags** — which means the place a census could inventory and the contract it recorded under were never delegated authority at all, only operator configuration.
- **The two kinds of responsibility-specific term are not the same kind.** Work's three (`contextBytes`, `allowedOutput`, `noNewSpending`) bound what it may *consume and produce*. The census's six bound what it may *touch*. A shape that treats them as one bucket would be a guess.
- `allowedOutput` is a single filename with no way to say "writes nothing" other than the empty string, which its own validator rejects as unbounded.

**Recommendation: still do not specify it.** The evidence now supports a common core of eight plus per-responsibility terms, and one `ValidateCore` would remove the triplicated clauses. But whether "may consume" and "may touch" are one category or two is not yet answerable from three responsibilities, and guessing it into a specification is how an ontology gets invented. The next responsibility that needs scope terms settles it.

## Recorded gaps, not solved here

- Authenticated remote `report` and `renew`: outcomes still travel through the ledger's own local command, so the ingress and the planning turn share the ledger's host.
- Observability credential rotation: the Antenna credential is a long-lived file with no rotation in this path.
- The independent Heartime watchdog: still absent. Process supervision is not it — see [`ops/launchd/README.md`](../ops/launchd/README.md).
- Presence semantics: the probe compares directory names at one place, so a live deployment and a leftover directory are indistinguishable.
- Registry admission of contracts and mandates: they remain trusted local operator documents.

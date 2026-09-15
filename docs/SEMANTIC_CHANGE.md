# Receipted semantic change loop

Continuity can now use its semantic self-model as the boundary for LLM-authored source changes.

```text
graph-context
    |
    v
LLM proposal (bound to current graph digest)
    |
    v
change-stage
    |
    +--> isolated source workspace
    +--> candidate semantic graph
    +--> semantic before/after diff
    +--> LogLine receipts + evidence
    |
    v
human / accountable review
    |
    v
change-accept -confirm <proposal-id>
    |
    +--> prove live base is unchanged
    +--> independently re-stage exact proposal
    +--> prove graph == reviewed candidate graph
    +--> apply with rollback snapshots
    +--> acceptance receipt
```

This is deliberately not an autonomous self-modification endpoint. The model may propose and Continuity may prove. Acceptance remains a distinct accountable act.

package powerfarm.continuity

import rego.v1

default allow := false
default requires_approval := false

allow if input.authorization.mode == "automatic"

allow if {
    input.authorization.mode == "policy"
    input.effect.class != "at_most_once"
    input.effect.class != "irreversible"
}

requires_approval if input.authorization.mode == "approval"
requires_approval if input.effect.class == "at_most_once"
requires_approval if input.effect.class == "irreversible"

decision := {
    "allow": allow,
    "requires_approval": requires_approval,
    "reason": reason,
}

reason := "allowed by Continuity policy" if allow
reason := "explicit approval required" if {
    not allow
    requires_approval
}
reason := "denied by Continuity policy" if {
    not allow
    not requires_approval
}

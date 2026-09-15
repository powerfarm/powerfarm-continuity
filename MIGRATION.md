# Migration from the LangGraph proof-of-concept

The original implementation is useful evidence, not the target architecture.

| v1 | v2 disposition |
|---|---|
| `graphs/rescue.json` custom drawing | replace with Open Workflow Specification document |
| `continuity/drawing.py` graph builder | delete after feature parity; Open Workflow is the source syntax and Temporal becomes runtime |
| named Python valves | replace with semantic capability calls backed by MCP/WoT/OpenAPI/AsyncAPI/Wasm contracts |
| `tier_of()` | delete; policy comes from capability metadata + OPA |
| `ask_approval()` LangGraph interrupt | replace with durable authorization/approval state in Temporal |
| `run_command()` | becomes one optional OS capability, not the executor itself |
| SQLite LangGraph checkpoints | replace centrally with Temporal history |
| `ledger.py` | keep the idea, redesign as the agent's effect journal |
| "started" / "done" | expand to planned/authorized/dispatched/acknowledged/verified/uncertain/failed |
| CLI | keep as a human/debug client; MCP is the primary authoring/control API |

## Migration rule

Do not delete v1 until v2 can demonstrate the original rescue scenario end-to-end with these stronger properties:

1. restart is expressed as `service.restart`, not a shell command;
2. policy authorizes the semantic operation;
3. execution is journaled before dispatch;
4. a lost acknowledgement produces `uncertain`, never an assumed failure;
5. `service.status` independently verifies the effect;
6. a process crash does not cause an unsafe duplicate effect;
7. the published plan and capability versions are content-addressed.

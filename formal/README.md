# Executable State Models

These TLA+ models cover runtime state machines. They do not prove external verb behavior.

## Models

- `Saga.tla` models durable forward and reverse dispatch, bounded attempts, lease owners and fencing tokens, rejected stale completions, outcome classes, blocked states, recovery, and terminal states.
- `GenerationSwap.tla` models concurrent candidates, validation, generation conflicts, atomic publication, and drain phases.

The saga model permits lease expiry and recovery. A replacement worker receives a larger token. A completion with a stale owner or token changes only the bounded stale-completion counter. Retryable outcomes consume attempts. Unknown, fence, and dependency outcomes enter explicit blocked states. Durable compensation dispatch runs in reverse source order.

The generation model records one generation for each started request. A later publication does not change that request generation. A candidate can publish only when its captured base is still active.

## Run the models

Install the TLA+ tools and run:

```bash
tlc formal/Saga.tla -config formal/Saga.cfg
tlc formal/GenerationSwap.tla -config formal/GenerationSwap.cfg
```

Run both commands before changing their modeled state transitions.

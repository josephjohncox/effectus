# Executable State Models

These finite TLA+ models check selected state-machine invariants. They do not prove the runtime implementation or external verb behavior.

## Models

- `Saga.tla` models durable forward and reverse dispatch, bounded attempts, lease owners and fencing tokens, rejected stale completions, outcome classes, blocked states, recovery, and terminal states.
- `GenerationSwap.tla` models concurrent candidates, validation, generation conflicts, atomic publication, and drain phases.

The saga model permits lease expiry and recovery. A replacement worker receives a larger token. A completion with a stale owner or token changes only the bounded stale-completion counter. Retryable outcomes consume attempts. Unknown, fence, and dependency outcomes enter explicit blocked states. Durable compensation dispatch runs in reverse source order.

The generation model records one generation for each started request. A later publication does not change that request generation. A candidate can publish only when its captured base is still active.

This publication model is abstract. The daemon does not provide hot reload. See the [implemented lifecycle](../docs/LIFECYCLE.md).

Both configurations disable deadlock checks and specify no temporal liveness property. A successful run checks only their finite domains and configured invariants.

## Run the models

CI uses the [stable v1.7.4 release](https://github.com/tlaplus/tlaplus/releases/tag/v1.7.4).
It verifies SHA256 `936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88` before installation.
The upstream `v1.8.0` prerelease changes with master commits. Do not replace a failed checksum with its latest download hash.

Use a `tlc` launcher for the verified stable jar. Run from the repository root:

```bash
tlc formal/Saga.tla -config formal/Saga.cfg
tlc formal/GenerationSwap.tla -config formal/GenerationSwap.cfg
```

Run both commands before changing their modeled state transitions.

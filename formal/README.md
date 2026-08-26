# Executable State Models

These TLA+ models cover runtime state machines. They do not prove external verb behavior.

## Models

- `Saga.tla` models ordered effects, retries, failure, and reverse compensation.
- `GenerationSwap.tla` models concurrent candidates, validation, generation conflicts, atomic publication, and drain phases.

The saga model includes `RetryPending`. This action can increase an effect attempt count more than once. The model therefore does not claim exactly-once execution.

The generation model records one generation for each started request. A later publication does not change that request generation. A candidate can publish only when its captured base is still active.

## Run the models

Install the TLA+ tools and run:

```bash
tlc formal/Saga.tla -config formal/Saga.cfg
tlc formal/GenerationSwap.tla -config formal/GenerationSwap.cfg
```

CI should run both commands when the TLA+ toolchain becomes part of the repository setup.

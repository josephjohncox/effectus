# Generated gRPC Execution Client

`main.go` is a small client for `RulesetExecutionService`. It accepts a daemon
address, bearer token, ruleset, and version, then sends typed facts over gRPC.

Plaintext transport is suitable only for a loopback development daemon. Supply
TLS credentials for a deployed daemon.

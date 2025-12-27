# Fraud E2E Example

This example exercises Effectus end-to-end with a fraud workflow: list rules for quick decisions and a saga-enabled flow for investigation. It shows how facts, schema types, verbs, and compensation fit together.

## Files
- `schema/fraud_facts.json`: fact type schema for `transaction`, `customer`, and `device`.
- `data/facts.json`: sample facts payload.
- `rules/fraud_rules.eff`: list rules that flag or freeze based on thresholds.
- `flows/fraud_flow.effx`: a flow that opens a case, freezes an account, updates the case, and notifies risk.
- `main.go`: runner that compiles and executes both rules and flow.

## Run
```bash
go run ./examples/fraud_e2e
```

Scripts (same flow, shorter commands):
```bash
./examples/fraud_e2e/scripts/run-local.sh
./examples/fraud_e2e/scripts/run-compose.sh
./examples/fraud_e2e/scripts/run-compose-failure.sh
./examples/fraud_e2e/scripts/down-compose.sh
```

To run with mocked risk and notification services:
```bash
docker compose -f examples/fraud_e2e/docker-compose.yml up -d --build
RISK_URL=http://localhost:8081/cases NOTIFY_URL=http://localhost:8082/notify go run ./examples/fraud_e2e
```

To simulate a notification failure and see saga compensation (the mock returns 500):
```bash
FAIL_NOTIFY=1 RISK_URL=http://localhost:8081/cases NOTIFY_URL=http://localhost:8082/notify go run ./examples/fraud_e2e
```

Expected behavior: `NotifyRisk` fails, and the flow compensates by invoking `UnfreezeAccount`.

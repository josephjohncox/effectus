"""Authenticated client for the shared order-review scenario.

Generate bindings beside the effectus/v1 proto files and set PYTHONPATH to the
repository root. TLS is the default. The README describes the local test gate.
"""

import argparse
import json
import math
import os
import sys
from pathlib import Path

import effectus.v1.execution_pb2 as execution_pb2
import effectus.v1.execution_pb2_grpc as execution_pb2_grpc
import grpc
from google.protobuf.struct_pb2 import Struct


def run(args):
    token = (args.token or os.environ.get("EFFECTUS_API_TOKEN", "")).strip()
    if not token:
        raise ValueError("set EFFECTUS_API_TOKEN or --token")
    if not math.isfinite(args.timeout) or args.timeout <= 0 or args.timeout > 300:
        raise ValueError("timeout must be positive and at most 300 seconds")
    if args.allow_insecure and args.ca_file:
        raise ValueError(
            "choose TLS with --ca-file or explicit --allow-insecure, not both"
        )
    scenario_path = Path(__file__).resolve().parents[1] / "order_review/data/order.json"
    try:
        scenario = json.loads(scenario_path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as error:
        raise ValueError("cannot load the shared order-review scenario") from error
    facts = scenario["request"]["facts"]
    if args.order_id:
        facts["order"]["id"] = args.order_id
    typed_facts = Struct()
    typed_facts.update(facts)
    request = execution_pb2.ExecutionRequest(
        ruleset_name=args.ruleset,
        version=args.version,
        namespace=args.namespace or scenario["request"]["namespace"],
        idempotency_key=args.idempotency_key or scenario["idempotency_key"],
        typed_facts=typed_facts,
        wait_mode=execution_pb2.EXECUTION_WAIT_MODE_TERMINAL,
    )
    if args.allow_insecure:
        channel = grpc.insecure_channel(args.address)
    else:
        roots = Path(args.ca_file).read_bytes() if args.ca_file else None
        credentials = grpc.ssl_channel_credentials(root_certificates=roots)
        channel = grpc.secure_channel(args.address, credentials)
    with channel:
        response = execution_pb2_grpc.RulesetExecutionServiceStub(
            channel
        ).ExecuteRuleset(
            request,
            timeout=args.timeout,
            metadata=(("authorization", "Bearer " + token),),
        )
    return {
        "execution_id": response.execution_id,
        "state": execution_pb2.ExecutionState.Name(response.state),
        "generation_digest": response.generation_digest,
        "durably_accepted": response.durably_accepted,
        "completed": response.completed,
        "success": response.success,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--address", default="127.0.0.1:9091")
    parser.add_argument("--token", default="", help="Prefer EFFECTUS_API_TOKEN")
    parser.add_argument("--ruleset", default="order-review")
    parser.add_argument("--version", default="1.0.0")
    parser.add_argument("--namespace", default="")
    parser.add_argument("--idempotency-key", default="")
    parser.add_argument("--order-id", default="")
    parser.add_argument(
        "--ca-file", default="", help="PEM trust roots. Default: system roots"
    )
    parser.add_argument("--allow-insecure", action="store_true")
    parser.add_argument(
        "--timeout", type=float, default=10, help="Seconds, at most 300"
    )
    args = parser.parse_args()
    try:
        result = run(args)
    except grpc.RpcError as error:
        print("RPC failed: " + error.code().name, file=sys.stderr)
        return 1
    except (OSError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

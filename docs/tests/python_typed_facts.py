from effectus.v1 import execution_pb2
from google.protobuf import struct_pb2

request = execution_pb2.ExecutionRequest(
    ruleset_name="orders",
    version="1.0.0",
    namespace="tenant-a",
    idempotency_key="order-42",
    typed_facts=struct_pb2.Struct(
        fields={
            "order_id": struct_pb2.Value(string_value="order-42"),
            "total_cents": struct_pb2.Value(number_value=12500),
        }
    ),
    wait_mode=execution_pb2.EXECUTION_WAIT_MODE_TERMINAL,
)

assert request.HasField("typed_facts")
assert request.typed_facts.fields["order_id"].string_value == "order-42"

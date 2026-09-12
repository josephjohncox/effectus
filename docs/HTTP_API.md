# HTTP API Reference

This reference describes the current `effectusd` HTTP handler.
It is separate from the [inbound gRPC service](GRPC_EXECUTION.md) and [outbound executor integration](INTEGRATION.md).
HTTP execution acknowledges admission. It does not offer a terminal-wait option or an execution-history API.

## Listener and authentication

`--http-addr` defaults to `:8080`. An empty address disables HTTP.
The daemon requires a nonblank `EFFECTUS_API_TOKEN` when HTTP is enabled.
The startup configuration trims that token.
Send exactly one header with the case-sensitive prefix and exact token:

```http
Authorization: Bearer TOKEN
```

The handler compares the parsed header value exactly.
HTTP/1 header parsing removes surrounding spaces and tabs before this comparison.
Multiple authorization values, a different prefix, or extra whitespace between `Bearer` and the token still fail authentication.
A failure returns HTTP 401, `WWW-Authenticate: Bearer realm="effectusd"`, and the error object below.

<!-- http-example: authentication-error -->
```json
{"error":"authentication failed"}
```

The bearer token authorizes the API, not a particular namespace.
A namespace separates execution identities. It is not a tenant authorization policy.
Use an appropriate trusted gateway if you need per-tenant access control.

HTTP has no built-in TLS flags. Use a trusted TLS-terminating boundary outside local development.
The gRPC certificate flags do not enable TLS for HTTP.

## Routes

| Method | Path | Authentication | Success |
| --- | --- | --- | --- |
| `GET` | `/healthz` | None | `200`, liveness object |
| `GET` | `/readyz` | None | `200`, active generation view |
| `GET` | `/v1/status` | Bearer | `200`, active generation view |
| `POST` | `/v1/dry-run` | Bearer | `200`, plan-evaluation array |
| `POST` | `/v1/execute` | Bearer | `202`, admission result |

Other methods return 405 with `Allow: GET` or `Allow: POST` for the supported route.
`HEAD` is not an alias for `GET`. A real HTTP server suppresses the response body for `HEAD`, including error bodies.
There is no automatic `OPTIONS` or CORS handler.

Paths must match the supported route. Trailing slashes, repeated separators, and traversal-looking paths return 404, not redirects.
Query parameters do not change route matching or execution options.
An unknown `/v1` or `/v1/*` path authenticates before returning 404.
Other unknown paths return 404 without authentication.

<!-- http-example: route-error -->
```json
{"error":"route not found"}
```

There is no HTTP endpoint to register rules, reload a bundle, list historical executions, fetch one execution by ID, or wait for terminal completion.
In particular, `/v1/status` describes the active generation, not the state of a supplied execution ID.

## JSON requests and limits

Send `Content-Type: application/json`. The current handler does not enforce that request header.
Handler responses use `Content-Type: application/json` and a trailing newline.
Header/protocol errors rejected by Go's HTTP server before the handler do not necessarily use this JSON format.

Both POST bodies have a limit of **1,048,576 bytes**, including JSON whitespace.
The limit applies to fixed-length and chunked bodies. The exact limit is accepted if the JSON and request are otherwise valid.
An oversized body returns 413.

Each body must contain one JSON object, followed only by whitespace.
Malformed input, empty bodies, extra JSON values, trailing junk, and unknown request fields return 400.
`facts` must be an object. Missing, `null`, array, or scalar facts do not supply valid input.
An empty object is syntactically valid but can fail the active generation's fact requirements.
Use the canonical lowercase field names shown below and do not rely on duplicate JSON members or duplicate non-authentication headers.

The decoder preserves number text with `json.Number` before typed normalization.
Declared integers must fit signed int64. Declared floats must normalize to finite values.
This does not recover digits that the caller already rounded before serialization.

## `GET /healthz`

This is a static liveness response while the HTTP admission gate is open.
It does not check PostgreSQL, executors, Kafka, or recovery progress.

<!-- http-example: health-response -->
```json
{"status":"ok"}
```

## `GET /readyz` and `GET /v1/status`

Both return the active generation view. Only `/v1/status` requires a token.
Startup compiles the generation, resolves bindings, and validates or applies durable migrations before serving begins.
Neither route pings dependencies on each request. A later database outage can coexist with HTTP 200 here.

The unauthenticated readiness response includes rule and environment metadata.
Restrict network access to the listener if that metadata is sensitive.
It does not include executor descriptor credentials.

The outer field names intentionally use Go-style capitalization. They are not the lowercase fields of the admission response.

| Field | Type | Meaning |
| --- | --- | --- |
| `Ruleset` | string | Active bundle name. |
| `Version` | string | Active bundle version. |
| `GenerationDigest` | string | Active immutable generation identity. |
| `IRDigest` | string | Checked IR identity. |
| `SourceDigest` | string | Source-bundle identity. |
| `Environment` | object | Declarations described below. |
| `Plans` | array of plan views | Plans in deterministic checked order. |

The executable JSON examples in this reference use a test generation named `orders`, version `1`.
It declares `order.id: string`, `order.risk: int`, and a `RequestReview` verb.
For another bundle, use its `Environment` rather than copying those example fact names.
Angle-bracket strings below stand for generation-specific values, not literal request values.

<!-- http-example: generation-response -->
```json
{
  "Ruleset":"orders",
  "Version":"1",
  "GenerationDigest":"<generation-digest>",
  "IRDigest":"<ir-digest>",
  "SourceDigest":"<source-digest>",
  "Environment":{
    "facts":{"order.id":"string","order.risk":"int"},
    "verbs":{
      "RequestReview":{
        "arguments":{"orderId":"string"},
        "required_args":["orderId"],
        "result_type":"string",
        "retry_policy":{"max_attempts":0,"initial_backoff_millis":0,"max_backoff_millis":0},
        "idempotency_policy":"",
        "fencing_required":false
      }
    },
    "functions":{},
    "types":{}
  },
  "Plans":[{
    "ID":"<plan-id>",
    "Dialect":1,
    "Priority":1,
    "Predicate":"<diagnostic-expression>",
    "Verbs":["RequestReview"]
  }]
}
```

### Plan view

All five fields are emitted, including empty values:

| Field | Type | Meaning |
| --- | --- | --- |
| `ID` | string | Plan identity within the checked artifact. Treat it as opaque. |
| `Dialect` | integer | `1` for `.eff` list rules, `2` for `.effx` flows. `0` is unspecified. |
| `Priority` | signed 32-bit integer | Checked plan priority. |
| `Predicate` | string | Diagnostic protobuf expression text, not source syntax or a stable parser input. |
| `Verbs` | array of strings | Ordered verb names, including repeated names. Empty plans use `[]`. |

### Declaration environment

`Environment` always emits `facts`, `verbs`, `functions`, and `types`.
These are maps. Clients should also tolerate JSON `null` for nil map values from the Go representation.
These are declaration values, not a rendering of effective checked-step defaults.
For example, zero `max_attempts` and an empty idempotency policy remain visible here.
The checker normalizes those execution defaults to one attempt and `none`.

- `facts`: fact path to type-name string.
- `verbs`: verb name to contract object.
- `functions`: function name to declaration object. A declaration does not make an unavailable predicate function executable.
- `types`: named type to structural definition.

A verb contract emits these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `arguments` | map of strings or null | Argument name to type name. |
| `required_args` | array of strings or null | Declared required argument names. An unspecified list means all arguments when checked. |
| `result_type` | string | Result type, including `void` when no result is expected. |
| `inverse_verb` | optional string | Declared inverse operation. Omitted when empty. |
| `retry_policy` | object | Unsigned integer `max_attempts`, `initial_backoff_millis`, and `max_backoff_millis`. All three are emitted. |
| `idempotency_policy` | string | Empty/default, `none`, `key_required`, or `sink_guaranteed`. This is a declaration, not proof of destination behavior. |
| `fencing_required` | boolean | Whether the checked operation requires fencing. |

A function declaration emits `argument_types` (string array or null), `return_type` (string), `pure` (boolean), and `total` (boolean).
A type definition emits `kind`: `object`, `list`, or `map`.
Its optional fields are `element_type` (string), `fields` (map of type-name strings), and `required_fields` (string array).
Empty optional fields are omitted.

## `POST /v1/dry-run`

The only request member is `facts`. No namespace, key, generation constraint, or wait option is required or interpreted.
The route evaluates predicates against the active generation without creating an execution or invoking a verb.
It does not prove that a destination will succeed.

<!-- http-example: dry-run-request -->
```json
{"facts":{"order.id":"one","order.risk":90}}
```

The response includes every plan and its match result, not only matching plans.
The top-level response is an array. An empty generation returns `[]`.
`Plan` and `Matched` retain their exact capitalization.

<!-- http-example: dry-run-response -->
```json
[{
  "Plan":{
    "ID":"<plan-id>",
    "Dialect":1,
    "Priority":1,
    "Predicate":"<diagnostic-expression>",
    "Verbs":["RequestReview"]
  },
  "Matched":true
}]
```

## `POST /v1/execute`

Required headers and their meanings:

| Header | Meaning |
| --- | --- |
| `Authorization` | Required bearer authentication. |
| `Idempotency-Key` | Required nonblank logical request key. The handler trims surrounding whitespace. It need not be a UUID. |
| `If-Match` | Optional generation-digest equality constraint. See below. |

Request fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `namespace` | string | Canonical execution namespace. Required unless a nonblank `universe` supplies it. |
| `universe` | string | Compatibility alias for `namespace`. New clients should use `namespace`. |
| `facts` | object | Facts compatible with the bundle's declarations. Required. |

The handler trims namespace and universe values.
If both are nonblank, they must agree. If both are blank, the request fails with 400.
Ruleset name and version come from the active generation, not request fields.
Fields such as `wait_mode`, `ruleset`, and `version` are unknown and return 400.

<!-- http-example: execute-request -->
```json
{"namespace":"docs","facts":{"order.id":"one","order.risk":90}}
```

A newly accepted response has this shape. All five fields are emitted:

<!-- http-example: accepted-response -->
```json
{
  "execution_id":"<execution-id>",
  "generation_digest":"<generation-digest>",
  "state":"accepted",
  "durably_accepted":true,
  "completed":false
}
```

| Field | Type | Meaning |
| --- | --- | --- |
| `execution_id` | string | Durable logical execution identity. Treat it as opaque. |
| `generation_digest` | string | The generation pinned to this execution, which can differ from the active generation on replay. |
| `state` | string | Observed durable execution state, not a promise about its later state. |
| `durably_accepted` | boolean | Admission has passed the intermediate `admitting` state. |
| `completed` | boolean | True only for successful `completed` state, not every terminal state. |

States are `admitting`, `accepted`, `running`, `completed`, `failed`, `blocked_unknown`, `blocked_fence`, `blocked_dependency`, and `blocked_compensation`.
`admitting` is an intermediate persistence state, not a successful durable-acceptance result.
A concurrent worker can change state before a response reaches the client.

The route always requests accepted-only execution. It does not invoke a terminal wait when a query, header, or timeout asks for one.
A replay of an unsuccessful terminal identity still returns HTTP 202 with its persisted disposition:

<!-- http-example: failed-replay-response -->
```json
{
  "execution_id":"<execution-id>",
  "generation_digest":"<generation-digest>",
  "state":"failed",
  "durably_accepted":true,
  "completed":false
}
```

### Identity and replay

Identity includes namespace, idempotency key, ruleset name, and version.
A matching retry returns the same execution identity. Conflicting content for that identity returns 409.
Do not generate a new key to hide an uncertain outcome from the same logical operation.
After a timeout or connection failure, retry the original identity because admission might already have persisted.

Facts are owned and normalized against the pinned generation for replay comparison.
Equivalent declared typed values and nested/dotted representations can match.
Do not assume every different number spelling or extra undeclared fact is equivalent.
An explicit dotted fact path wins over a nested representation of that path.

Replay across process replacement requires retained durable storage and the same ruleset name/version identity.
Changing the active bundle version changes the HTTP identity scope for the same namespace and key.
The HTTP body cannot select an older ruleset version or resume an arbitrary execution ID.

### `If-Match` is a generation constraint

Send one bare digest or one quoted digest, such as `If-Match: "DIGEST"`.
For a new identity, it must match the active generation.
For replay, it must match that identity's pinned historical generation.
When omitted, HTTP does not implicitly constrain replay to the active digest.

The current parser trims outer whitespace and leading/trailing double quotes.
It is not an RFC ETag parser. Weak tags, comma-separated lists, and `*` have no special meaning and do not match a digest.
An absent, blank, or empty quoted value supplies no constraint.
The response does not supply an `ETag` header. Read the explicit digest fields instead.

## Errors and retry decisions

Handler errors use one lowercase `error` string, without internal SQL, destination, or wrapped-cause details:

<!-- http-example: identity-error -->
```json
{"error":"idempotency identity conflicts with an existing request"}
```

| Status | Meaning and representative fixed message |
| --- | --- |
| `400` | Invalid JSON/request, absent key/namespace/facts, or alias disagreement. |
| `401` | `authentication failed` |
| `404` | `route not found`, or a typed `execution is not available` error. |
| `405` | `method not allowed`, with `Allow`. |
| `408` | `request canceled` |
| `409` | Identity conflict, `requested generation does not match`, optimistic state conflict, or a surfaced blocked execution error. |
| `413` | `request body exceeds 1 MiB` |
| `422` | A surfaced terminal execution error: `execution failed`. Accepted-only replay normally acknowledges that state with 202 instead. |
| `500` | `internal execution error` |
| `503` | `execution dependency is unavailable`, or `server is draining`. |
| `504` | `execution deadline exceeded` |

Specific 400 messages include `invalid request JSON`, `invalid execution request`, `Idempotency-Key header is required`, `namespace is required`, `namespace and universe disagree`, and `facts must be a JSON object`.
A scalar or array where an object is required can fail JSON decoding instead of returning the object-specific message.
Authentication and method checks occur before normal request decoding. Do not depend on one error when multiple parts of a request are invalid.

Retry transient errors using the same logical identity.
Resolve invalid input, identity conflicts, generation constraints, and blocked outcomes rather than blindly retrying them.
A transport error never proves that an external destination did not commit.

## Shutdown and connection bounds

Once HTTP draining begins, the gate rejects new handler entries with HTTP 503, including probes and unauthenticated routes.
Handlers already admitted receive the configured shutdown grace.
When grace expires, the daemon cancels requests and closes connections but still joins handlers before closing dependencies.
Noncooperative callbacks can extend total shutdown indefinitely.

The server configures a 10-second header-read timeout, 30-second read timeout, 35-second write timeout, and 60-second idle timeout.
Its configured header limit is 1 MiB. These connection bounds are not a terminal-execution deadline or completion guarantee.
See [Runtime Lifecycle](LIFECYCLE.md), [Runtime Configuration](RUNTIME_CONFIG.md), and [Runtime Guarantees](GUARANTEES.md).

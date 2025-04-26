# Basics

* **Verbs** are *codata*: the interpreter for each verb lives in code, so the *set* of verbs is intentionally closed and versioned (you add one rarely and ship a new binary).  
* **Effects** are output from the verb interpreters; they are *idempotent* and *atomic*.
* **Facts** describe domain reality; every new data source, event stream, or external system can add them. Trying to "freeze" that set would strangle the system's ability to model the evolving domain.  
Hence we need a **strongly-typed but *extensible*** facts model that can adapt to arbitrary domains.

# Facts

Canonical schema = “fact registry”  

* Author every fact field once in **protobuf (proto3)**, each in its own **namespace** (package).  
* Store descriptors in a **schema registry** (git repo or Buf registry).  
* Rule compiler loads *all* linked descriptors at build time and treats them as one giant symbol table.

```proto
// shopfloor.facts.customer.v1/customer.proto
message Customer {
  string code    = 1;   
  string program = 2;   
}

// shopfloor.facts.part.v1/part.proto
message Part {
  string  material   = 1;
  double  tolerance  = 2;  
  double  thickness  = 3;
  bool    itar       = 4;
}

// shopfloor.facts.sensors.v1/temp.proto (added later)
message Temperature {
  double spindle_c   = 1;
  double ambient_c   = 2;
}
```

Each package is versioned (`.v1`, `.v2`) **independently**.  
Adding new facts never breaks older rule files; they just ignore them.

## “Extensible” without `Any` abuse  
We still want *compile-time* type safety, so avoid untyped `map<string, Value>` or `google.protobuf.Any`.  
Instead:

* **Composition** – top-level `Facts` message is just a bag of *pointers* to module messages.

```proto
message Facts {
  shopfloor.facts.customer.v1.Customer customer  = 1;
  shopfloor.facts.part.v1.Part         part      = 2;
  shopfloor.facts.operation.v1.Op      operation = 3;
  shopfloor.facts.machine.v1.Machine   machine   = 4;
  shopfloor.facts.sensors.v1.Temperature temperature = 5;
  // …fields keep being added
}
```

*Unpopulated modules are simply `nil` at runtime.*


## Rule compiler: path resolution via descriptors  

```text
part.material      ✅  (string)
temperature.spindle_c > 50.0   ✅  (double)
foo.bar            ❌  unknown
```

1. Split the path (`temperature.spindle_c`).  
2. Start at `Facts` descriptor, walk through field names.  
3. Fail fast if a segment is unknown or type–operator mismatch (`part.tolerance in "7075"` ⇒ error).  
4. Emit a **required-fact manifest** for each rule set – CI fails if a service forgets to fill one.


## Client libraries auto-generated  
Every language that needs to publish facts imports the same protos:

```go
import factsv1 "github.com/yourorg/facts/go/v1"

facts := &factsv1.Facts{
    Customer: &factsv1.Customer{Code: "BOE"},
    Part:     &factsv1.Part{Material: "INCONEL", Thickness: 0.100},
    Temperature: &factsv1.Temperature{SpindleC: 58.3},
}
```

*JSON-only environments* use the canonical JSON schema auto-generated from the proto descriptors (`buf build --template jsonschema`).

## Source-of-truth data → facts (dbt style)  
*In dbt* you create one model per proto module:

```sql
-- models/fact_part.sql
select
  material,
  tolerance,
  thickness,
  itar_flag as itar
from erp_parts
```

A lightweight adapter turns each row into `part.v1.Part` and assembles the big `Facts` envelope.

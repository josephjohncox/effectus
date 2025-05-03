# Effectus Schema Files

This directory contains schema definitions for fact paths used in Effectus examples.

## Schema Format

Schema files are JSON documents containing an array of schema entries. Each entry defines the type for a specific fact path:

```json
[
  {
    "path": "customer.code",
    "type": {
      "PrimType": 1,
      "Name": "string"
    }
  },
  {
    "path": "part.tolerance",
    "type": {
      "PrimType": 3,
      "Name": "float"
    }
  }
]
```

## Type Definitions

The `PrimType` field uses the following values:

- `0`: Unknown
- `1`: String
- `2`: Int
- `3`: Float
- `4`: Bool
- `5`: List
- `6`: Map

For complex types like lists and maps, additional type information is provided:

```json
{
  "path": "order.items",
  "type": {
    "PrimType": 5,
    "Name": "list",
    "ListType": {
      "PrimType": 6,
      "Name": "map",
      "MapKeyType": {
        "PrimType": 1,
        "Name": "string"
      },
      "MapValType": {
        "PrimType": 1,
        "Name": "string"
      }
    }
  }
}
```

## Using Schema Files with effectusc

Use the `--schema` flag to specify schema files when compiling or type-checking:

```bash
effectusc --compile --schema ../examples/schemas/manufacturing.json my_flow.effx
```

Multiple schema files can be specified as a comma-separated list:

```bash
effectusc --compile --schema ../examples/schemas/manufacturing.json,../examples/common/facts/v1/schema.json my_flow.effx
```

This enables proper type checking of fact paths used in your rules and flows. 
# Example Schema Files

This directory contains JSON fact-path declarations for examples.

## Format

Each entry maps one fact path to an Effectus type:

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

The numeric `PrimType` values are compatibility encodings:

| Value | Type |
| --- | --- |
| `0` | unknown |
| `1` | string |
| `2` | int |
| `3` | float |
| `4` | bool |
| `5` | list |
| `6` | map |

New production schemas should use named declarations or protobuf sources where practical. Numeric compatibility values are easy to misuse.

Use `effectusc typecheck` with the applicable schema and verb declarations before you build a bundle.

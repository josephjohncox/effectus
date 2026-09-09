# Legacy BufIntegration contract

`schema.BufIntegration` is a local compatibility wrapper, not the runtime's schema registry.
New applications should use checked-in protobuf definitions and explicit Buf CLI commands.
The wrapper remains import-compatible. Its safety fixes do not make it a general JSON Schema compiler.

## Ownership and concurrency

- `NewBufIntegration(root)` requires a nonblank workspace path and stores its absolute path.
  It reads `buf.yaml` but does not create directories or write configuration.
- Registration methods copy their inputs. Getters and lists return deep copies.
  Caller changes after a call cannot alter a stored schema.
  Do not mutate an input while its registration call reads it.
- Copies preserve JSON-shaped containers and numeric types. Unsupported values, non-finite numbers, and excessive nesting fail.
- Registration, generation, and validation serialize within one integration.
  Getters and lists can run concurrently with those operations.
- Use one integration per workspace. Do not concurrently modify its configuration or run external writers.
  Create a new integration after changing the module configuration.
- Methods that accept a context reject nil or already-canceled contexts before work.
  Nil and zero integrations reject mutations. Their getters return not-found and their lists return empty maps.

## Protobuf files

Registration supports flat scalar fields: `string`, `integer`, `number`, and `boolean`.
A field can use its type string or an object containing that `type`.
Other metadata remains metadata. The wrapper does not enforce JSON Schema constraints, privacy rules, retention rules, or capabilities.

The wrapper checks schema and field names before file writes.
New files assign field numbers in sorted field-name order.
Installation confines paths to the chosen workspace and rejects symlink escapes.
It writes and syncs a temporary file, then installs the file without replacing an existing target.

An identical existing file is reusable. A different definition fails, even after restarting the integration.
The wrapper does not renumber or overwrite existing definitions.
Preserve existing field numbers in an explicit protobuf migration instead.
Changing only registry metadata does not change the protobuf definition.

Without module configuration, registration uses the legacy `proto/` directory.
A v2 configuration can select one relative module path inside the workspace.
Multiple modules require direct Buf CLI use.
The wrapper does not invent a module name, dependencies, or a remote registry configuration.

## Generation and validation

`GenerateCode` uses `buf.gen.yaml` and the `buf` executable on `PATH`.
It accepts v1 or v2 plugin output configuration.
Declared output paths must remain inside the workspace and must not use symlink aliases.
`GeneratedFiles` lists Go protobuf files present in those directories, including unchanged files.
It is not an audit of files written by the command.

`ValidateSchemas` runs `buf breaking --against .git#branch=main`, then `buf lint`.
Either command's failure returns an invalid result and a non-nil error.
Cancellation stops the remaining sequence and stays visible through `errors.Is`.
Diagnostics retain up to 256 KiB of combined command output.

The workspace, Buf executable, and plugins must be trusted.
This wrapper is not a process sandbox.
Context cancellation stops the direct command. A misbehaving plugin can outlive that process.
Do not reuse the workspace while such a plugin remains active.

## Migration path

1. Keep the protobuf definitions under version control.
2. Preserve existing service names, message names, and field numbers.
3. Configure module paths and language outputs explicitly.
4. Run Buf lint, breaking checks, and generation as build steps.
5. Use `bundle`, `embedded`, and checked-generation APIs for Effectus execution.

No import removal or wire-identity removal is part of this change.

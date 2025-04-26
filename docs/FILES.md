## File-type contract

| Dialect      | Extension | Top-level keyword | Pipeline                             |
| ------------ | --------- | ----------------- | ------------------------------------ |
| Simple list  | `.eff`    | `rule`            | `effectusc list …`  → `[]Effect`     |
| Monadic flow | `.effx`   | `flow`            | `effectusc flow …` → `Program[Unit]` |

*CLI refuses to compile a file whose extension & keyword mismatch.*

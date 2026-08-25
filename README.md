# protoc-gen-go-plainjson

[![CI](https://github.com/gopherex/protoc-gen-go-plainjson/actions/workflows/ci.yml/badge.svg)](https://github.com/gopherex/protoc-gen-go-plainjson/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gopherex/protoc-gen-go-plainjson.svg)](https://pkg.go.dev/github.com/gopherex/protoc-gen-go-plainjson)
[![Go Report Card](https://goreportcard.com/badge/github.com/gopherex/protoc-gen-go-plainjson)](https://goreportcard.com/report/github.com/gopherex/protoc-gen-go-plainjson)
[![Go](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A `protoc` plugin that generates **one-way, reflection-free JSON marshalers** for
protobuf messages, driven by options declared in your `.proto`.

The point is *flattening*: turning a deeply nested, branchy protobuf message into
an almost-flat JSON object, merging fields from many levels — and from mutually
exclusive `oneof` branches — into a single set of top-level keys.

The output is intentionally **not** reversible. There is no `Unmarshal`. Nesting,
branch identity and skipped fields are lost by design.

```proto
message Process {
  option (plainjson.message).generate = true;
  oneof os {
    Linux   linux   = 1;
    Windows windows = 2;
  }
}
message Linux   { int32 pid = 1; string cgroup = 2; }
message Windows { int32 pid = 1; string sid    = 2; }
```

```go
Process{Os: &Process_Linux{&Linux{Pid: 4242, Cgroup: "/sys/fs/x"}}}
// {"pid":4242,"cgroup":"/sys/fs/x"}

Process{Os: &Process_Windows{&Windows{Pid: 77, Sid: "S-1-5-18"}}}
// {"pid":77,"sid":"S-1-5-18"}
```

Both branches contribute the key `pid`. That is not a conflict — it is the goal.

## Why

Protobuf models a domain: deep, branchy, precise. A lot of what consumes that
data wants the opposite — one row, one flat object, one set of columns:

- **log and event pipelines** where every nested field has to become a top-level
  key before it is searchable;
- **columnar stores and warehouses** (ClickHouse, BigQuery, Parquet) that take a
  flat record per row;
- **SIEM and analytics schemas** — ECS, OTel attributes — that prescribe flat key
  names your proto does not match;
- **webhooks and public payloads** where you want to expose a curated, stable set
  of fields rather than your internal message shape.

Doing that by hand means writing a mapper per message and keeping it in step with
the schema, or paying for a reflective walk on every event. This plugin makes the
mapping part of the `.proto` — the flattening rules live next to the fields they
describe, and the traversal is resolved once, at generation time, into
straight-line Go.

```proto
Linux   linux   = 1;                                        // linux.pid    -> "pid"
Windows windows = 2;                                        // windows.pid  -> "pid"
Debug   debug   = 3 [(plainjson.field).omit = true];        // dropped
Money   total   = 4 [(plainjson.field).pick = "amount"];    // {amount,cur} -> 1000
```

The result is [~4× faster than the same flattening written by hand over
descriptors, and ~20× faster than the protojson-then-collapse-a-map
approach](#performance), with 9 allocations instead of 229.

## What it is not

- **Not a protojson replacement.** For a faithful, reversible protobuf ↔ JSON
  mapping, use `protojson`. This plugin throws information away on purpose.
- **Not a transport codec.** Nothing can read the output back into a message.
- **Not a query language.** Rules are declarative and static: paths, keys,
  cardinality, precedence. There are no expressions or conditionals.

---

## Table of contents

- [Why](#why)
- [Install](#install)
- [Usage](#usage)
- [Generated API](#generated-api)
- [Core model](#core-model)
- [Option inheritance](#option-inheritance)
- [File options](#file-options)
- [Message options](#message-options)
- [Oneof options](#oneof-options)
- [Field options](#field-options)
- [Enum options](#enum-options)
- [Flatten modes](#flatten-modes)
- [Key naming](#key-naming)
- [Oneof handling](#oneof-handling)
- [Cardinality](#cardinality)
- [Picking, lifting, omitting](#picking-lifting-omitting)
- [Merge rules](#merge-rules)
- [Constants](#constants)
- [Key collisions](#key-collisions)
- [Scalars and well-known types](#scalars-and-well-known-types)
- [Empty values and presence](#empty-values-and-presence)
- [Generation-time validation](#generation-time-validation)
- [Runtime errors](#runtime-errors)
- [Worked example](#worked-example)
- [Performance](#performance)
- [Scope and limitations](#scope-and-limitations)
- [Development](#development)

---

## Install

```bash
go install github.com/gopherex/protoc-gen-go-plainjson@latest
```

This puts `protoc-gen-go-plainjson` in `$(go env GOBIN)` (or `$(go env GOPATH)/bin`) —
make sure that directory is on `PATH`.

Add the runtime module to your project; generated code imports it:

```bash
go get github.com/gopherex/protoc-gen-go-plainjson
```

From source:

```bash
git clone https://github.com/gopherex/protoc-gen-go-plainjson
cd protoc-gen-go-plainjson
make build          # -> bin/protoc-gen-go-plainjson
```

## Usage

The plugin runs **alongside** `protoc-gen-go`: generate the standard `*.pb.go`
first, then `protoc-gen-go-plainjson` writes `*.pb.plainjson.go` next to it in the
same Go package.

Import the options in your proto:

```proto
syntax = "proto3";
package myapp;

import "plainjson/plainjson.proto";
```

### With protoc

```bash
protoc \
  -I . -I $(go env GOPATH)/pkg/mod/github.com/gopherex/protoc-gen-go-plainjson@latest \
  --go_out=. --go_opt=paths=source_relative \
  --go-plainjson_out=. --go-plainjson_opt=paths=source_relative \
  your.proto
```

### With buf

```yaml
# buf.gen.yaml
version: v2
plugins:
  - local: protoc-gen-go
    out: .
    opt: paths=source_relative
  - local: protoc-gen-go-plainjson
    out: .
    opt: paths=source_relative
```

### With easyp

```yaml
generate:
  plugins:
    - name: go
      out: .
      opts: { paths: source_relative }
    - path: ./bin/protoc-gen-go-plainjson
      out: .
      opts: { paths: source_relative }
```

### Plugin flags

| flag | meaning |
|---|---|
| `paths=source_relative` | standard protoc-gen-go path mode |
| `default_flatten=deep\|shallow\|none` | overrides the built-in default for files that set no `(plainjson.file).flatten` |
| `default_collision_policy=ignore\|generate\|runtime` | same, for the collision policy |
| `strict` | shorthand for `default_collision_policy=generate`; useful in CI |

Flags are the lowest precedence — any option in the `.proto` wins.

---

## Generated API

For every message with `(plainjson.message).generate = true` (or under
`(plainjson.file).generate_all = true`):

```go
// Streaming encode into a jx encoder. The primary entry point.
func (m *Process) EncodePlainJSON(e *jx.Encoder) error

// Convenience wrapper over EncodePlainJSON.
func (m *Process) MarshalPlainJSON() ([]byte, error)
```

With `(plainjson.message).override_marshal_json = true` the plugin additionally
emits the standard interface method, so `json.Marshal(msg)` produces the flat
form too:

```go
func (m *Process) MarshalJSON() ([]byte, error)
```

Notes:

- A `nil` receiver marshals to `null`.
- Both methods return `error` because collision policy `ERROR_RUNTIME` and merge
  conflict mode `ERROR` can fail at encode time. With the default policy they
  never return a non-nil error.
- No `Unmarshal`, no `Decode`, no `UnmarshalJSON` is generated, ever. The
  transformation is lossy and one-way.
- Messages **not** marked `generate` still take part in flattening when reached
  from a generated message. `generate` only decides whose methods are emitted.

---

## Core model

At generation time the plugin walks the message tree and builds a **flatten plan**:
an ordered list of entries

```
(json key, source path, encoder, emit condition)
```

The generated code is a direct `jx` writer over that plan. There is no reflection,
no intermediate `map[string]any`, and no descriptor lookup at runtime.

### Traversal order

Pre-order depth-first, following **declaration order** in the `.proto` file. This
order defines:

- the order of keys in the output JSON;
- which entry is "first" for collision policy `IGNORE` with `COLLISION_WINS_FIRST`.

### Leaves

A path stops descending — becomes a leaf, producing exactly one key — when it hits:

- a scalar, `string`, `bytes` or enum;
- a well-known type (`Timestamp`, `Duration`, wrappers, `Struct`, `Value`,
  `ListValue`, `Empty`, `FieldMask`, `Any`) — WKTs are never flattened, they encode
  as their protojson form;
- a repeated or map field whose `cardinality` keeps it a collection;
- any message field whose boundary `flatten` resolves to `NONE`.

### Exclusivity

Two plan entries are **mutually exclusive** when at most one of them can produce a
value in a single encode. This happens when they live in different branches of the
same `oneof`, or in different members of a declared `exclusive_groups` entry.
Mutually exclusive entries are allowed to share a JSON key and are never reported
as collisions. This is what makes `Linux.pid` and `Windows.pid` collapse into one
`pid`.

---

## Option inheritance

Every formatting option resolves through this chain, nearest wins:

```
plugin flag  →  file option  →  message option  →  oneof option  →  field option
```

An unset enum (value `0`, `*_UNSPECIFIED`) and an unset `optional bool` mean
"inherit".

**Formatting options propagate down the subtree.** Setting
`key_case: KEY_CASE_SNAKE` on a message field snake-cases every key lifted out of
it, at any depth, without touching its siblings. Same for `key_from`, `emit_empty`,
`cardinality`, and every scalar format option.

`prefix` and `suffix` propagate too, but **accumulate** instead of overriding — see
[Key naming](#key-naming).

**`flatten` does not propagate.** On a field it is a *boundary* decision only: is
*this* field inlined into its parent, or does it keep its own key. What happens
below that boundary is decided by the flatten mode of the field's own message type
(or, failing that, the file). This is what makes partial flattening composable —
see [Flatten modes](#flatten-modes). To bound the depth of a subtree from the
outside, use `max_depth` on the field.

---

## File options

```proto
option (plainjson.file) = {
  generate_all: true,
  flatten: FLATTEN_MODE_DEEP,
  key_from: KEY_FROM_LEAF,
  key_case: KEY_CASE_CAMEL,
  collision_policy: COLLISION_POLICY_IGNORE,
  collision_wins: COLLISION_WINS_FIRST
};
```

| option | type | default | meaning |
|---|---|---|---|
| `generate_all` | bool | false | generate marshalers for every message in the file; per-message `generate: false` still opts out |
| `override_marshal_json` | bool | false | default for `MessageOptions.override_marshal_json` |
| `flatten` | `FlattenMode` | `DEEP` | default flatten mode for the file |
| `key_from` | `KeyFrom` | `LEAF` | default key derivation |
| `key_case` | `KeyCase` | `CAMEL` | default key casing |
| `collision_policy` | `CollisionPolicy` | `IGNORE` | default collision policy |
| `collision_wins` | `CollisionWins` | `FIRST` | default winner under `IGNORE` |
| `max_depth` | optional uint32 | 0 (unlimited) | default flatten depth limit |
| `emit_empty` | optional bool | false | default for emitting zero/empty values |
| `cardinality` | `Cardinality` | `KEEP` | default collection handling |
| `join_separator` | string | `","` | default separator for `CARDINALITY_JOIN` |
| `index_separator` | string | `"_"` | default separator for `CARDINALITY_INDEXED` |
| `enum_format` | `EnumFormat` | `NAME` | default enum representation |
| `int64_format` | `Int64Format` | `STRING` | default 64-bit integer representation |
| `bytes_format` | `BytesFormat` | `BASE64` | default `bytes` representation |
| `time_format` | `TimeFormat` | `RFC3339` | default `Timestamp` representation |
| `duration_format` | `DurationFormat` | `PROTOJSON` | default `Duration` representation |

`FLATTEN_MODE_DEEP` is the built-in default: with no options at all beyond
`generate`, a message is fully flattened.

---

## Message options

```proto
message Event {
  option (plainjson.message) = {
    generate: true,
    flatten: FLATTEN_MODE_DEEP,
    collision_policy: COLLISION_POLICY_ERROR_RUNTIME
  };
  ...
}
```

Every file option above (except `generate_all`) is also a message option and
overrides the file value. In addition:

| option | type | default | meaning |
|---|---|---|---|
| `generate` | optional bool | inherited from `generate_all` | emit marshalers for this message; an explicit `false` opts out of `generate_all` |
| `override_marshal_json` | optional bool | inherited | also emit `MarshalJSON()` |
| `merge` | repeated `MergeRule` | — | coalesce several source paths into one key, see [Merge rules](#merge-rules) |
| `constants` | repeated `Constant` | — | inject fixed key/value pairs, see [Constants](#constants) |
| `exclusive_groups` | repeated `ExclusiveGroup` | — | declare non-`oneof` fields mutually exclusive, see [Key collisions](#key-collisions) |

---

## Oneof options

Declared inside the `oneof` block:

```proto
oneof os {
  option (plainjson.oneof) = {
    mode: ONEOF_MODE_TAGGED,
    discriminator: "os_kind"
  };
  Linux   linux   = 1;
  Windows windows = 2;
}
```

| option | type | default | meaning |
|---|---|---|---|
| `mode` | `OneofMode` | `INLINE` when the enclosing flatten mode is `DEEP`/`SHALLOW`, `BRANCH_KEY` when it is `NONE` | how the oneof is rendered, see [Oneof handling](#oneof-handling) |
| `discriminator` | string | `"type"` | key holding the branch tag in `TAGGED` / `DISCRIMINATOR_ONLY` |
| `value_key` | string | the oneof name | key holding the value in `SINGLE_KEY` |
| `omit_if_unset` | optional bool | true | when no branch is set, write nothing (`false` writes `null` under the discriminator/value key) |
| plus every formatting option | | inherited | applies to all branches of this oneof |

---

## Field options

```proto
Linux linux = 1 [(plainjson.field) = {
  flatten: FLATTEN_MODE_DEEP,
  prefix: "linux_"
}];
```

### Structural

| option | type | default | meaning |
|---|---|---|---|
| `omit` | bool | false | drop the field and its whole subtree |
| `name` | string | — | replace this path segment's name (accumulated prefixes still apply) |
| `prefix` | string | — | prepended to every key produced by this subtree |
| `suffix` | string | — | appended to every key produced by this subtree |
| `flatten` | `FlattenMode` | the field type's own mode | **boundary only**: `DEEP`/`SHALLOW` inline this message field into its parent, `NONE` keeps it as its own nested key. Does not propagate below the boundary |
| `pick` | string | — | dot path inside this field; the field is replaced by that single value |
| `lift` | repeated `Lift` | — | hoist selected dot paths out of the subtree under given keys |
| `tag` | string | branch field name | discriminator value when this field is a `oneof` branch |
| `max_depth` | optional uint32 | inherited | depth limit for this subtree; propagates, unlike `flatten`. An explicit `0` lifts an inherited bound |

### Cardinality

| option | type | default | meaning |
|---|---|---|---|
| `cardinality` | `Cardinality` | inherited (`KEEP`) | how a repeated/map field collapses, see [Cardinality](#cardinality) |
| `join_separator` | string | inherited | separator for `CARDINALITY_JOIN` |
| `index_separator` | string | inherited | separator for `CARDINALITY_INDEXED` |

### Value formatting

| option | type | default | meaning |
|---|---|---|---|
| `key_from` | `KeyFrom` | inherited | key derivation for this subtree |
| `key_case` | `KeyCase` | inherited | key casing for this subtree |
| `emit_empty` | optional bool | inherited | emit the key even when the value is empty/zero |
| `enum_format` | `EnumFormat` | inherited | `NAME` or `NUMBER` |
| `int64_format` | `Int64Format` | inherited | `STRING` or `NUMBER` |
| `bytes_format` | `BytesFormat` | inherited | `BASE64`, `BASE64_URL`, `HEX`, `ARRAY` |
| `time_format` | `TimeFormat` | inherited | `RFC3339`, `UNIX_SECONDS`, `UNIX_MILLI`, `UNIX_MICRO`, `UNIX_NANO` |
| `duration_format` | `DurationFormat` | inherited | `PROTOJSON`, `SECONDS`, `MILLIS`, `NANOS` |

---

## Enum options

```proto
enum Severity {
  option (plainjson.enum).format = ENUM_FORMAT_NUMBER;
  SEVERITY_UNSPECIFIED = 0;
  SEVERITY_LOW  = 1 [(plainjson.enum_value).name = "low"];
  SEVERITY_HIGH = 2 [(plainjson.enum_value).name = "high"];
}
```

| option | level | meaning |
|---|---|---|
| `(plainjson.enum).format` | enum | default representation for all fields of this enum type |
| `(plainjson.enum).strip_prefix` | enum | strip the `SEVERITY_` prefix from emitted names |
| `(plainjson.enum_value).name` | enum value | override the emitted name for one value |
| `(plainjson.enum_value).omit` | enum value | emit nothing when the field holds this value |

Precedence for an enum field is field option, then the enum type's own
`format`, then whatever the message or file set.

---

## Flatten modes

```proto
message Wrap {
  option (plainjson.message).generate = true;
  string id  = 1;
  Outer  out = 2;
}
message Outer { string a = 1; Inner in = 2; }
message Inner { string b = 1; }
```

Input: `Wrap{Id: "w", Out: &Outer{A: "x", In: &Inner{B: "y"}}}`

| mode | result |
|---|---|
| `FLATTEN_MODE_DEEP` (default) | `{"id":"w","a":"x","b":"y"}` |
| `FLATTEN_MODE_SHALLOW` | `{"id":"w","a":"x","in":{"b":"y"}}` |
| `FLATTEN_MODE_NONE` | `{"id":"w","out":{"a":"x","b":"y"}}` — `out` keeps its key, its interior still follows `Outer` |

`NONE` on a message is a boundary decision about *its own* fields; the protojson
shape needs the nested types to say `NONE` too, which is what a file-level
`flatten: FLATTEN_MODE_NONE` does in one line.

Mixing per field:

```proto
message Wrap {
  option (plainjson.message).flatten = FLATTEN_MODE_NONE;  // nested by default
  string id  = 1;
  Outer  out = 2 [(plainjson.field).flatten = FLATTEN_MODE_DEEP]; // except this one
}
// {"id":"w","a":"x","b":"y"}
```

The reverse — keep one subtree behind its own key inside a deep-flattened
message. What that key holds depends on the field type's own mode, not on the
boundary:

```proto
message Wrap {
  option (plainjson.message).flatten = FLATTEN_MODE_DEEP;
  Outer out = 2 [(plainjson.field).flatten = FLATTEN_MODE_NONE];
}
message Outer { string a = 1; Inner in = 2; }          // no option: DEEP applies
// {"id":"w","out":{"a":"x","b":"y"}}
```

For a fully protojson-shaped subtree, say so on the type:

```proto
message Outer {
  option (plainjson.message).flatten = FLATTEN_MODE_NONE;
  string a = 1;
  Inner in = 2;
}
// {"id":"w","out":{"a":"x","in":{"b":"y"}}}
```

### Boundaries compose

`flatten` on a field answers exactly one question: **is this field inlined into its
parent?** It says nothing about what happens further down. Below the boundary the
field's own message type decides — which is how you get a nested key whose contents
are themselves flat:

```proto
message Wrap {
  option (plainjson.message) = {generate: true, flatten: FLATTEN_MODE_DEEP};
  string id  = 1;
  Outer  out = 2 [(plainjson.field).flatten = FLATTEN_MODE_NONE];  // keeps its key
}
message Outer {
  option (plainjson.message).flatten = FLATTEN_MODE_DEEP;          // but is flat inside
  string a  = 1;
  Inner  in = 2;
}
message Inner { string b = 1; }
```

```json
{"id":"w","out":{"a":"x","b":"y"}}
```

Flat header, one grouped object, flat inside that object. The same message type can
be inlined at one use site and kept nested at another — the boundary is a property
of the field, the interior is a property of the type:

```proto
message Event {
  option (plainjson.message).generate = true;
  Outer inlined = 1 [(plainjson.field).flatten = FLATTEN_MODE_DEEP];
  Outer grouped = 2 [(plainjson.field).flatten = FLATTEN_MODE_NONE];
}
// {"a":"x","b":"y","grouped":{"a":"x2","b":"y2"}}
```

To bound how deep a subtree flattens from the outside — without changing its
type's options — use `max_depth` on the field.

### Depth limit

`max_depth` counts message levels descended from the generated message. When the
limit is hit, the remaining subtree is written as a nested object instead of being
flattened further.

```proto
option (plainjson.message) = {generate: true, max_depth: 1};
// {"id":"w","a":"x","in":{"b":"y"}}
```

`max_depth` is also the escape hatch for self-recursive messages: a type that
flattens into itself would cycle, and a bound at the use site — or
`FLATTEN_MODE_NONE` on the type — makes it legal. The cycle check runs over the
plan of each generated message, so the bound only has to exist on the paths
actually reached. See [Generation-time validation](#generation-time-validation).

---

## Key naming

### `key_from`

```proto
message Event {
  option (plainjson.message).generate = true;
  Process process = 1;
}
message Process { Linux linux = 1; }
message Linux   { int32 pid = 1; string cgroup_path = 2; }
```

| `key_from` | `key_case` | result |
|---|---|---|
| `LEAF` (default) | `CAMEL` | `{"pid":4242,"cgroupPath":"/sys/x"}` |
| `LEAF` | `SNAKE` | `{"pid":4242,"cgroup_path":"/sys/x"}` |
| `LEAF` | `ORIGINAL` | `{"pid":4242,"cgroup_path":"/sys/x"}` |
| `PATH` | `CAMEL` | `{"processLinuxPid":4242,"processLinuxCgroupPath":"/sys/x"}` |
| `PATH` | `SNAKE` | `{"process_linux_pid":4242,"process_linux_cgroup_path":"/sys/x"}` |

`KEY_FROM_PATH` joins every field name along the path from the generated message
down to the leaf. It removes most collisions by construction, at the cost of longer
keys. `KEY_FROM_LEAF` is the flattening default and the reason collisions are a
first-class concept here.

Mix them per subtree:

```proto
  Process process = 1 [(plainjson.field).key_from = KEY_FROM_PATH];
```

### `name`

Renames one path segment. On a leaf it renames the key; on a message field it
changes the segment used by `KEY_FROM_PATH`.

```proto
message Linux {
  int32  pid         = 1 [(plainjson.field).name = "process_id"];  // -> "processId"
  string cgroup_path = 2;
}
```

### `prefix` / `suffix`

Applied to every key produced by the subtree, and **accumulated** across levels
(outer prefix first):

```proto
message Order {
  option (plainjson.message).generate = true;
  Customer shipping = 1 [(plainjson.field).prefix = "ship_"];
  Customer billing  = 2 [(plainjson.field).prefix = "bill_"];
}
message Customer { string name = 1; Address addr = 2 [(plainjson.field).prefix = "addr_"]; }
message Address  { string city = 1; }
```

```json
{"ship_name":"Ann","ship_addr_city":"Berlin","bill_name":"Bob","bill_addr_city":"Rome"}
```

Prefixes are applied to the raw segment names *before* casing, so
`prefix: "ship_"` with `key_case: KEY_CASE_CAMEL` yields `shipName`, not
`ship_name`. Use `key_case: KEY_CASE_ORIGINAL` (or a prefix without an underscore)
when you want the literal form.

---

## Oneof handling

```proto
message Event {
  option (plainjson.message).generate = true;
  string id = 1;
  oneof payload {
    option (plainjson.oneof) = {mode: ONEOF_MODE_TAGGED, discriminator: "type"};
    Click  click  = 10;
    Scroll scroll = 11 [(plainjson.field).tag = "SCROLL_EV"];
  }
}
message Click  { int32 x = 1; int32 y = 2; }
message Scroll { int32 dy = 1; }
```

Input: `Event{Id: "e1", Payload: &Event_Click{&Click{X: 10, Y: 20}}}`

| `mode` | result |
|---|---|
| `ONEOF_MODE_INLINE` (default under flattening) | `{"id":"e1","x":10,"y":20}` |
| `ONEOF_MODE_BRANCH_KEY` (protojson shape) | `{"id":"e1","click":{"x":10,"y":20}}` |
| `ONEOF_MODE_TAGGED` | `{"id":"e1","type":"click","x":10,"y":20}` |
| `ONEOF_MODE_SINGLE_KEY` (`value_key: "payload"`) | `{"id":"e1","payload":{"x":10,"y":20}}` |
| `ONEOF_MODE_DISCRIMINATOR_ONLY` | `{"id":"e1","type":"click"}` |
| `ONEOF_MODE_OMIT` | `{"id":"e1"}` |

With the `Scroll` branch set, `TAGGED` gives `{"id":"e1","type":"SCROLL_EV","dy":5}` —
`tag` overrides the discriminator value for that branch.

A scalar branch under `INLINE` keeps its own field name as the key:

```proto
oneof id {
  option (plainjson.oneof).mode = ONEOF_MODE_INLINE;
  string uuid = 1;
  int64  seq  = 2;
}
// {"uuid":"7f3a..."}   or   {"seq":"91"}
```

To collapse differently-named scalar branches into one key, use
[Merge rules](#merge-rules) or `name`:

```proto
oneof id {
  string uuid = 1 [(plainjson.field).name = "id"];
  int64  seq  = 2 [(plainjson.field).name = "id"];
}
// {"id":"7f3a..."}   or   {"id":"91"}
```

This is legal with no collision diagnostics: the branches are mutually exclusive.

---

## Cardinality

`cardinality` describes what happens to a field that holds **more than one value**.
It is a property of cardinality, not of the field type, so the same option covers
`repeated` and `map` alike.

```proto
message Metrics {
  option (plainjson.message).generate = true;
  string name = 1;
  repeated Sample samples = 2;
  map<string, string> labels = 3;
}
message Sample { int64 value = 1; string unit = 2; }
```

Input:

```go
Metrics{
  Name:    "http_reqs",
  Samples: []*Sample{{Value: 1, Unit: "ms"}, {Value: 2, Unit: "ms"}},
  Labels:  map[string]string{"env": "prod", "region": "eu"},
}
```

| `cardinality` | `samples` | `labels` |
|---|---|---|
| `KEEP` (default) | `"samples":[{"value":"1","unit":"ms"},{"value":"2","unit":"ms"}]` | `"labels":{"env":"prod","region":"eu"}` |
| `FIRST` | `"samples":{"value":"1","unit":"ms"}` | `"labels":"prod"` |
| `LAST` | `"samples":{"value":"2","unit":"ms"}` | `"labels":"eu"` |
| `COUNT` | `"samples":2` | `"labels":2` |
| `JOIN` | needs a scalar — combine with `pick` | `"labels":"prod,eu"` |
| `INDEXED` | `"samples_0_value":"1","samples_0_unit":"ms","samples_1_value":"2",…` | `"labels_0":"prod","labels_1":"eu"` |
| `EXPLODE` | `"value":"1","unit":"ms"` then `"value":"2"…` → collision policy decides | `"env"…` keys flattened, unprefixed |
| `KEYS` (map only) | generation error | `"labels":["env","region"]` |
| `VALUES` (map only) | generation error | `"labels":["prod","eu"]` |
| `INLINE_KEYS` (map only) | generation error | `"env":"prod","region":"eu"` |

Notes:

- Map iteration is **sorted by key** so output is deterministic. `FIRST`/`LAST`
  therefore mean lowest/highest key.
- `JOIN` requires scalar elements. Combine with `pick` to take a scalar out of
  message elements:

  ```proto
  repeated Sample samples = 2 [(plainjson.field) = {
    pick: "value", cardinality: CARDINALITY_JOIN, join_separator: ";"
  }];
  // "samples":"1;2"
  ```

- `INDEXED` builds keys as `<key><index_separator><i>` and, for message elements,
  continues flattening below that prefix.
- `EXPLODE` merges every element's keys into the parent object with no index. Use
  it for collections that are 0..1 in practice; otherwise elements overwrite each
  other according to [collision policy](#key-collisions).
- `INLINE_KEYS` promotes **map keys to JSON keys**. Since those keys are only known
  at runtime, collisions with static keys can only be caught by
  `COLLISION_POLICY_ERROR_RUNTIME`:

  ```proto
  map<string, string> labels = 3 [(plainjson.field) = {
    cardinality: CARDINALITY_INLINE_KEYS, prefix: "lbl_"
  }];
  // {"name":"http_reqs","lbl_env":"prod","lbl_region":"eu"}
  ```

  Non-string map keys are stringified the same way protojson does.

---

## Picking, lifting, omitting

### `omit`

```proto
Debug debug = 9 [(plainjson.field).omit = true];
```

The field and everything under it disappear from the plan.

### `pick`

Replaces a message field with a single value from inside it. The path is
dot-separated field names, and it must resolve to a leaf.

```proto
message Order {
  option (plainjson.message).generate = true;
  Money total = 1 [(plainjson.field).pick = "amount"];
  User  buyer = 2 [(plainjson.field) = {pick: "profile.contact.email", name: "email"}];
}
message Money { int64 amount = 1; string currency = 2; }
```

```json
{"total":"1000","email":"ann@example.com"}
```

Without `name`, the key comes from the field itself (`total`), not from the picked
leaf. Set `name` when you want the leaf's name.

### `lift`

Hoists several specific paths out of a subtree, under keys you choose. Everything
else in that subtree is dropped.

```proto
message Report {
  option (plainjson.message).generate = true;
  Deep deep = 1 [(plainjson.field) = {
    lift: [
      {path: "a.b.id",    as: "id"},
      {path: "a.b.title", as: "title"},
      {path: "a.stats.total"}
    ]
  }];
}
```

```json
{"id":"r-7","title":"Weekly","total":42}
```

An entry with no `as` uses the leaf name from the path. `lift` and `pick` are
mutually exclusive on the same field.

---

## Merge rules

`pick`/`lift` are per-field. **`merge` is per-message and cross-subtree**: it
collects several source paths into one key, in priority order.

Use it when the same logical value lives at different places under different names,
and the sources are not naturally exclusive.

```proto
message Process {
  option (plainjson.message) = {
    generate: true,
    merge: [
      {key: "pid",  from: ["container.runtime.host_pid", "linux.pid", "windows.process_id"]},
      {key: "user", from: ["linux.ucred.uid", "windows.sid"], on_conflict: MERGE_CONFLICT_ERROR},
      {key: "started_at", from: ["linux.start_time", "windows.create_time"],
       time_format: TIME_FORMAT_UNIX_MILLI}
    ]
  };
  Container container = 1;
  oneof os {
    Linux   linux   = 2;
    Windows windows = 3;
  }
}
```

```json
{"pid":4242,"user":"1000","startedAt":1787000000123,"…":"other flattened keys"}
```

| `MergeRule` field | type | default | meaning |
|---|---|---|---|
| `key` | string | required | the JSON key produced |
| `from` | repeated string | required | dot paths, in priority order |
| `on_conflict` | `MergeConflict` | `FIRST_NON_EMPTY` | `FIRST_NON_EMPTY`, `LAST_NON_EMPTY`, or `ERROR` when more than one source is non-empty |
| `emit_empty` | optional bool | inherited | write the key even when every source is empty |
| formatting options | | inherited | `enum_format`, `int64_format`, `bytes_format`, `time_format`, `duration_format`, `cardinality`, … |

Semantics:

- Paths listed in `from` are **removed from the normal flatten plan** — they only
  reach the output through the rule.
- Merged keys are written **after** the flatten plan, in rule declaration order.
  The full emission order of an object is: constants, then the flatten plan in
  traversal order, then merged keys.
- `FIRST_NON_EMPTY` scans `from` in order and takes the first source that has a
  value under the [presence rules](#empty-values-and-presence).
- `ERROR` returns a runtime `*plainjsonpb.MergeConflictError` if two sources are
  non-empty at once.
- All sources must encode to a compatible JSON type; mixing e.g. a string and an
  object is a generation error unless every source is picked down to a scalar.

---

## Constants

Inject fixed keys that have no protobuf source. Values are raw JSON.

```proto
message Event {
  option (plainjson.message) = {
    generate: true,
    constants: [
      {key: "schema_version", value_json: "\"v3\""},
      {key: "source",         value_json: "\"agent\""},
      {key: "flags",          value_json: "[\"flat\",\"lossy\"]"}
    ]
  };
}
```

```json
{"schemaVersion":"v3","source":"agent","flags":["flat","lossy"],"…":"…"}
```

Constants are emitted first, in declaration order, and participate in collision
detection like any other key. Set `raw_key: true` on a constant to bypass
`key_case` and use `key` verbatim.

---

## Key collisions

Flattening deliberately merges keys. A collision is only a problem when two
entries that **can both produce a value in the same encode** claim the same key.

### Not a collision

- Different branches of the same `oneof`.
- Different members of a declared `exclusive_groups` entry.
- Sources of the same `merge` rule.

```proto
message Runtime {
  option (plainjson.message) = {
    generate: true,
    exclusive_groups: [{fields: ["docker", "podman", "containerd"]}]
  };
  Docker      docker      = 1;   // has .id
  Podman      podman      = 2;   // has .id
  Containerd  containerd  = 3;   // has .id
}
// {"id":"c0ffee"} — whichever one is set
```

`exclusive_groups` lists field names of the message (dot paths are allowed for
deeper fields). The plugin trusts the declaration; if two members are set at once,
the winner follows `collision_wins`.

### Is a collision

```proto
message Event {
  option (plainjson.message).generate = true;
  Linux linux = 1;   // linux.cgroup.path
  Exe   exe   = 2;   // exe.path
}
```

Both subtrees are live simultaneously and both yield `path`.

### Policies

| `collision_policy` | behavior |
|---|---|
| `COLLISION_POLICY_IGNORE` (default) | keep one value per `collision_wins`, no diagnostics |
| `COLLISION_POLICY_ERROR_GENERATE` | fail `protoc` for statically detectable collisions |
| `COLLISION_POLICY_ERROR_RUNTIME` | generate a key tracker; `MarshalPlainJSON` returns an error when a duplicate key is actually written |

`ERROR_GENERATE` output:

```
protoc-gen-go-plainjson: example.Event: JSON key "path" produced by two live sources:
  - linux.cgroup.path  (example.Cgroup.path)
  - exe.path           (example.Exe.path)
  fix: set (plainjson.field).prefix / .name, use KEY_FROM_PATH, add a merge rule,
       declare an exclusive group, or set collision_policy
```

`ERROR_RUNTIME` is the only policy that catches **dynamic** collisions —
`CARDINALITY_INLINE_KEYS` map keys, `CARDINALITY_EXPLODE` elements, and
`exclusive_groups` that turn out not to be exclusive:

```go
b, err := m.MarshalPlainJSON()
var ce *plainjsonpb.KeyCollisionError
if errors.As(err, &ce) {
    // ce.Key == "name", ce.First == "name", ce.Second == "labels[name]"
}
```

### Winner

| `collision_wins` | behavior | cost |
|---|---|---|
| `COLLISION_WINS_FIRST` (default) | the first entry in traversal order that produces a non-empty value wins; later writes for that key are skipped | none — fully streaming, the decision is made before the key is written |
| `COLLISION_WINS_LAST` | the last writer wins | the object is buffered before flush; the plugin enables buffering only for messages that need it |

Because empty values are omitted by default, `FIRST` behaves as "first source that
actually has something" — which is usually what flattening wants.

---

## Scalars and well-known types

The baseline is **protojson at its default options**; the format options above
override it per field, message or file.

| proto | default JSON | overrides |
|---|---|---|
| `int32`/`uint32`/`fixed32`/`sfixed32`/`sint32` | number | — |
| `int64`/`uint64`/`fixed64`/`sfixed64`/`sint64` | string | `int64_format: NUMBER` |
| `float`/`double` | number; `"NaN"`, `"Infinity"`, `"-Infinity"`; `-0` preserved | — |
| `bool` | `true`/`false` | — |
| `string` | string | — |
| `bytes` | padded base64 | `bytes_format: BASE64_URL \| HEX \| ARRAY` |
| `enum` | value name | `enum_format: NUMBER`, `(plainjson.enum).strip_prefix`, `(plainjson.enum_value).name` |
| `Timestamp` | RFC 3339 string | `time_format: UNIX_SECONDS \| UNIX_MILLI \| UNIX_MICRO \| UNIX_NANO` |
| `Duration` | `"1.5s"` | `duration_format: SECONDS \| MILLIS \| NANOS` (numbers) |
| wrappers (`StringValue`, …) | bare value | — |
| `Struct` / `Value` / `ListValue` | native JSON | — |
| `Empty` | `{}` | — |
| `FieldMask` | comma-separated lowerCamel string | — |
| `Any` | `{"@type": …}` | — |

Well-known types are **leaves**: `flatten` never descends into them. To pull a
value out of a `Struct`, use `pick` with a JSON pointer-ish path:

```proto
google.protobuf.Struct attrs = 5 [(plainjson.field) = {pick: "user.id", name: "user_id"}];
```

Unknown `Any` type URLs degrade to `{"@type": …}` rather than failing.

---

## Empty values and presence

By default a key is omitted when its value is "empty", matching protojson:

- proto3 implicit-presence scalar equal to its zero value;
- empty `string`/`bytes`;
- empty repeated/map field;
- unset message field;
- unset `optional` field, unset `oneof` branch.

Explicit presence wins over the zero check: an `optional int32` set to `0` **is**
emitted, as is a set wrapper holding `0`.

`emit_empty: true` forces the key to appear:

```proto
message Health {
  option (plainjson.message) = {generate: true, emit_empty: true};
  int32  restarts = 1;                                        // -> "restarts":0
  string reason   = 2 [(plainjson.field).emit_empty = false];  // still omitted
}
```

For a message field, `emit_empty: true` under flattening emits every leaf key of
its subtree with its zero value — this is how you get a stable, fixed-shape flat
record out of a sparse tree.

---

## Generation-time validation

The plugin refuses to generate on any of these:

| check | message |
|---|---|
| flatten cycle | `inline cycle: example.A.b -> example.B.a -> example.A; set flatten: FLATTEN_MODE_NONE or max_depth` |
| unresolvable path | `pick "a.b" on example.M.f: field "b" not found in example.A` |
| non-leaf path | `pick "user" on example.M.f: resolves to a message; pick a scalar or use lift` |
| `pick` + `lift` together | `pick and lift are mutually exclusive on example.M.f` |
| `flatten` on a non-message | `flatten on example.M.count: only message fields can be flattened` |
| `prefix`/`suffix` with `FLATTEN_MODE_NONE` on a message field | `prefix on example.M.f has no effect: the field is not flattened` |
| `join` on non-scalar elements | `CARDINALITY_JOIN on example.M.items: elements are messages; add pick` |
| map-only cardinality on repeated | `CARDINALITY_KEYS on example.M.items: repeated fields have no keys` |
| `tag` outside a oneof | `tag on example.M.f: not a oneof branch` |
| duplicate merge key | `merge key "pid" declared twice on example.M` |
| merge path overlap | `merge path "linux.pid" used by rules "pid" and "process_id"` |
| incompatible merge sources | `merge "pid": sources have different JSON types (number, object)` |
| unknown exclusive-group field | `exclusive_groups: field "dockerr" not found in example.M` |
| static collision under `ERROR_GENERATE` | see [Key collisions](#key-collisions) |
| `generate` on a map entry | map entries are synthetic and cannot be generated |

---

## Runtime errors

All returned from `EncodePlainJSON` / `MarshalPlainJSON`, all wrapped so
`errors.As` works:

| type | when |
|---|---|
| `*plainjsonpb.KeyCollisionError` | `COLLISION_POLICY_ERROR_RUNTIME` saw a duplicate key. Fields: `Key`, `First`, `Second` |
| `*plainjsonpb.MergeConflictError` | a merge rule with `MERGE_CONFLICT_ERROR` had two non-empty sources. Fields: `Key`, `Paths` |

With default options neither can occur and the error is always `nil`.

---

## Worked example

```proto
syntax = "proto3";
package telemetry;

import "google/protobuf/timestamp.proto";
import "plainjson/plainjson.proto";

option (plainjson.file) = {
  flatten: FLATTEN_MODE_DEEP,
  key_case: KEY_CASE_SNAKE,
  collision_policy: COLLISION_POLICY_ERROR_RUNTIME
};

message Event {
  option (plainjson.message) = {
    generate: true,
    override_marshal_json: true,
    constants: [{key: "schema", value_json: "\"v1\""}],
    merge: [{key: "pid", from: ["process.linux.pid", "process.windows.process_id"]}]
  };

  string                    id         = 1;
  google.protobuf.Timestamp observed   = 2 [(plainjson.field).time_format = TIME_FORMAT_UNIX_MILLI];
  Process                   process    = 3;
  Network                   network    = 4;
  map<string, string>       labels     = 5 [(plainjson.field) = {
                                              cardinality: CARDINALITY_INLINE_KEYS,
                                              prefix: "lbl_",
                                              key_case: KEY_CASE_ORIGINAL
                                            }];
  Debug                     debug      = 6 [(plainjson.field).omit = true];
}

message Process {
  Exe exe = 1;
  oneof os {
    option (plainjson.oneof) = {mode: ONEOF_MODE_TAGGED, discriminator: "os"};
    Linux   linux   = 2;
    Windows windows = 3;
  }
  repeated string argv = 4 [(plainjson.field) = {
    cardinality: CARDINALITY_JOIN, join_separator: " "
  }];
}

message Exe     { string path = 1 [(plainjson.field).name = "exe_path"]; string sha256 = 2; }
message Linux   { int32 pid = 1; Cgroup cgroup = 2 [(plainjson.field).prefix = "cgroup_"]; }
message Cgroup  { string path = 1; }
message Windows { int32 process_id = 1; string sid = 2; }
message Network { string remote_ip = 1; uint32 remote_port = 2; }
message Debug   { string trace = 1; }
```

Input:

```go
&telemetry.Event{
  Id:       "e-1",
  Observed: timestamppb.New(t),
  Process: &telemetry.Process{
    Exe:  &telemetry.Exe{Path: "/usr/bin/curl", Sha256: "9f86d0…"},
    Os:   &telemetry.Process_Linux{Linux: &telemetry.Linux{
            Pid:    4242,
            Cgroup: &telemetry.Cgroup{Path: "/sys/fs/cgroup/app"},
          }},
    Argv: []string{"curl", "-sS", "https://x"},
  },
  Network: &telemetry.Network{RemoteIp: "10.0.0.1", RemotePort: 443},
  Labels:  map[string]string{"env": "prod"},
  Debug:   &telemetry.Debug{Trace: "…"},
}
```

Output:

```json
{
  "schema": "v1",
  "id": "e-1",
  "observed": 1787000000123,
  "exe_path": "/usr/bin/curl",
  "sha256": "9f86d0…",
  "os": "linux",
  "cgroup_path": "/sys/fs/cgroup/app",
  "argv": "curl -sS https://x",
  "remote_ip": "10.0.0.1",
  "remote_port": 443,
  "lbl_env": "prod",
  "pid": 4242
}
```

Five levels of nesting, one `oneof`, a map, a repeated field and two `path`
fields — one flat object. `Windows` input would produce the same `pid` and `os`
keys with `"os":"windows"` and `"sid"` instead of `"cgroup_path"`.

---

## Performance

Generated code writes straight into a `jx.Encoder`: no reflection, no descriptor
walk, no intermediate map. The flatten plan — key strings, order, presence
checks — is resolved at generation time and compiled into straight-line Go.

Numbers from `make bench` on an i5-14600K, Go 1.27. Run it yourself; the
benchmarks live in `example/bench/`.

**Flattening the same message three ways.** All three produce the same record,
and `TestBaselinesAgree` fails the suite if they ever stop agreeing. The
baselines are what you would write instead of this plugin: `hand-reflect` walks
descriptors and writes into `jx`, `hand-json` lets protojson build the nested
document and collapses it as a map.

| implementation | ns/op | B/op | allocs/op | vs generated |
|---|--:|--:|--:|--:|
| generated | 776 | 1 048 | 9 | — |
| hand-reflect | 3 099 | 3 112 | 20 | **4.0×** slower |
| hand-json | 16 060 | 9 830 | 229 | **20.7×** slower |

**Against protojson.** protojson emits the nested document rather than the flat
one, so this is a floor for "serialise this message at all", not a like-for-like
comparison. `Event` exercises the full option set — a tagged oneof, a merge
rule, inlined map keys, a joined repeated field; `Scalars` is flat, so it
isolates the cost of writing values.

| message | codec | ns/op | B/op | allocs/op | speedup |
|---|---|--:|--:|--:|--:|
| Event | plainjson | 1 585 | 2 424 | 20 | **3.4×** |
| Event | protojson | 5 450 | 5 159 | 102 | — |
| Scalars | plainjson | 687 | 600 | 14 | **2.3×** |
| Scalars | protojson | 1 612 | 961 | 22 | — |

**What each collision policy costs**, on one message whose two subtrees both
claim `path`:

| policy | ns/op | B/op | allocs/op | what it needs |
|---|--:|--:|--:|---|
| `IGNORE` + `FIRST` (default) | 268 | 368 | 6 | a local bool per contested key |
| `ERROR_RUNTIME` | 498 | 1 560 | 9 | a key tracker |
| `IGNORE` + `LAST` | 928 | 2 480 | 24 | the object buffered before flush |

The default policy is free: the flag is a register, and a message with no
contested key gets no machinery at all. `LAST` is the one to avoid in a hot
path — it has to hold the whole object to let a later write replace an earlier
one. `ERROR_RUNTIME` is measured on data that does *not* collide, since a real
collision returns an error rather than bytes; what it prices is the tracker.

## Scope and limitations

- **Marshal only.** No decoder is generated, and the output cannot be mapped back
  to a message. That is the design, not a missing feature.
- proto3 only. proto2 semantics, extensions and groups are out of scope.
- Cross-package message fields are supported when generated code exists for them;
  flattening across a package boundary requires the referenced package to be
  generated by this plugin too.
- `Any`, `Struct`, `Value` and `ListValue` are rendered through `protojson`,
  the one place the runtime is not reflection-free: their JSON is defined by
  their content rather than by fields.
- Lifting out of a well-known type is not supported; `pick` into a `Struct`
  is, and resolves the path at run time.
- `exclusive_groups` is a promise you make to the generator; violating it falls
  back to `collision_wins` (or to a runtime error under `ERROR_RUNTIME`).

---

## Development

```bash
make build      # build bin/protoc-gen-go-plainjson
make gen-opts   # regenerate plainjson/plainjson.pb.go from plainjson/plainjson.proto
make gen        # regenerate example/golden via easyp
make test       # gofmt + go vet + go test
```

Repository layout:

- `main.go`, `generator/` — the plugin: option resolution, flatten planning,
  collision analysis, `jx` code emission.
- `plainjson/` — `plainjson.proto`, the option definitions (extension number
  `60040` on `FileOptions`, `MessageOptions`, `FieldOptions`, `OneofOptions`,
  `EnumOptions`, `EnumValueOptions`).
- `plainjsonpb/` — the runtime imported by generated code: scalar and WKT
  encoders, key tracker, error types.
- `example/golden/` — protos exercising every option, plus tests asserting the
  documented JSON for each.

## License

See [LICENSE](LICENSE).

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
- [What it is not](#what-it-is-not)
- [Install](#install)
- [Usage](#usage)
- [Generated API](#generated-api)
- [Core model](#core-model)
- [Options](#options) — full reference in [OPTIONS.md](OPTIONS.md)
- [Scalars and well-known types](#scalars-and-well-known-types)
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

Full list with defaults in [OPTIONS.md](OPTIONS.md#plugin-flags).

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
## Options

Every flattening rule lives in the `.proto`. The full reference — every option,
what it changes, and a worked example for each — is in
**[OPTIONS.md](OPTIONS.md)**.

The shape of it:

| group | options | what it decides |
|---|---|---|
| [Shape](OPTIONS.md#shape-what-gets-flattened) | `flatten`, `max_depth` | which subtrees are folded into the parent, and how deep |
| [Keys](OPTIONS.md#keys-what-they-are-called) | `key_from`, `key_case`, `name`, `prefix`, `suffix` | what the resulting keys are called |
| [Selection](OPTIONS.md#selection-what-survives) | `omit`, `pick`, `lift` | what is dropped, and what is pulled up out of a subtree |
| [Collections](OPTIONS.md#collections) | `cardinality`, `join_separator`, `index_separator` | what happens to repeated fields and maps |
| [Oneof](OPTIONS.md#oneof) | `mode`, `discriminator`, `value_key`, `omit_if_unset`, `tag` | how a branch and its identity are represented |
| [Combining](OPTIONS.md#combining-fields) | `merge`, `constants`, `exclusive_groups` | values gathered across the message, and keys with no source |
| [Collisions](OPTIONS.md#collisions) | `collision_policy`, `collision_wins` | what happens when two live sources claim one key |
| [Values](OPTIONS.md#values) | `emit_empty`, `enum_format`, `int64_format`, `bytes_format`, `time_format`, `duration_format` | how each value is spelled |
| [Enums](OPTIONS.md#enums) | `format`, `strip_prefix`, value `name`/`omit` | enum vocabulary |
| [Generation](OPTIONS.md#generation) | `generate`, `generate_all`, `override_marshal_json` | whose marshalers are emitted |

Options resolve nearest-wins along `plugin flag → file → message → oneof →
field`. Formatting options propagate down a subtree; `prefix`/`suffix`
accumulate; `flatten` is a boundary decision that does not propagate. See
[Inheritance and precedence](OPTIONS.md#inheritance-and-precedence).

The plugin refuses to generate on a contradictory or unresolvable rule — a
flatten cycle, a `pick` path that does not exist, a map-only cardinality on a
repeated field, and a dozen more. Each diagnostic names the message, the field
and the way out: see
[Generation-time validation](OPTIONS.md#generation-time-validation).

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

Documentation:

- [README.md](README.md) — what the plugin is, how to run it, what it costs.
- [OPTIONS.md](OPTIONS.md) — every option, with a worked example each.

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

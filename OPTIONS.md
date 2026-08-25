# Option reference

Every flattening rule this plugin applies is declared in the `.proto`, next to
the field it describes. This document covers **every option**, what it changes,
and what it produces — one worked example per option, all of them taken from the
conformance suite in `testdata/cases`, so nothing here can drift from the
implementation without a test going red.

For what the plugin is and how to run it, see [README.md](README.md).

## Contents

- [Reading the options](#reading-the-options)
- [Inheritance and precedence](#inheritance-and-precedence)
- [What is available where](#what-is-available-where)
- [Shape: what gets flattened](#shape-what-gets-flattened)
  - [`flatten`](#flatten) · [`max_depth`](#max_depth)
- [Keys: what they are called](#keys-what-they-are-called)
  - [`key_from`](#key_from) · [`key_case`](#key_case) · [`name`](#name) · [`prefix` / `suffix`](#prefix--suffix)
- [Selection: what survives](#selection-what-survives)
  - [`omit`](#omit) · [`pick`](#pick) · [`lift`](#lift)
- [Collections](#collections)
  - [`cardinality`](#cardinality) · [`join_separator`](#join_separator) · [`index_separator`](#index_separator)
- [Oneof](#oneof)
  - [`mode`](#mode) · [`discriminator`](#discriminator) · [`value_key`](#value_key) · [`omit_if_unset`](#omit_if_unset) · [`tag`](#tag)
- [Combining fields](#combining-fields)
  - [`merge`](#merge) · [`constants`](#constants) · [`exclusive_groups`](#exclusive_groups)
- [Collisions](#collisions)
  - [`collision_policy`](#collision_policy) · [`collision_wins`](#collision_wins)
- [Values](#values)
  - [`emit_empty`](#emit_empty) · [`enum_format`](#enum_format) · [`int64_format`](#int64_format) · [`bytes_format`](#bytes_format) · [`time_format`](#time_format) · [`duration_format`](#duration_format)
- [Enums](#enums)
  - [`(plainjson.enum).format`](#plainjsonenumformat) · [`strip_prefix`](#strip_prefix) · [`(plainjson.enum_value).name`](#plainjsonenum_valuename) · [`(plainjson.enum_value).omit`](#plainjsonenum_valueomit)
- [Generation](#generation)
  - [`generate`](#generate) · [`generate_all`](#generate_all) · [`override_marshal_json`](#override_marshal_json)
- [Generation-time validation](#generation-time-validation)
- [Plugin flags](#plugin-flags)
- [Alphabetical index](#alphabetical-index)

---

## Reading the options

Import the option definitions and annotate whatever level the rule belongs to:

```proto
syntax = "proto3";
package myapp;

import "plainjson/plainjson.proto";

option (plainjson.file) = {generate_all: true, key_case: KEY_CASE_SNAKE};

message Event {
  option (plainjson.message) = {flatten: FLATTEN_MODE_DEEP};

  string id = 1 [(plainjson.field).name = "event_id"];

  oneof body {
    option (plainjson.oneof).mode = ONEOF_MODE_TAGGED;
    Click click = 2;
  }
}

enum Severity {
  option (plainjson.enum).strip_prefix = true;
  SEVERITY_UNSPECIFIED = 0;
  SEVERITY_HIGH = 1 [(plainjson.enum_value).name = "high"];
}
```

Six extension points, all on extension number `60040`:

| target | extension | declared |
|---|---|---|
| file | `(plainjson.file)` | top of the file |
| message | `(plainjson.message)` | inside the message |
| oneof | `(plainjson.oneof)` | inside the `oneof` block |
| field | `(plainjson.field)` | in the field's option brackets |
| enum type | `(plainjson.enum)` | inside the enum |
| enum value | `(plainjson.enum_value)` | in the value's option brackets |

Both spellings work: `option (plainjson.message) = {a: 1, b: 2};` sets several
at once, `option (plainjson.message).a = 1;` sets one.

---

## Inheritance and precedence

Options resolve nearest-wins along:

```
plugin flag  →  file  →  message  →  oneof  →  field
```

An unset enum (`*_UNSPECIFIED`, the zero value) and an unset `optional bool`
mean "inherit". Three groups behave differently, and the difference matters:

**Formatting options propagate down the subtree.** `key_from`, `key_case`,
`emit_empty`, `cardinality`, the separators and every value format keep applying
to everything lifted out of the field they are set on, at any depth — siblings
are untouched.

```proto
message MixedKeyCase {
  option (plainjson.message).generate = true;
  Linux left  = 1 [(plainjson.field).key_case = KEY_CASE_SNAKE];
  Linux right = 2;
}
message Linux { int32 pid = 1; string cgroup_path = 2; }
```
```json
// left {pid:1, cgroupPath:"/l"}, right {pid:2, cgroupPath:"/r"}
{"pid":1,"cgroup_path":"/l","cgroupPath":"/r"}
```
Left's subtree is snake_cased, right's is not. (`right.pid` collides with
`left.pid` and loses — see [`collision_wins`](#collision_wins).)

**`prefix` and `suffix` accumulate** instead of overriding — see
[`prefix` / `suffix`](#prefix--suffix).

**`flatten` does not propagate at all.** On a field it is a *boundary* decision
— is this field inlined into its parent — and nothing more. What happens below
that boundary belongs to the field's own message type. See [`flatten`](#flatten).

---

## What is available where

| option | file | message | oneof | field | notes |
|---|:-:|:-:|:-:|:-:|---|
| `flatten` | ● | ● | | ● | on a field it is a boundary only |
| `max_depth` | ● | ● | | ● | `optional`, explicit `0` lifts a bound |
| `key_from` | ● | ● | ● | ● | |
| `key_case` | ● | ● | ● | ● | |
| `emit_empty` | ● | ● | ● | ● | `optional bool` |
| `cardinality` | ● | ● | ● | ● | |
| `join_separator` | ● | ● | ● | ● | |
| `index_separator` | ● | ● | ● | ● | |
| `enum_format` | ● | ● | ● | ● | enum type sits between scope and field |
| `int64_format` | ● | ● | ● | ● | |
| `bytes_format` | ● | ● | ● | ● | |
| `time_format` | ● | ● | ● | ● | |
| `duration_format` | ● | ● | ● | ● | |
| `collision_policy` | ● | ● | | | a property of the object being built |
| `collision_wins` | ● | ● | | | |
| `override_marshal_json` | ● | ● | | | |
| `generate` | | ● | | | `optional bool` |
| `generate_all` | ● | | | | |
| `merge` | | ● | | | |
| `constants` | | ● | | | |
| `exclusive_groups` | | ● | | | |
| `mode`, `discriminator`, `value_key`, `omit_if_unset` | | | ● | | |
| `omit`, `name`, `prefix`, `suffix`, `pick`, `lift`, `tag` | | | | ● | |

---

## Shape: what gets flattened

### `flatten`

**Type** `FlattenMode` · **Levels** file, message, field · **Default** `FLATTEN_MODE_DEEP`

Decides whether a message field is folded into its parent object or keeps a key
of its own.

| value | meaning |
|---|---|
| `FLATTEN_MODE_DEEP` | recursively hoist every leaf of the subtree into the parent |
| `FLATTEN_MODE_SHALLOW` | hoist one level; message fields below stay nested |
| `FLATTEN_MODE_NONE` | keep the subtree as its own nested object |

On a **message** it says what happens to that message's own fields. On a
**field** it is a boundary decision about that one field, and it does *not*
propagate: below the boundary the field's type decides.

```proto
message Wrap {
  option (plainjson.message).generate = true;   // FLATTEN_MODE_DEEP by default
  string id  = 1;
  Outer  out = 2;
}
message Outer { string a = 1; Inner in = 2; }
message Inner { string b = 1; }
```

Input `{"id":"w","out":{"a":"x","in":{"b":"y"}}}`:

| mode set on `Wrap` | output |
|---|---|
| `DEEP` (default) | `{"id":"w","a":"x","b":"y"}` |
| `SHALLOW` | `{"id":"w","a":"x","in":{"b":"y"}}` |
| `NONE` | `{"id":"w","out":{"a":"x","b":"y"}}` |

Read that last row carefully. `NONE` on `Wrap` stops `Wrap` from inlining `out`
— but the *contents* of `out` follow `Outer`, which is still `DEEP`, so `in` is
flattened inside the nested object. For a fully protojson-shaped subtree, say so
on the type, or once at file level:

```proto
message Outer {
  option (plainjson.message).flatten = FLATTEN_MODE_NONE;
  string a = 1;
  Inner in = 2;
}
// {"id":"w","out":{"a":"x","in":{"b":"y"}}}
```

**Boundary and interior compose.** This is what lets you build a flat header
with one grouped object that is itself flat:

```proto
message GroupedButFlatInside {
  option (plainjson.message) = {generate: true, flatten: FLATTEN_MODE_DEEP};
  string     id  = 1;
  FlatInside out = 2 [(plainjson.field).flatten = FLATTEN_MODE_NONE];  // keeps its key
}
message FlatInside {
  option (plainjson.message).flatten = FLATTEN_MODE_DEEP;              // flat inside
  string a  = 1;
  Inner  in = 2;
}
```
```json
{"id":"w","out":{"a":"x","b":"y"}}
```

The same type can be inlined at one use site and kept nested at another — the
boundary belongs to the field, the interior to the type:

```proto
message SameTypeBothWays {
  option (plainjson.message).generate = true;
  FlatInside inlined = 1 [(plainjson.field).flatten = FLATTEN_MODE_DEEP];
  FlatInside grouped = 2 [(plainjson.field).flatten = FLATTEN_MODE_NONE];
}
// {"a":"x","b":"y","grouped":{"a":"x2","b":"y2"}}
```

**Errors.** `flatten` on a non-message field is rejected at generation time
(`only message fields can be flattened`), as is `prefix`/`suffix` alongside
`FLATTEN_MODE_NONE` on a field (`has no effect: the field is not flattened`).
A type that flattens into itself with no bound is an `inline cycle` error.

### `max_depth`

**Type** `optional uint32` · **Levels** file, message, field · **Default** `0` (unlimited)

Counts message levels descended from the generated message. When the limit is
reached, the rest of the subtree is written as a nested object instead of being
flattened further. Unlike `flatten`, it **does** propagate down the subtree.

```proto
message MaxDepthMessage {
  option (plainjson.message) = {generate: true, max_depth: 1};
  string id  = 1;
  Outer  out = 2;
}
// {"id":"w","a":"x","in":{"b":"y"}}
```

On a field it bounds one subtree without touching that type's options:

```proto
message MaxDepthField {
  option (plainjson.message).generate = true;
  string a  = 1;
  Level1 l1 = 2 [(plainjson.field).max_depth = 1];
}
// {"a":"1","b":"2","l2":{"c":"3","l3":{"d":"4"}}}
```

Being `optional` matters: an explicit `0` lifts a bound inherited from the file,
which a plain `uint32` could not express.

```proto
option (plainjson.file) = {max_depth: 2};        // file bounds everything at 2

message OverridesDepthAndKeys {
  option (plainjson.message) = {flatten: FLATTEN_MODE_DEEP, max_depth: 0};
  ...                                            // this message is unbounded
}
```

`max_depth` is also what makes a self-recursive type legal — the cycle check
runs per generated message and accepts a bounded descent:

```proto
message DeepNode { string name = 1; DeepNode child = 2; }

message RecursiveBounded {
  option (plainjson.message).generate = true;
  DeepNode root = 1 [(plainjson.field).max_depth = 2];
}
// input  {"root":{"name":"a","child":{"name":"b","child":{"name":"c"}}}}
// output {"name":"a","child":{"name":"c"}}
```
Level 2's `name` loses to level 1's under the default policy; level 3 is past the
bound and stays an object.

---

## Keys: what they are called

All four options below feed one pipeline: the segments along the path are picked
according to `key_from`, wrapped in the accumulated `prefix`/`suffix`, and the
result is rendered in `key_case`. `name` replaces one segment on the way.

### `key_from`

**Type** `KeyFrom` · **Levels** file, message, oneof, field · **Default** `KEY_FROM_LEAF`

| value | key is built from |
|---|---|
| `KEY_FROM_LEAF` | the leaf field's name only |
| `KEY_FROM_PATH` | every field name from the generated message down to the leaf |

```proto
message LeafCamel {
  option (plainjson.message).generate = true;
  Process process = 1;
}
message Process { Linux linux = 1; }
message Linux   { int32 pid = 1; string cgroup_path = 2; }
```

| `key_from` | `key_case` | output |
|---|---|---|
| `LEAF` | `CAMEL` | `{"pid":4242,"cgroupPath":"/sys/x"}` |
| `LEAF` | `SNAKE` | `{"pid":4242,"cgroup_path":"/sys/x"}` |
| `PATH` | `CAMEL` | `{"processLinuxPid":4242,"processLinuxCgroupPath":"/sys/x"}` |
| `PATH` | `SNAKE` | `{"process_linux_pid":4242,"process_linux_cgroup_path":"/sys/x"}` |

`LEAF` is the flattening default and the reason collisions are a first-class
concept here: two paths ending in the same field name land on one key, which is
usually the point. `PATH` is collision-free by construction and is the cheapest
way out of a collision you did not want:

```proto
message ResolvedByPath {
  option (plainjson.message) = {
    generate: true, key_from: KEY_FROM_PATH,
    collision_policy: COLLISION_POLICY_ERROR_GENERATE
  };
  Linux linux = 1;   // linux.cgroup.path
  Exe   exe   = 2;   // exe.path
}
// {"linuxPid":4242,"linuxCgroupPath":"/sys/fs/x","exePath":"/usr/bin/curl","exeSha256":"9f86"}
```

Set per subtree, it mixes freely — here the left branch is keyed by path and the
right by leaf:

```proto
message MixedKeyFrom {
  option (plainjson.message).generate = true;
  Process left  = 1 [(plainjson.field).key_from = KEY_FROM_PATH];
  Process right = 2;
}
// {"leftLinuxPid":1,"leftLinuxCgroupPath":"/l","pid":2,"cgroupPath":"/r"}
```

### `key_case`

**Type** `KeyCase` · **Levels** file, message, oneof, field · **Default** `KEY_CASE_CAMEL`

| value | `cgroup_path` becomes |
|---|---|
| `KEY_CASE_CAMEL` | `cgroupPath` — protojson's `json_name` spelling |
| `KEY_CASE_SNAKE` | `cgroup_path` |
| `KEY_CASE_ORIGINAL` | `cgroup_path` — the proto name verbatim, no transformation |

`SNAKE` and `ORIGINAL` differ whenever the proto name is not already snake_case,
and `ORIGINAL` is what you want when a prefix has to survive literally — see
below.

### `name`

**Type** `string` · **Levels** field · **Default** the proto field name

Replaces this path segment's name. On a leaf it renames the key; accumulated
prefixes still apply.

```proto
message Renamed {
  int32  pid         = 1 [(plainjson.field).name = "process_id"];
  string cgroup_path = 2;
}
// {"processId":4242,"cgroupPath":"/sys/x"}
```

On a message field it changes the segment `KEY_FROM_PATH` uses:

```proto
message NameAffectsPathSegment {
  option (plainjson.message) = {generate: true, key_from: KEY_FROM_PATH};
  Address shipping_address = 1 [(plainjson.field).name = "ship"];
}
// {"shipCity":"Berlin"}
```

`name` is also how two branches of a oneof are collapsed onto one key — legal
without diagnostics, because the branches are mutually exclusive:

```proto
oneof id {
  string uuid = 1 [(plainjson.field).name = "id"];
  int64  seq  = 2 [(plainjson.field).name = "id"];
}
// {"id":"7f3a"}   or   {"id":"91"}
```

### `prefix` / `suffix`

**Type** `string` · **Levels** field · **Default** empty

Applied to **every key produced by the subtree**, and **accumulated** across
levels — outer prefix first, suffixes innermost-last.

```proto
message PrefixAccumulation {
  option (plainjson.message).generate = true;
  Customer shipping = 1 [(plainjson.field).prefix = "ship_"];
  Customer billing  = 2 [(plainjson.field).prefix = "bill_"];
}
message Customer {
  string  name = 1;
  Address addr = 2 [(plainjson.field).prefix = "addr_"];
}
message Address { string city = 1; }
```
```json
{"shipName":"Ann","shipAddrCity":"Berlin","billName":"Bob","billAddrCity":"Rome"}
```

Note the casing: prefixes are glued to the raw segment *before* `key_case` runs,
so `ship_` + `name` becomes `shipName` under `CAMEL`. To keep the literal
underscore form, ask for it:

```proto
message PrefixOriginalCase {
  option (plainjson.message) = {generate: true, key_case: KEY_CASE_ORIGINAL};
  Customer shipping = 1 [(plainjson.field).prefix = "ship_"];
  Customer billing  = 2 [(plainjson.field).prefix = "bill_"];
}
// {"ship_name":"Ann","ship_addr_city":"Berlin","bill_name":"Bob","bill_addr_city":"Rome"}
```

`suffix` works the same way from the other side:

```proto
message SuffixUse {
  option (plainjson.message).generate = true;
  Address home = 1 [(plainjson.field).suffix = "_home"];
  Address work = 2 [(plainjson.field).suffix = "_work"];
}
// {"cityHome":"Berlin","cityWork":"Rome"}
```

Prefixing is the standard way to keep two copies of one type apart without
falling back to `KEY_FROM_PATH`.

---

## Selection: what survives

### `omit`

**Type** `bool` · **Levels** field · **Default** `false`

Drops the field and everything under it. The subtree is never visited, so it
costs nothing at run time.

```proto
message Omit {
  option (plainjson.message).generate = true;
  string id    = 1;
  Debug  debug = 2 [(plainjson.field).omit = true];
}
// input  {"id":"o1","debug":{"trace":"t"}}
// output {"id":"o1"}
```

### `pick`

**Type** `string` (dot path) · **Levels** field · **Default** empty

Replaces a message field with a single value from inside it. The path is
dot-separated proto field names and must resolve to a value, not a message.

```proto
message Pick {
  option (plainjson.message).generate = true;
  Money total = 1 [(plainjson.field).pick = "amount"];
  User  buyer = 2 [(plainjson.field) = {pick: "profile.contact.email", name: "email"}];
}
message Money { int64 amount = 1; string currency = 2; }
```
```json
{"total":"1000","email":"ann@example.com"}
```

Without `name`, the key comes from the annotated field, not from the picked
leaf — `buyer` stays `buyer`:

```proto
User buyer = 1 [(plainjson.field).pick = "profile.contact.email"];
// {"buyer":"ann@example.com"}
```

On a repeated or map field, `pick` applies to **every element**, reducing
message elements to a value — which is what makes `CARDINALITY_JOIN` usable on
them:

```proto
repeated Sample samples = 1 [(plainjson.field) = {
  pick: "value", cardinality: CARDINALITY_JOIN, join_separator: ";"
}];
// {"samples":"1;2"}
```

`pick` also reaches into a well-known `Struct`, whose members exist only at run
time; the path is resolved then, and a missing member simply writes nothing:

```proto
google.protobuf.Struct attrs = 1 [(plainjson.field) = {pick: "user.id", name: "user_id"}];
// input {"attrs":{"user":{"id":"u-7"}}} -> {"userId":"u-7"}
```

**Errors.** An unresolvable path (`field "b" not found in …`), a path stopping on
a message (`resolves to a message; pick a scalar or use lift`), or `pick`
together with `lift` on one field.

### `lift`

**Type** `repeated Lift {path, as}` · **Levels** field · **Default** empty

Hoists selected paths out of a subtree and **drops the rest of it**. `path` is
relative to the annotated field; `as` names the key, defaulting to the leaf's
own name.

```proto
message Lift {
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
// input {"deep":{"a":{"b":{"id":"r-7","title":"Weekly"},"stats":{"total":42}},"ignored":"x"}}
{"id":"r-7","title":"Weekly","total":42}
```

`ignored` is gone: `lift` is an allow-list for its subtree. Use `pick` for one
value, `lift` for a handful, plain flattening when you want everything.

Lifting out of a well-known type is rejected — use `pick` there.

---

## Collections

### `cardinality`

**Type** `Cardinality` · **Levels** file, message, oneof, field · **Default** `CARDINALITY_KEEP`

Describes what happens to a field holding **more than one value**. It is a
property of cardinality, not of type, so one option covers `repeated` and `map`
alike. Maps always iterate **sorted by key**, so output bytes never depend on Go's
map ordering.

Shared input for the table below:

```go
Metrics{
  Samples: []*Sample{{Value: 1, Unit: "ms"}, {Value: 2, Unit: "ms"}},
  Labels:  map[string]string{"env": "prod", "region": "eu"},
}
```

| value | `repeated Sample samples` | `map<string,string> labels` |
|---|---|---|
| `CARDINALITY_KEEP` | `"samples":[{"value":"1","unit":"ms"},{"value":"2","unit":"ms"}]` | `"labels":{"env":"prod","region":"eu"}` |
| `CARDINALITY_FIRST` | `"samples":{"value":"1","unit":"ms"}` | `"labels":"prod"` |
| `CARDINALITY_LAST` | `"samples":{"value":"2","unit":"ms"}` | `"labels":"eu"` |
| `CARDINALITY_COUNT` | `"samples":2` | `"labels":2` |
| `CARDINALITY_JOIN` | needs `pick` — see below | `"labels":"prod,eu"` |
| `CARDINALITY_INDEXED` | `"samples_0_value":"1","samples_0_unit":"ms","samples_1_value":"2","samples_1_unit":"ms"` | `"labels_0":"prod","labels_1":"eu"` |
| `CARDINALITY_EXPLODE` | `"value":"1","unit":"ms"` (first element wins) | keys merged into the parent |
| `CARDINALITY_KEYS` | generation error | `"labels":["env","region"]` |
| `CARDINALITY_VALUES` | generation error | `"labels":["prod","eu"]` |
| `CARDINALITY_INLINE_KEYS` | generation error | `"env":"prod","region":"eu"` |

Notes per mode:

**`KEEP`** — the default. Message elements still flatten *inside* the collection:

```proto
message ElementsFlattenInside {
  option (plainjson.message).generate = true;
  repeated Item items = 1;
}
message Item { string a = 1; Nested n = 2; }
message Nested { string b = 1; }
// {"items":[{"a":"x","b":"y"}]}
```

**`FIRST` / `LAST`** — for maps these mean lowest and highest key, since
iteration is sorted.

**`JOIN`** — requires scalar elements; combine with `pick` for message elements.
The separator is [`join_separator`](#join_separator).

```proto
repeated string argv = 3 [(plainjson.field) = {
  cardinality: CARDINALITY_JOIN, join_separator: " "
}];
// {"argv":"curl -sS"}
```

**`INDEXED`** — builds `<key><index_separator><i>` and keeps flattening below it
for message elements. Pairs naturally with `KEY_CASE_ORIGINAL`, since the parts
are glued after casing.

**`EXPLODE`** — merges each element's keys into the parent with no index. Meant
for collections that are 0..1 in practice; when several elements produce the same
key, the [collision policy](#collision_policy) decides, and under
`COLLISION_POLICY_ERROR_RUNTIME` it is reported:

```proto
message ExplodeCollision {
  option (plainjson.message) = {
    generate: true, collision_policy: COLLISION_POLICY_ERROR_RUNTIME
  };
  repeated Elem elems = 1 [(plainjson.field).cardinality = CARDINALITY_EXPLODE];
}
// [{"k":"a"}]            -> {"k":"a"}
// [{"k":"a"},{"k":"b"}]  -> *plainjsonpb.KeyCollisionError{Key: "k"}
```

**`KEYS` / `VALUES` / `INLINE_KEYS`** — map only; on a repeated field they are a
generation error (`repeated fields have no keys`). `INLINE_KEYS` promotes map
keys to keys of the parent object, which is the one case where JSON keys are
invented at run time:

```proto
message MapInlineKeys {
  option (plainjson.message) = {generate: true, key_case: KEY_CASE_ORIGINAL};
  string name = 1;
  map<string, string> labels = 2 [(plainjson.field) = {
    cardinality: CARDINALITY_INLINE_KEYS, prefix: "lbl_"
  }];
}
// {"name":"http_reqs","lbl_env":"prod","lbl_region":"eu"}
```

Drop the prefix and a map key can clash with a real field — only
`COLLISION_POLICY_ERROR_RUNTIME` can see that:

```json
// labels {"name":"cpu"} against field name -> *plainjsonpb.KeyCollisionError{Key:"name"}
```

Non-string map keys are stringified the protojson way: `map<int32,string>` gives
`{"counts":{"1":"a","2":"b"}}`.

### `join_separator`

**Type** `string` · **Levels** file, message, oneof, field, merge rule · **Default** `","`

Separator for `CARDINALITY_JOIN`. Inherited like any formatting option and
overridable per field:

```proto
option (plainjson.file) = {join_separator: "|"};

message JoinSeparators {
  repeated string a = 1;                                       // file default
  repeated string b = 2 [(plainjson.field).join_separator = "+"];
}
// {"a":"x|y","b":"p+q"}
```

### `index_separator`

**Type** `string` · **Levels** file, message, oneof, field · **Default** `"_"`

Separator for `CARDINALITY_INDEXED`, used both between the key and the index and
between the index and the element's own keys.

```proto
message IndexedSeparator {
  option (plainjson.message) = {
    generate: true, cardinality: CARDINALITY_INDEXED,
    index_separator: "#", key_case: KEY_CASE_ORIGINAL
  };
  repeated Sample samples = 1;
}
// {"samples#0#value":"1","samples#0#unit":"ms","samples#1#value":"2","samples#1#unit":"ms"}
```

---

## Oneof

### `mode`

**Type** `OneofMode` · **Levels** oneof · **Default** `ONEOF_MODE_INLINE` under flattening, `ONEOF_MODE_BRANCH_KEY` under `FLATTEN_MODE_NONE`

```proto
message Event {
  option (plainjson.message).generate = true;
  string id = 1;
  oneof payload {
    option (plainjson.oneof) = {mode: ONEOF_MODE_TAGGED, discriminator: "type"};
    Click  click  = 10;
    Scroll scroll = 11;
  }
}
message Click { int32 x = 1; int32 y = 2; }
```

With `Click{X:10, Y:20}` set:

| mode | output |
|---|---|
| `ONEOF_MODE_INLINE` | `{"id":"e1","x":10,"y":20}` |
| `ONEOF_MODE_BRANCH_KEY` | `{"id":"e1","click":{"x":10,"y":20}}` |
| `ONEOF_MODE_TAGGED` | `{"id":"e1","type":"click","x":10,"y":20}` |
| `ONEOF_MODE_SINGLE_KEY` | `{"id":"e1","payload":{"x":10,"y":20}}` |
| `ONEOF_MODE_DISCRIMINATOR_ONLY` | `{"id":"e1","type":"click"}` |
| `ONEOF_MODE_OMIT` | `{"id":"e1"}` |

The oneof is written **in the position of its first branch**, so key order
follows declaration order like everything else.

`INLINE` is where the headline behaviour lives: branches are mutually exclusive,
so identically named leaves in different branches collapse onto one key with no
diagnostics, even under `COLLISION_POLICY_ERROR_GENERATE`:

```proto
message ExclusiveLeafCollapse {
  option (plainjson.message) = {
    generate: true, collision_policy: COLLISION_POLICY_ERROR_GENERATE
  };
  oneof os {
    Linux   linux   = 1;   // pid, cgroup
    Windows windows = 2;   // pid, sid
  }
}
// {"pid":4242,"cgroup":"/sys/x"}   or   {"pid":77,"sid":"S-1-5"}
```

A scalar branch under `INLINE` keeps its own field name (`{"uuid":"7f3a"}` or
`{"seq":"91"}`); use [`name`](#name) to collapse those onto one key.

### `discriminator`

**Type** `string` · **Levels** oneof · **Default** `"type"`

The key holding the branch tag under `TAGGED` and `DISCRIMINATOR_ONLY`. Rendered
through `key_case` like any other key.

```proto
oneof os {
  option (plainjson.oneof) = {mode: ONEOF_MODE_TAGGED, discriminator: "os_kind"};
  Linux   linux   = 1;
  Windows windows = 2;
}
// {"os_kind":"linux","pid":4242,…}
```

### `value_key`

**Type** `string` · **Levels** oneof · **Default** the oneof's name

The key holding the branch value under `SINGLE_KEY`.

```proto
oneof payload {
  option (plainjson.oneof) = {mode: ONEOF_MODE_SINGLE_KEY, value_key: "body"};
  Click click = 1;
}
// {"body":{"x":10,"y":20}}
```

### `omit_if_unset`

**Type** `optional bool` · **Levels** oneof · **Default** `true`

What an unset oneof writes. `true` writes nothing; `false` writes `null` under
the discriminator or value key — useful when a consumer needs a fixed set of
columns.

```proto
oneof payload {
  option (plainjson.oneof) = {
    mode: ONEOF_MODE_TAGGED, discriminator: "type", omit_if_unset: false
  };
  Click  click  = 10;
  Scroll scroll = 11;
}
// no branch set -> {"id":"e1","type":null}
// click set     -> {"id":"e1","type":"click","x":10,"y":20}
```

### `tag`

**Type** `string` · **Levels** field (oneof branches only) · **Default** the branch's field name

The discriminator value written for one branch.

```proto
oneof payload {
  option (plainjson.oneof) = {mode: ONEOF_MODE_TAGGED, discriminator: "type"};
  Click  click  = 10;
  Scroll scroll = 11 [(plainjson.field).tag = "SCROLL_EV"];
}
// scroll set -> {"id":"e1","type":"SCROLL_EV","dy":5}
```

`tag` on a field that is not a oneof branch is a generation error.

---

## Combining fields

### `merge`

**Type** `repeated MergeRule` · **Levels** message · **Default** none

Collects several source paths into one key, in priority order. Where `pick` and
`lift` work inside one field, `merge` reaches across the whole message — use it
when the same logical value lives in different places under different names.

```proto
message Merge {
  option (plainjson.message) = {
    generate: true,
    merge: [
      {key: "pid",  from: ["container.runtime.host_pid", "linux.pid", "windows.process_id"]},
      {key: "user", from: ["linux.ucred.uid", "windows.sid"]},
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
// linux branch
{"image":"nginx","pid":4242,"user":"1000","startedAt":1787659200000}
// windows branch, same keys
{"image":"nginx","pid":77,"user":"S-1-5","startedAt":1787659200000}
```

Rule fields:

| field | type | default | meaning |
|---|---|---|---|
| `key` | string | required | the JSON key produced |
| `from` | repeated string | required | dot paths, in priority order |
| `on_conflict` | `MergeConflict` | `FIRST_NON_EMPTY` | how several non-empty sources resolve |
| `emit_empty` | optional bool | inherited | write the key even when every source is empty |
| `raw_key` | bool | `false` | use `key` verbatim, ignoring `key_case` |
| `cardinality`, `join_separator` | | inherited | for collection sources |
| `enum_format`, `int64_format`, `bytes_format`, `time_format`, `duration_format` | | inherited | value formatting for the merged value |

Semantics worth knowing:

- Paths in `from` **leave the normal flatten plan** — they reach the output only
  through the rule. Their siblings are untouched: `container.image` above still
  appears, only `container.runtime.host_pid` is consumed.
- Merged keys are written **after** the flatten plan, in rule declaration order.
  The full emission order is: constants, flatten plan, merged keys.
- Sources of one rule are exempt from collision handling, like oneof branches.

`on_conflict` decides what happens when more than one source holds a value:

| value | behaviour with `container.runtime.host_pid=999` and `linux.pid=4242` |
|---|---|
| `MERGE_CONFLICT_FIRST_NON_EMPTY` (default) | `{"pid":999}` |
| `MERGE_CONFLICT_LAST_NON_EMPTY` | `{"pid":4242}` |
| `MERGE_CONFLICT_ERROR` | `*plainjsonpb.MergeConflictError{Key:"pid", Paths:[…]}` |

An empty earlier source never claims the key, so `FIRST_NON_EMPTY` reads as
"first source that actually has something". With every source empty the key is
omitted, unless the rule sets `emit_empty: true`, which writes `{"pid":null}`.

A rule carries its own formatting, independent of the fields it reads:

```proto
merge: [
  {key: "payload", from: ["blob.data"],      bytes_format: BYTES_FORMAT_HEX},
  {key: "joined",  from: ["counted.parts"],  cardinality: CARDINALITY_JOIN, join_separator: "+"},
  {key: "kind",    from: ["typed.kind"],     enum_format: ENUM_FORMAT_NUMBER},
  {key: "took",    from: ["timed.took"],     duration_format: DURATION_FORMAT_MILLIS}
]
// {"payload":"010203","joined":"a+b","kind":1,"took":1500}
```

**Errors.** A duplicate `key` (`merge key "pid" declared twice`), one path used by
two rules (`merge path … used by rules …`), an unresolvable path, or sources that
cannot land on one key (`sources have different JSON types (number, object)`).

### `constants`

**Type** `repeated Constant {key, value_json, raw_key}` · **Levels** message · **Default** none

Injects fixed key/value pairs with no protobuf source. `value_json` is raw JSON
and must parse.

```proto
message Constants {
  option (plainjson.message) = {
    generate: true,
    constants: [
      {key: "schema", value_json: "\"v1\""},
      {key: "source", value_json: "\"agent\""},
      {key: "flags",  value_json: "[\"flat\",\"lossy\"]"}
    ]
  };
  string id = 1;
}
// {"schema":"v1","source":"agent","flags":["flat","lossy"],"id":"e-1"}
```

Constants are emitted **first**, in declaration order, and take part in collision
detection like any other key. `raw_key` keeps the key exactly as written:

```proto
constants: [
  {key: "X-Schema",       value_json: "\"v1\"", raw_key: true},
  {key: "schema_version", value_json: "\"v1\""}
]
// {"X-Schema":"v1","schema_version":"v1"}
```

Typical uses: a schema version for downstream consumers, a source marker, a fixed
`event.kind` your pipeline expects.

### `exclusive_groups`

**Type** `repeated ExclusiveGroup {fields}` · **Levels** message · **Default** none

Declares that a set of fields never carries a value at the same time. Members may
share JSON keys without being reported as a collision — the same exemption oneof
branches get automatically.

```proto
message Runtime {
  option (plainjson.message) = {
    generate: true,
    collision_policy: COLLISION_POLICY_ERROR_GENERATE,
    exclusive_groups: [{fields: ["docker", "podman", "containerd"]}]
  };
  Docker     docker     = 1;   // each has .id
  Podman     podman     = 2;
  Containerd containerd = 3;
}
// {"id":"c0ffee"} — whichever one is set
```

`fields` accepts dot paths for deeper fields:

```proto
exclusive_groups: [{fields: ["left.docker", "right.docker"]}]
```

This is a promise you make to the generator, not something it verifies. Break it
and the winner falls back to [`collision_wins`](#collision_wins) — or, under
`COLLISION_POLICY_ERROR_RUNTIME`, becomes an error:

```json
// docker and podman both set, ERROR_RUNTIME
*plainjsonpb.KeyCollisionError{Key: "id"}
```

An unknown field name is a generation error.

---

## Collisions

Flattening deliberately merges keys. A collision is only a problem when two
entries that **can both produce a value in one encode** claim the same key.
Exempt by construction: different branches of one oneof, different members of a
declared `exclusive_groups` entry, and sources of one merge rule.

### `collision_policy`

**Type** `CollisionPolicy` · **Levels** file, message · **Default** `COLLISION_POLICY_IGNORE`

```proto
message IgnoreFirst {
  option (plainjson.message).generate = true;
  Linux linux = 1;   // linux.cgroup.path
  Exe   exe   = 2;   // exe.path
}
```

| value | behaviour |
|---|---|
| `COLLISION_POLICY_IGNORE` | keep one value per [`collision_wins`](#collision_wins), silently |
| `COLLISION_POLICY_ERROR_GENERATE` | fail `protoc` for statically detectable collisions |
| `COLLISION_POLICY_ERROR_RUNTIME` | track written keys; fail the encode on a real duplicate |

`ERROR_GENERATE` names both sources and the ways out:

```
protoc-gen-go-plainjson: example.Event: JSON key "path" produced by two live sources:
  - linux.cgroup.path  (example.Cgroup.path)
  - exe.path           (example.Exe.path)
  fix: set (plainjson.field).prefix / .name, use KEY_FROM_PATH, add a merge rule,
       declare an exclusive group, or set collision_policy
```

`ERROR_RUNTIME` is the only policy that sees **dynamic** collisions —
`CARDINALITY_INLINE_KEYS` map keys, `CARDINALITY_EXPLODE` elements, a broken
exclusivity promise — and it fires only when both sources actually write:

```go
b, err := m.MarshalPlainJSON()
var ce *plainjsonpb.KeyCollisionError
if errors.As(err, &ce) {
    // ce.Key == "path", ce.First == "linux.cgroup.path", ce.Second == "exe.path"
}
```

A reasonable setup is `IGNORE` in production protos plus
`--go-plainjson_opt=strict` in CI, which flips the default to `ERROR_GENERATE`
and turns any *unintended* collision into a build failure.

### `collision_wins`

**Type** `CollisionWins` · **Levels** file, message · **Default** `COLLISION_WINS_FIRST`

Which writer keeps the key under `IGNORE`.

| value | result for the message above | cost |
|---|---|---|
| `COLLISION_WINS_FIRST` | `{"pid":4242,"path":"/sys/fs/x","sha256":"9f86"}` | none — the decision happens before the key is written |
| `COLLISION_WINS_LAST` | `{"pid":4242,"path":"/usr/bin/curl","sha256":"9f86"}` | the object is buffered so a later write can replace an earlier one |

Because empty values are omitted, `FIRST` means "first source that actually has
something" — an empty earlier source does not claim the key:

```json
// linux.cgroup.path empty
{"pid":4242,"path":"/usr/bin/curl","sha256":"9f86"}
```

`LAST` replaces the value **in place**: key order still follows the first write.
It costs measurably — 3.5× and +18 allocations on the benchmark in
[README](README.md#performance) — so prefer disambiguating keys over relying on
it in a hot path.

---

## Values

### `emit_empty`

**Type** `optional bool` · **Levels** file, message, oneof, field, merge rule · **Default** `false`

By default a key is omitted when its value is empty, matching protojson: a proto3
implicit-presence scalar at its zero value, an empty string/bytes, an empty
collection, an unset message, an unset `optional`, an unset oneof branch.
Explicit presence wins over the zero check — an `optional int32` set to `0`, or a
set wrapper holding `0`, is written.

`emit_empty: true` writes the key regardless:

```proto
message EmitsZeros {
  option (plainjson.message) = {
    generate: true, emit_empty: true, key_case: KEY_CASE_ORIGINAL
  };
  int32  n   = 1;
  string s   = 2;
  bool   b   = 3;
  bytes  by  = 4;
  Leaf   msg = 5 [(plainjson.field).prefix = "nested_"];
}
// empty message -> {"n":0,"s":"","b":false,"by":"","nested_a":"","nested_n":0}
```

On a message field it produces a **fixed-shape record** out of a sparse tree:
every leaf key of the subtree appears with its zero value, which is exactly what
a columnar sink or a CSV-shaped consumer wants.

It is inherited, so a field can opt back out of a message-wide setting:

```proto
message MixedEmitEmpty {
  option (plainjson.message) = {generate: true, emit_empty: true};
  int32  forced = 1;
  string quiet  = 2 [(plainjson.field).emit_empty = false];
}
// empty message -> {"forced":0}
```

### `enum_format`

**Type** `EnumFormat` · **Levels** file, message, oneof, field, merge rule · **Default** `ENUM_FORMAT_NAME`

| value | `SEVERITY_HIGH` becomes |
|---|---|
| `ENUM_FORMAT_NAME` | `"SEVERITY_HIGH"` — protojson's spelling |
| `ENUM_FORMAT_NUMBER` | `2` |

Precedence is field option → the enum type's own [`format`](#plainjsonenumformat)
→ message/file:

```proto
message EnumFormats {
  option (plainjson.message).generate = true;
  Severity name   = 1;
  Severity number = 2 [(plainjson.field).enum_format = ENUM_FORMAT_NUMBER];
}
// {"name":"SEVERITY_HIGH","number":2}
```

### `int64_format`

**Type** `Int64Format` · **Levels** file, message, oneof, field, merge rule · **Default** `INT64_FORMAT_STRING`

Covers `int64`, `uint64`, `sint64`, `fixed64`, `sfixed64` and the 64-bit wrappers.

| value | `-3` becomes |
|---|---|
| `INT64_FORMAT_STRING` | `"-3"` — protojson's default, exact in JavaScript |
| `INT64_FORMAT_NUMBER` | `-3` — convenient, loses precision past 2^53 in most JSON readers |

```proto
message Int64Mixed {
  option (plainjson.message) = {generate: true, int64_format: INT64_FORMAT_NUMBER};
  int64 as_string = 1 [(plainjson.field).int64_format = INT64_FORMAT_STRING];
  int64 as_number = 2;
}
// {"asString":"-3","asNumber":-3}
```

Pick `NUMBER` when the sink is a numeric column and the values are small — ids
and counters usually are, nanosecond timestamps are not.

### `bytes_format`

**Type** `BytesFormat` · **Levels** file, message, oneof, field, merge rule · **Default** `BYTES_FORMAT_BASE64`

Input `0x01 0x02 0x03`:

| value | output |
|---|---|
| `BYTES_FORMAT_BASE64` | `"AQID"` — standard padded base64, protojson's default |
| `BYTES_FORMAT_BASE64_URL` | `"AQID"` — URL-safe alphabet, unpadded |
| `BYTES_FORMAT_HEX` | `"010203"` |
| `BYTES_FORMAT_ARRAY` | `[1,2,3]` |

```proto
message BytesFormats {
  option (plainjson.message).generate = true;
  bytes b64    = 1;
  bytes hex    = 3 [(plainjson.field).bytes_format = BYTES_FORMAT_HEX];
  bytes arr    = 4 [(plainjson.field).bytes_format = BYTES_FORMAT_ARRAY];
}
// {"b64":"AQID","hex":"010203","arr":[1,2,3]}
```

`HEX` is the usual choice for digests and identifiers that humans read.

### `time_format`

**Type** `TimeFormat` · **Levels** file, message, oneof, field, merge rule · **Default** `TIME_FORMAT_RFC3339`

Applies to `google.protobuf.Timestamp`. Input `2026-08-25T12:00:00Z`:

| value | output |
|---|---|
| `TIME_FORMAT_RFC3339` | `"2026-08-25T12:00:00Z"` |
| `TIME_FORMAT_UNIX_SECONDS` | `1787659200` |
| `TIME_FORMAT_UNIX_MILLI` | `1787659200000` |
| `TIME_FORMAT_UNIX_MICRO` | `1787659200000000` |
| `TIME_FORMAT_UNIX_NANO` | `1787659200000000000` |

```proto
google.protobuf.Timestamp observed = 2 [(plainjson.field).time_format = TIME_FORMAT_UNIX_MILLI];
// {"observed":1787659200000}
```

Note the interaction with `int64_format`: the unix variants write a JSON number
regardless, since a timestamp column is numeric by nature.

### `duration_format`

**Type** `DurationFormat` · **Levels** file, message, oneof, field, merge rule · **Default** `DURATION_FORMAT_PROTOJSON`

Applies to `google.protobuf.Duration`. Input `1.5s`:

| value | output |
|---|---|
| `DURATION_FORMAT_PROTOJSON` | `"1.500s"` |
| `DURATION_FORMAT_SECONDS` | `1.5` |
| `DURATION_FORMAT_MILLIS` | `1500` |
| `DURATION_FORMAT_NANOS` | `1500000000` |

```proto
google.protobuf.Duration took = 3 [(plainjson.field).duration_format = DURATION_FORMAT_MILLIS];
// {"took":1500}
```

---

## Enums

### `(plainjson.enum).format`

**Type** `EnumFormat` · **Levels** enum type · **Default** unset

Default representation for **every field of this enum type**, wherever it is
used. Saves repeating `enum_format` on each field; a field option still wins.

```proto
enum Numeric {
  option (plainjson.enum).format = ENUM_FORMAT_NUMBER;
  NUMERIC_UNSPECIFIED = 0;
  NUMERIC_ONE = 1;
}

message M {
  Numeric a = 1;                                                   // -> 1
  Numeric b = 2 [(plainjson.field).enum_format = ENUM_FORMAT_NAME]; // -> "NUMERIC_ONE"
}
```

### `strip_prefix`

**Type** `bool` · **Levels** enum type · **Default** `false`

Removes the SCREAMING_SNAKE type prefix from emitted names — the noise protobuf
naming conventions force on you.

```proto
enum Stripped {
  option (plainjson.enum).strip_prefix = true;
  STRIPPED_UNSPECIFIED = 0;
  STRIPPED_HIGH = 2;
}
// {"stripped":"HIGH"}   instead of   "STRIPPED_HIGH"
```

### `(plainjson.enum_value).name`

**Type** `string` · **Levels** enum value · **Default** the value's proto name

Renames one value, for schemas that prescribe their own vocabulary.

```proto
enum Renamed {
  RENAMED_UNSPECIFIED = 0;
  RENAMED_LOW  = 1 [(plainjson.enum_value).name = "low"];
  RENAMED_HIGH = 2 [(plainjson.enum_value).name = "high"];
}
// {"renamed":"low"}
```

### `(plainjson.enum_value).omit`

**Type** `bool` · **Levels** enum value · **Default** `false`

Drops the key entirely when the field holds this value — a way to treat a
sentinel as "no value".

```proto
enum Renamed {
  RENAMED_UNSPECIFIED = 0;
  RENAMED_HIDDEN = 3 [(plainjson.enum_value).omit = true];
}
// field set to RENAMED_HIDDEN -> {}
```

---

## Generation

### `generate`

**Type** `optional bool` · **Levels** message · **Default** inherited from `generate_all`

Whether the plugin emits marshalers for this message. Being `optional` is what
lets an explicit `false` opt out of a file-wide `generate_all`:

```proto
option (plainjson.file).generate_all = true;

message Covered { string a = 1; }                       // gets marshalers
message OptedOut {
  option (plainjson.message).generate = false;          // does not
  string a = 1;
}
```

A message with no `generate` still takes part in flattening when another message
reaches it — `generate` only decides whose methods are emitted.

### `generate_all`

**Type** `bool` · **Levels** file · **Default** `false`

Generates marshalers for every message in the file, including helper types.
Convenient when a file is dedicated to output shapes; combine with per-message
`generate: false` for exceptions.

### `override_marshal_json`

**Type** `optional bool` · **Levels** file, message · **Default** `false`

Also emits `MarshalJSON()`, so `encoding/json` — and anything built on it —
produces the flattened form:

```proto
message OverridesMarshalJSON {
  option (plainjson.message).override_marshal_json = true;
  string user_id = 1;
}
```
```go
b, _ := json.Marshal(&OverridesMarshalJSON{UserId: "u-1"})  // {"user_id":"u-1"}
```

Convenient for slotting into an existing pipeline that calls `json.Marshal`;
think twice if the same type is also serialised somewhere that expects the
protojson shape.

---

## Generation-time validation

The plugin refuses to generate on a rule that cannot mean anything. Every
diagnostic names the message, the field and the way out, so a mistake surfaces
as a build failure rather than as a key that is quietly missing.

| check | message |
|---|---|
| flatten cycle | `inline cycle: root.child -> spec.Node; set flatten: FLATTEN_MODE_NONE on the type or max_depth at the use site` |
| unresolvable path | `pick "a.b" on example.M: field "b" not found in example.A` |
| non-leaf path | `pick "user" on example.M: resolves to a message; pick a scalar or use lift` |
| `pick` and `lift` together | `example.M.f: pick and lift are mutually exclusive` |
| `flatten` on a non-message | `example.M.count: flatten: only message fields can be flattened` |
| `prefix`/`suffix` with `FLATTEN_MODE_NONE` | `example.M.f: prefix/suffix has no effect: the field is not flattened` |
| `JOIN` on message elements | `example.M.items: CARDINALITY_JOIN: elements are messages; add pick` |
| map-only cardinality on repeated | `example.M.items: CARDINALITY_KEYS: repeated fields have no keys` |
| `tag` outside a oneof | `example.M.f: tag: not a oneof branch` |
| duplicate merge key | `example.M: merge key "pid" declared twice` |
| merge path used twice | `example.M: merge path "a.n" used by rules "pid" and "process_id"` |
| incompatible merge sources | `example.M: merge "pid": sources have different JSON types (number, object)` |
| unknown exclusive-group field | `example.M: exclusive_groups "dockerr": field "dockerr" not found in example.M` |
| constant that is not JSON | `example.M: constant "schema": value_json is not valid JSON: not json` |
| lift out of a well-known type | `example.M.f: lift "x": lifting out of a well-known type is not supported; use pick` |
| static collision under `ERROR_GENERATE` | see [`collision_policy`](#collision_policy) |

Each of these has a proto in `testdata/invalid/` and a test asserting the
message, so the wording above stays true.

---

## Plugin flags

Passed as `--go-plainjson_opt=…`. They are the lowest precedence — any option in
the `.proto` wins.

| flag | meaning |
|---|---|
| `paths=source_relative` | standard protoc-gen-go path mode |
| `default_flatten=deep\|shallow\|none` | changes the built-in default for files that set no `(plainjson.file).flatten` |
| `default_collision_policy=ignore\|generate\|runtime` | same, for the collision policy |
| `strict` | shorthand for `default_collision_policy=generate`; meant for CI |

---

## Alphabetical index

| option | level | section |
|---|---|---|
| `bytes_format` | file, message, oneof, field, merge | [Values](#bytes_format) |
| `cardinality` | file, message, oneof, field, merge | [Collections](#cardinality) |
| `collision_policy` | file, message | [Collisions](#collision_policy) |
| `collision_wins` | file, message | [Collisions](#collision_wins) |
| `constants` | message | [Combining fields](#constants) |
| `discriminator` | oneof | [Oneof](#discriminator) |
| `duration_format` | file, message, oneof, field, merge | [Values](#duration_format) |
| `emit_empty` | file, message, oneof, field, merge | [Values](#emit_empty) |
| `enum_format` | file, message, oneof, field, merge | [Values](#enum_format) |
| `(plainjson.enum).format` | enum type | [Enums](#plainjsonenumformat) |
| `(plainjson.enum).strip_prefix` | enum type | [Enums](#strip_prefix) |
| `(plainjson.enum_value).name` | enum value | [Enums](#plainjsonenum_valuename) |
| `(plainjson.enum_value).omit` | enum value | [Enums](#plainjsonenum_valueomit) |
| `exclusive_groups` | message | [Combining fields](#exclusive_groups) |
| `flatten` | file, message, field | [Shape](#flatten) |
| `generate` | message | [Generation](#generate) |
| `generate_all` | file | [Generation](#generate_all) |
| `index_separator` | file, message, oneof, field | [Collections](#index_separator) |
| `int64_format` | file, message, oneof, field, merge | [Values](#int64_format) |
| `join_separator` | file, message, oneof, field, merge | [Collections](#join_separator) |
| `key_case` | file, message, oneof, field | [Keys](#key_case) |
| `key_from` | file, message, oneof, field | [Keys](#key_from) |
| `lift` | field | [Selection](#lift) |
| `max_depth` | file, message, field | [Shape](#max_depth) |
| `merge` | message | [Combining fields](#merge) |
| `mode` | oneof | [Oneof](#mode) |
| `name` | field | [Keys](#name) |
| `omit` | field | [Selection](#omit) |
| `omit_if_unset` | oneof | [Oneof](#omit_if_unset) |
| `override_marshal_json` | file, message | [Generation](#override_marshal_json) |
| `pick` | field | [Selection](#pick) |
| `prefix` | field | [Keys](#prefix--suffix) |
| `suffix` | field | [Keys](#prefix--suffix) |
| `tag` | field | [Oneof](#tag) |
| `value_key` | oneof | [Oneof](#value_key) |

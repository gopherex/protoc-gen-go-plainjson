package generator

import (
	"fmt"

	"github.com/gopherex/protoc-gen-go-plainjson/plainjson"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// plan is the flattened shape of one generated message: an ordered list of
// writes, resolved entirely at generation time.
type plan struct {
	msg     *protogen.Message
	root    scope
	entries []*entry

	// needsTracker is set when the message must watch written keys at run time
	// (COLLISION_POLICY_ERROR_RUNTIME, or a dynamic source that can collide).
	needsTracker bool
	// needsBuffer is set when a later write must be able to replace an earlier
	// one (COLLISION_WINS_LAST).
	needsBuffer bool
	// oneofSeq hands out exclusivity ids for oneofs.
	oneofSeq int
	// guarded lists keys written by more than one non-exclusive entry, which
	// need a "already written" flag under COLLISION_WINS_FIRST.
	guarded map[string]bool
}

type entryKind int

const (
	// entryScalar writes one value: a scalar, enum, or well-known type.
	entryScalar entryKind = iota
	// entryObject writes a nested object for a message field kept behind its
	// own key.
	entryObject
	// entryCollection writes a repeated or map field under one key.
	entryCollection
	// entryConstant writes a fixed key/value pair.
	entryConstant
	// entryDiscriminator writes the branch tag of a oneof.
	entryDiscriminator
	// entryOneofValue writes the active branch's value under a single key.
	entryOneofValue
	// entryMerge writes the result of a merge rule.
	entryMerge
	// entryStructPick writes a value resolved inside a well-known Struct.
	entryStructPick
)

// entry is one write in the plan.
type entry struct {
	kind entryKind
	key  string
	sc   scope

	// path is the chain of fields from the generated message down to the
	// source. Empty for constants.
	path []*protogen.Field
	// leaf is the field that produces the value, i.e. the last of path.
	leaf *protogen.Field

	// oneof is set for discriminator and single-key entries.
	oneof *protogen.Oneof
	// branchTags maps a oneof branch to the tag it writes.
	branchTags map[*protogen.Field]string
	// omitIfUnset controls whether an unset oneof writes nothing or null.
	omitIfUnset bool

	// constJSON is the raw JSON of a constant entry.
	constJSON string

	// structPath is the dot path a Struct pick resolves at run time.
	structPath string

	// pickPath is the path taken inside each element of a collection, when a
	// pick reduces message elements to a value.
	pickPath []*protogen.Field

	// mergeSources are the resolved source paths of a merge entry, in
	// priority order.
	mergeSources [][]*protogen.Field
	// mergeConflict is how several non-empty sources are resolved.
	mergeConflict plainjson.MergeConflict

	// exclusive groups entries that can never both write: entries sharing a
	// non-zero id are exempt from collision handling.
	exclusive int

	// dynamic marks an entry whose keys are only known at run time
	// (INLINE_KEYS, EXPLODE, INDEXED).
	dynamic bool
	// keyPrefix is the accumulated prefix a dynamic entry puts before every
	// key it invents.
	keyPrefix string

	// source describes where the value comes from, for diagnostics.
	source string
}

// walkCtx is the state carried down the message tree.
type walkCtx struct {
	sc        scope
	keys      keyParts
	path      []*protogen.Field
	depth     uint32
	seen      map[protoreflect.FullName]bool
	exclusive int
}

// buildPlan resolves one generated message into a plan, using the message's
// own options as the root scope.
func buildPlan(msg *protogen.Message) (*plan, error) {
	return buildPlanIn(msg, interiorScope(msg))
}

// buildPlanIn resolves a message into a plan under an explicit root scope.
func buildPlanIn(msg *protogen.Message, root scope) (*plan, error) {
	p := &plan{msg: msg, root: root, guarded: map[string]bool{}}

	opts := messageOptions(msg)
	if err := p.addConstants(opts); err != nil {
		return nil, err
	}

	merges, err := resolveMerges(msg, opts)
	if err != nil {
		return nil, err
	}

	groups, err := resolveExclusiveGroups(msg, opts)
	if err != nil {
		return nil, err
	}

	ctx := walkCtx{
		sc:   root,
		seen: map[protoreflect.FullName]bool{msg.Desc.FullName(): true},
	}
	if err := p.walkMessage(msg, ctx, merges, groups); err != nil {
		return nil, err
	}

	p.appendMerges(merges)
	if err := p.analyse(); err != nil {
		return nil, err
	}
	return p, nil
}

// walkMessage appends the entries produced by one message level.
func (p *plan) walkMessage(msg *protogen.Message, ctx walkCtx, merges []*mergeRule, groups exclusiveGroups) error {
	// Declaration order decides key order, and a oneof takes the position of
	// its first branch.
	done := map[*protogen.Oneof]bool{}
	for _, field := range msg.Fields {
		if oneof := field.Oneof; oneof != nil && !oneof.Desc.IsSynthetic() {
			if done[oneof] {
				continue
			}
			done[oneof] = true
			if err := p.walkOneof(oneof, ctx, merges, groups); err != nil {
				return err
			}
			continue
		}
		if err := p.walkField(field, ctx, merges, groups); err != nil {
			return err
		}
	}
	return nil
}

// walkField appends the entries produced by one field.
func (p *plan) walkField(field *protogen.Field, ctx walkCtx, merges []*mergeRule, groups exclusiveGroups) error {
	opts := fieldOptions(field)
	if opts.GetOmit() {
		return nil
	}
	if consumedByMerge(merges, append(ctx.path, field)) {
		return nil
	}
	if err := validateFieldOptions(field, opts); err != nil {
		return err
	}

	sc := ctx.sc.applyField(opts)
	if field.Enum != nil && opts.GetEnumFormat() == plainjson.EnumFormat_ENUM_FORMAT_UNSPECIFIED {
		// The enum type's own format sits between the scope and the field.
		if f := enumOptions(field.Enum).GetFormat(); f != plainjson.EnumFormat_ENUM_FORMAT_UNSPECIFIED {
			sc.enumFormat = f
		}
	}
	segment := string(field.Desc.Name())
	if opts.GetName() != "" {
		segment = opts.GetName()
	}
	keys := ctx.keys.push(opts.GetPrefix(), opts.GetSuffix(), segment)
	path := append(append([]*protogen.Field(nil), ctx.path...), field)
	exclusive := groups.idFor(path, ctx.exclusive)

	switch {
	case opts.GetPick() != "":
		return p.addPick(field, opts, sc, keys, path, exclusive)
	case len(opts.GetLift()) > 0:
		return p.addLifts(field, opts, sc, keys, path, exclusive)
	case field.Desc.IsList() || field.Desc.IsMap():
		return p.addCollection(field, sc, keys, path, exclusive)
	case isLeafKind(field):
		p.add(&entry{
			kind:      entryScalar,
			key:       keys.key(sc),
			sc:        sc,
			path:      path,
			leaf:      field,
			exclusive: exclusive,
			source:    pathString(path),
		})
		return nil
	default:
		return p.descend(field, opts, sc, keys, path, ctx, merges, groups, exclusive)
	}
}

// descend crosses a message boundary: the field decides whether it is inlined,
// the field's own type decides what happens below.
func (p *plan) descend(
	field *protogen.Field,
	opts *plainjson.FieldOptions,
	sc scope,
	keys keyParts,
	path []*protogen.Field,
	ctx walkCtx,
	merges []*mergeRule,
	groups exclusiveGroups,
	exclusive int,
) error {
	boundary := ctx.sc.flatten
	if o := opts.GetFlatten(); o != plainjson.FlattenMode_FLATTEN_MODE_UNSPECIFIED {
		boundary = o
	}

	depth := ctx.depth + 1
	beyondDepth := sc.maxDepth != 0 && depth > sc.maxDepth
	inline := boundary != plainjson.FlattenMode_FLATTEN_MODE_NONE && !beyondDepth

	// A type that flattens into itself only terminates when something bounds
	// the descent; an unbounded cycle is a generation error.
	if inline && ctx.seen[field.Message.Desc.FullName()] && sc.maxDepth == 0 {
		return fmt.Errorf("%s: inline cycle: %s -> %s; set flatten: FLATTEN_MODE_NONE on the type or max_depth at the use site",
			p.msg.Desc.FullName(), pathString(path), field.Message.Desc.FullName())
	}

	if !inline {
		// The nested object is emitted by its own encoder function, resolved
		// lazily: that is what stops a recursive type from expanding forever.
		nested := sc
		if beyondDepth {
			// Past the bound the rest of the subtree keeps its protojson shape.
			nested.depthExhausted = true
		}
		p.add(&entry{
			kind:      entryObject,
			key:       keys.key(sc),
			sc:        nested,
			path:      path,
			leaf:      field,
			exclusive: exclusive,
			source:    pathString(path),
		})
		return nil
	}

	// Inside the boundary the type's own mode applies; SHALLOW hoists exactly
	// one level, so below it nothing else is inlined.
	inner := sc
	inner.flatten = interiorScope(field.Message).flatten
	if boundary == plainjson.FlattenMode_FLATTEN_MODE_SHALLOW {
		inner.flatten = plainjson.FlattenMode_FLATTEN_MODE_NONE
	}

	seen := make(map[protoreflect.FullName]bool, len(ctx.seen)+1)
	for k, v := range ctx.seen {
		seen[k] = v
	}
	seen[field.Message.Desc.FullName()] = true

	return p.walkMessage(field.Message, walkCtx{
		sc:        inner,
		keys:      keys,
		path:      path,
		depth:     depth,
		seen:      seen,
		exclusive: exclusive,
	}, merges, groups)
}

// add appends an entry to the plan.
func (p *plan) add(e *entry) {
	p.entries = append(p.entries, e)
}

// isLeafKind reports whether a field ends a path: scalars, enums, and every
// well-known type.
func isLeafKind(f *protogen.Field) bool {
	switch f.Desc.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return isWellKnown(f.Message)
	default:
		return true
	}
}

// isWellKnown reports whether a message is a protobuf well-known type. WKTs
// are leaves: flattening never descends into them.
func isWellKnown(m *protogen.Message) bool {
	if m == nil {
		return false
	}
	switch m.Desc.FullName() {
	case "google.protobuf.Timestamp", "google.protobuf.Duration",
		"google.protobuf.Struct", "google.protobuf.Value",
		"google.protobuf.ListValue", "google.protobuf.Empty",
		"google.protobuf.FieldMask", "google.protobuf.Any",
		"google.protobuf.DoubleValue", "google.protobuf.FloatValue",
		"google.protobuf.Int64Value", "google.protobuf.UInt64Value",
		"google.protobuf.Int32Value", "google.protobuf.UInt32Value",
		"google.protobuf.BoolValue", "google.protobuf.StringValue",
		"google.protobuf.BytesValue":
		return true
	}
	return false
}

// pathString renders a field path for diagnostics.
func pathString(path []*protogen.Field) string {
	out := ""
	for i, f := range path {
		if i > 0 {
			out += "."
		}
		out += string(f.Desc.Name())
	}
	return out
}

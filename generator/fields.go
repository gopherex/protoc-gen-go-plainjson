package generator

import (
	"fmt"
	"strings"

	"github.com/gopherex/protoc-gen-go-plainjson/plainjson"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// validateFieldOptions rejects option combinations that cannot mean anything,
// so a mistake surfaces at generation time rather than as a missing key.
func validateFieldOptions(field *protogen.Field, opts *plainjson.FieldOptions) error {
	if opts == nil {
		return nil
	}
	owner := field.Parent.Desc.FullName()

	if opts.GetPick() != "" && len(opts.GetLift()) > 0 {
		return fmt.Errorf("%s.%s: pick and lift are mutually exclusive",
			owner, field.Desc.Name())
	}
	if opts.GetTag() != "" && (field.Oneof == nil || field.Oneof.Desc.IsSynthetic()) {
		return fmt.Errorf("%s.%s: tag: not a oneof branch", owner, field.Desc.Name())
	}

	isMessage := field.Message != nil && !isWellKnown(field.Message)
	if opts.GetFlatten() != plainjson.FlattenMode_FLATTEN_MODE_UNSPECIFIED && !isMessage {
		return fmt.Errorf("%s.%s: flatten: only message fields can be flattened",
			owner, field.Desc.Name())
	}
	if opts.GetFlatten() == plainjson.FlattenMode_FLATTEN_MODE_NONE &&
		(opts.GetPrefix() != "" || opts.GetSuffix() != "") {
		return fmt.Errorf("%s.%s: prefix/suffix has no effect: the field is not flattened",
			owner, field.Desc.Name())
	}

	return validateCardinality(field, opts)
}

// validateCardinality checks a collection option against the field's shape.
func validateCardinality(field *protogen.Field, opts *plainjson.FieldOptions) error {
	c := opts.GetCardinality()
	if c == plainjson.Cardinality_CARDINALITY_UNSPECIFIED {
		return nil
	}
	owner := field.Parent.Desc.FullName()

	switch c {
	case plainjson.Cardinality_CARDINALITY_KEYS,
		plainjson.Cardinality_CARDINALITY_VALUES,
		plainjson.Cardinality_CARDINALITY_INLINE_KEYS:
		if !field.Desc.IsMap() {
			return fmt.Errorf("%s.%s: %s: repeated fields have no keys",
				owner, field.Desc.Name(), c)
		}
	case plainjson.Cardinality_CARDINALITY_JOIN:
		if opts.GetPick() != "" {
			return nil // the pick reduces elements to a scalar
		}
		if elem := elementField(field); elem != nil && !isLeafKind(elem) {
			return fmt.Errorf("%s.%s: %s: elements are messages; add pick",
				owner, field.Desc.Name(), c)
		}
	}
	return nil
}

// elementField returns the field describing a collection's element: the map
// value for maps, the field itself for repeated fields.
func elementField(field *protogen.Field) *protogen.Field {
	if field.Desc.IsMap() {
		return field.Message.Fields[1]
	}
	return field
}

// jsonShape names the JSON type a field lands on, for merge compatibility.
func jsonShape(field *protogen.Field, sc scope) string {
	switch {
	case field.Desc.IsMap():
		return "object"
	case field.Desc.IsList():
		if sc.cardinality == plainjson.Cardinality_CARDINALITY_KEEP {
			return "array"
		}
	}
	if field.Message != nil && !isWellKnown(field.Message) {
		return "object"
	}
	switch field.Desc.Kind() {
	case protoreflect.BoolKind:
		return "boolean"
	case protoreflect.StringKind, protoreflect.BytesKind:
		return "string"
	case protoreflect.EnumKind:
		if sc.enumFormat == plainjson.EnumFormat_ENUM_FORMAT_NUMBER {
			return "number"
		}
		return "string"
	case protoreflect.Int64Kind, protoreflect.Uint64Kind, protoreflect.Sint64Kind,
		protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind:
		if sc.int64Format == plainjson.Int64Format_INT64_FORMAT_NUMBER {
			return "number"
		}
		return "string"
	default:
		if field.Message != nil {
			return wellKnownShape(field, sc)
		}
		return "number"
	}
}

// wellKnownShape names the JSON type of a well-known type under the scope's
// formats.
func wellKnownShape(field *protogen.Field, sc scope) string {
	switch field.Message.Desc.FullName() {
	case "google.protobuf.Timestamp":
		if sc.timeFormat == plainjson.TimeFormat_TIME_FORMAT_RFC3339 {
			return "string"
		}
		return "number"
	case "google.protobuf.Duration":
		if sc.durationFormat == plainjson.DurationFormat_DURATION_FORMAT_PROTOJSON {
			return "string"
		}
		return "number"
	case "google.protobuf.StringValue", "google.protobuf.BytesValue":
		return "string"
	case "google.protobuf.BoolValue":
		return "boolean"
	case "google.protobuf.Int64Value", "google.protobuf.UInt64Value":
		if sc.int64Format == plainjson.Int64Format_INT64_FORMAT_NUMBER {
			return "number"
		}
		return "string"
	case "google.protobuf.Int32Value", "google.protobuf.UInt32Value",
		"google.protobuf.DoubleValue", "google.protobuf.FloatValue":
		return "number"
	case "google.protobuf.ListValue":
		return "array"
	case "google.protobuf.Value":
		return "any"
	default:
		return "object"
	}
}

// ---------------------------------------------------------------------------
// pick / lift
// ---------------------------------------------------------------------------

// addPick replaces a field with one value taken from inside it.
func (p *plan) addPick(
	field *protogen.Field,
	opts *plainjson.FieldOptions,
	sc scope,
	keys keyParts,
	path []*protogen.Field,
	exclusive int,
) error {
	inner, err := p.resolveInside(field, opts.GetPick(), "pick")
	if err != nil {
		return err
	}
	if len(inner) == 0 {
		// A pick into a well-known Struct: its members only exist at run time.
		p.add(&entry{
			kind:       entryStructPick,
			key:        keys.key(sc),
			sc:         sc,
			path:       path,
			leaf:       field,
			structPath: opts.GetPick(),
			exclusive:  exclusive,
			source:     pathString(path) + "." + opts.GetPick(),
		})
		return nil
	}
	if err := requireLeaf(inner, opts.GetPick(), "pick", field.Parent); err != nil {
		return err
	}

	if field.Desc.IsList() || field.Desc.IsMap() {
		// The pick applies to every element rather than to the field, so the
		// field stays a collection and carries the inner path.
		e := &entry{
			kind:      entryCollection,
			key:       keys.key(sc),
			sc:        sc,
			path:      path,
			leaf:      field,
			pickPath:  inner,
			exclusive: exclusive,
			source:    pathString(path) + "." + opts.GetPick(),
		}
		markDynamic(e, sc, keys)
		p.add(e)
		return nil
	}

	full := append(append([]*protogen.Field(nil), path...), inner...)
	kind := entryScalar
	if last := inner[len(inner)-1]; last.Desc.IsList() || last.Desc.IsMap() {
		kind = entryCollection
	}
	p.add(&entry{
		kind:      kind,
		key:       keys.key(sc),
		sc:        sc,
		path:      full,
		leaf:      full[len(full)-1],
		exclusive: exclusive,
		source:    pathString(full),
	})
	return nil
}

// addLifts hoists chosen paths out of a subtree and drops the rest of it.
func (p *plan) addLifts(
	field *protogen.Field,
	opts *plainjson.FieldOptions,
	sc scope,
	keys keyParts,
	path []*protogen.Field,
	exclusive int,
) error {
	for _, l := range opts.GetLift() {
		inner, err := p.resolveInside(field, l.GetPath(), "lift")
		if err != nil {
			return err
		}
		if len(inner) == 0 {
			return fmt.Errorf("%s.%s: lift %q: lifting out of a well-known type is not supported; use pick",
				field.Parent.Desc.FullName(), field.Desc.Name(), l.GetPath())
		}
		if err := requireLeaf(inner, l.GetPath(), "lift", field.Parent); err != nil {
			return err
		}

		leafName := string(inner[len(inner)-1].Desc.Name())
		if l.GetAs() != "" {
			leafName = l.GetAs()
		}
		// A lifted key replaces the field's own segment rather than nesting
		// under it: the point is to pull the value up.
		liftKeys := keys
		liftKeys.segments = liftKeys.segments[:len(liftKeys.segments)-1]
		liftKeys = liftKeys.push("", "", leafName)

		full := append(append([]*protogen.Field(nil), path...), inner...)
		p.add(&entry{
			kind:      entryScalar,
			key:       liftKeys.key(sc),
			sc:        sc,
			path:      full,
			leaf:      full[len(full)-1],
			exclusive: exclusive,
			source:    pathString(full),
		})
	}
	return nil
}

// resolveInside resolves a dot path relative to a message field, including
// paths into a well-known Struct, whose members are only known at run time.
func (p *plan) resolveInside(field *protogen.Field, spec, what string) ([]*protogen.Field, error) {
	if field.Message == nil {
		return nil, fmt.Errorf("%s.%s: %s %q: not a message field",
			field.Parent.Desc.FullName(), field.Desc.Name(), what, spec)
	}
	if isWellKnown(field.Message) {
		// Struct/Value members are addressed dynamically; the field itself is
		// the source and the path is carried to the runtime helper.
		return []*protogen.Field{}, nil
	}
	inner, err := resolvePath(field.Message, spec, what)
	if err != nil {
		return nil, err
	}
	return inner, nil
}

// ---------------------------------------------------------------------------
// Collections
// ---------------------------------------------------------------------------

// addCollection appends a repeated or map field under one key, or, for the
// modes that spread it out, as a dynamic entry.
func (p *plan) addCollection(
	field *protogen.Field,
	sc scope,
	keys keyParts,
	path []*protogen.Field,
	exclusive int,
) error {
	e := &entry{
		kind:      entryCollection,
		key:       keys.key(sc),
		sc:        sc,
		path:      path,
		leaf:      field,
		exclusive: exclusive,
		source:    pathString(path),
	}
	markDynamic(e, sc, keys)
	p.add(e)
	return nil
}

// markDynamic flags the cardinality modes whose keys are invented at run time.
func markDynamic(e *entry, sc scope, keys keyParts) {
	switch sc.cardinality {
	case plainjson.Cardinality_CARDINALITY_INLINE_KEYS,
		plainjson.Cardinality_CARDINALITY_EXPLODE,
		plainjson.Cardinality_CARDINALITY_INDEXED:
		e.dynamic = true
		e.keyPrefix = strings.Join(keys.prefixes, "")
	}
}

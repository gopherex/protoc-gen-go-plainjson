package plainjsonpb

import (
	"github.com/go-faster/jx"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// The well-known types whose JSON is defined by their content rather than by
// their fields are written here by hand. Going through protojson would pull a
// reflective marshaler into the hot path and, worse, emit map keys in Go's
// random order — the generated output has to be byte-stable.

// Struct writes a google.protobuf.Struct as a JSON object, keys sorted so the
// output does not depend on map iteration order.
func Struct(e *jx.Encoder, s *structpb.Struct) {
	if s == nil {
		e.Null()
		return
	}
	fields := s.GetFields()
	e.ObjStart()
	for _, k := range SortedKeys(fields) {
		e.FieldStart(k)
		Value(e, fields[k])
	}
	e.ObjEnd()
}

// ListValue writes a google.protobuf.ListValue as a JSON array.
func ListValue(e *jx.Encoder, l *structpb.ListValue) {
	if l == nil {
		e.Null()
		return
	}
	e.ArrStart()
	for _, v := range l.GetValues() {
		Value(e, v)
	}
	e.ArrEnd()
}

// Value writes a google.protobuf.Value as the JSON value it stands for.
func Value(e *jx.Encoder, v *structpb.Value) {
	if v == nil {
		e.Null()
		return
	}
	switch k := v.GetKind().(type) {
	case *structpb.Value_NullValue:
		e.Null()
	case *structpb.Value_NumberValue:
		Float64(e, k.NumberValue)
	case *structpb.Value_StringValue:
		e.Str(k.StringValue)
	case *structpb.Value_BoolValue:
		e.Bool(k.BoolValue)
	case *structpb.Value_StructValue:
		Struct(e, k.StructValue)
	case *structpb.Value_ListValue:
		ListValue(e, k.ListValue)
	default:
		e.Null()
	}
}

// Any writes a google.protobuf.Any as its type URL plus, when the wrapped
// message is itself plainjson-generated, that message's flattened fields.
//
// A type the registry cannot resolve, or one with no generated marshaler,
// degrades to {"@type": …} rather than failing: the output is lossy by design,
// so a missing payload is better than a broken encode.
func Any(e *jx.Encoder, a *anypb.Any) error {
	if a == nil {
		e.Null()
		return nil
	}

	msg, err := a.UnmarshalNew()
	if err != nil {
		typeOnly(e, a.GetTypeUrl())
		return nil
	}
	m, ok := msg.(Marshaler)
	if !ok {
		typeOnly(e, a.GetTypeUrl())
		return nil
	}

	raw, err := m.MarshalPlainJSON()
	if err != nil {
		return err
	}
	e.ObjStart()
	e.FieldStart("@type")
	e.Str(a.GetTypeUrl())
	if err := spread(raw, "", nil, "", func(key string, value []byte) {
		e.FieldStart(key)
		e.Raw(value)
	}); err != nil {
		return err
	}
	e.ObjEnd()
	return nil
}

// typeOnly writes the degraded form of an Any.
func typeOnly(e *jx.Encoder, url string) {
	e.ObjStart()
	e.FieldStart("@type")
	e.Str(url)
	e.ObjEnd()
}

// PickStruct resolves a dot path inside a google.protobuf.Struct. Struct
// members exist only at run time, so a pick into one is resolved here rather
// than at generation time.
func PickStruct(s *structpb.Struct, path string) (*structpb.Value, bool) {
	var cur *structpb.Value
	fields := s.GetFields()
	for _, part := range splitPath(path) {
		v, ok := fields[part]
		if !ok {
			return nil, false
		}
		cur = v
		fields = v.GetStructValue().GetFields()
	}
	return cur, cur != nil
}

// splitPath splits a dot path without pulling in strings for one call.
func splitPath(path string) []string {
	var out []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			out = append(out, path[start:i])
			start = i + 1
		}
	}
	return append(out, path[start:])
}

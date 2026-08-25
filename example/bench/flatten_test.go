package bench_test

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/go-faster/jx"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The two implementations below are what this plugin exists to replace: the
// same flattening, written by hand. They are the honest baselines to benchmark
// against, and a test asserts all three agree on the result.

// flattenViaJSON is the obvious approach: let protojson produce the nested
// document, walk it as a map, and re-encode the leaves at the top level.
func flattenViaJSON(m proto.Message) ([]byte, error) {
	raw, err := protojson.Marshal(m)
	if err != nil {
		return nil, err
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}

	flat := make(map[string]any, len(tree)*2)
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		keys := make([]string, 0, len(node))
		for k := range node {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch v := node[k].(type) {
			case map[string]any:
				walk(v)
			default:
				key := snake(k)
				if _, taken := flat[key]; !taken {
					flat[key] = v
				}
			}
		}
	}
	walk(tree)
	return json.Marshal(flat)
}

// flattenViaReflect skips the JSON round trip and walks the descriptors
// directly, writing into jx — the fastest a hand-written flattener gets
// without generating code.
func flattenViaReflect(m proto.Message) ([]byte, error) {
	var e jx.Encoder
	e.ObjStart()
	seen := make(map[string]bool, 32)

	var walk func(msg protoreflect.Message) error
	walk = func(msg protoreflect.Message) error {
		fields := msg.Descriptor().Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			if !msg.Has(fd) {
				continue
			}
			v := msg.Get(fd)

			if fd.Kind() == protoreflect.MessageKind && !fd.IsList() && !fd.IsMap() &&
				!isWellKnownName(string(fd.Message().FullName())) {
				if err := walk(v.Message()); err != nil {
					return err
				}
				continue
			}

			key := string(fd.Name())
			if seen[key] {
				continue
			}
			seen[key] = true
			e.FieldStart(key)
			if err := writeReflect(&e, fd, v); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(m.ProtoReflect()); err != nil {
		return nil, err
	}
	e.ObjEnd()
	return e.Bytes(), nil
}

// writeReflect writes one value the protojson way, by descriptor kind.
func writeReflect(e *jx.Encoder, fd protoreflect.FieldDescriptor, v protoreflect.Value) error {
	if fd.IsList() {
		list := v.List()
		e.ArrStart()
		for i := 0; i < list.Len(); i++ {
			if err := writeScalarReflect(e, fd, list.Get(i)); err != nil {
				return err
			}
		}
		e.ArrEnd()
		return nil
	}
	return writeScalarReflect(e, fd, v)
}

// writeScalarReflect writes a single value.
func writeScalarReflect(e *jx.Encoder, fd protoreflect.FieldDescriptor, v protoreflect.Value) error {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		e.Bool(v.Bool())
	case protoreflect.StringKind:
		e.Str(v.String())
	case protoreflect.BytesKind:
		e.Str(base64(v.Bytes()))
	case protoreflect.EnumKind:
		values := fd.Enum().Values()
		if vd := values.ByNumber(v.Enum()); vd != nil {
			e.Str(string(vd.Name()))
		} else {
			e.Int32(int32(v.Enum()))
		}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		e.Int32(int32(v.Int()))
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		e.UInt32(uint32(v.Uint()))
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		e.Str(strconv.FormatInt(v.Int(), 10))
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		e.Str(strconv.FormatUint(v.Uint(), 10))
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		e.Float64(v.Float())
	case protoreflect.MessageKind:
		// A well-known type: fall back to protojson for its content.
		raw, err := protojson.Marshal(v.Message().Interface())
		if err != nil {
			return err
		}
		e.Raw(raw)
	default:
		e.Null()
	}
	return nil
}

// isWellKnownName reports the message types a flattener must not descend into.
func isWellKnownName(name string) bool {
	return strings.HasPrefix(name, "google.protobuf.")
}

// snake converts protojson's lowerCamel key back to the proto spelling.
func snake(key string) string {
	var b strings.Builder
	for i, r := range key {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// base64 encodes bytes the protojson way.
func base64(b []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		var chunk [3]byte
		n := copy(chunk[:], b[i:])
		out.WriteByte(alphabet[chunk[0]>>2])
		out.WriteByte(alphabet[(chunk[0]&0x03)<<4|chunk[1]>>4])
		if n > 1 {
			out.WriteByte(alphabet[(chunk[1]&0x0f)<<2|chunk[2]>>6])
		} else {
			out.WriteByte('=')
		}
		if n > 2 {
			out.WriteByte(alphabet[chunk[2]&0x3f])
		} else {
			out.WriteByte('=')
		}
	}
	return out.String()
}

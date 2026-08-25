package generator

import (
	"fmt"
	"strconv"

	"github.com/gopherex/protoc-gen-go-plainjson/plainjson"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// scalar emits one value: a scalar, an enum, or a well-known type.
func (w *writer) scalar(e *entry) error {
	leaf := e.leaf
	g := w.g

	// A branch of a oneof holding a scalar has presence only through its
	// wrapper type, so it is reached by a type assertion.
	if isScalarBranch(leaf) {
		parent := "m"
		if len(e.path) > 1 {
			parent = parentChain(e.path)
		}
		g.P("if b, ok := ", parent, ".Get", leaf.Oneof.GoName, "().(*", leaf.GoIdent, "); ok {")
		w.write(e, func() { w.value("b."+leaf.GoName, leaf, e.sc) })
		g.P("}")
		return nil
	}

	if e.sc.emitEmpty {
		w.write(e, func() { w.value(chain(e.path), leaf, e.sc) })
		return nil
	}

	if omitter, ok := w.em.enumOmitter(leaf.Enum); ok {
		// Some values of this enum are not emitted at all.
		g.P("if v := ", chain(e.path), "; !", omitter, "(v) {")
		if e.sc.emitEmpty {
			w.write(e, func() { w.value("v", leaf, e.sc) })
		} else {
			g.P("if ", zeroTest("v", leaf), " {")
			w.write(e, func() { w.value("v", leaf, e.sc) })
			g.P("}")
		}
		g.P("}")
		return nil
	}

	switch {
	case leaf.Message != nil:
		// Message-shaped leaves (the well-known types) carry presence.
		g.P("if v := ", chain(e.path), "; v != nil {")
		w.write(e, func() { w.value("v", leaf, e.sc) })
		g.P("}")

	case leaf.Desc.HasPresence():
		parent := "m"
		if len(e.path) > 1 {
			parent = parentChain(e.path)
			g.P("if p := ", parent, "; p != nil && p.", leaf.GoName, " != nil {")
		} else {
			g.P("if p := m; p.", leaf.GoName, " != nil {")
		}
		w.write(e, func() { w.value("*p."+leaf.GoName, leaf, e.sc) })
		g.P("}")

	default:
		g.P("if v := ", chain(e.path), "; ", zeroTest("v", leaf), " {")
		w.write(e, func() { w.value("v", leaf, e.sc) })
		g.P("}")
	}
	return nil
}

// isScalarBranch reports whether a field is a oneof branch whose getter cannot
// tell "unset" from "set to zero".
func isScalarBranch(f *protogen.Field) bool {
	return f.Oneof != nil && !f.Oneof.Desc.IsSynthetic() && f.Message == nil
}

// zeroTest renders the condition under which a value is worth writing.
func zeroTest(expr string, f *protogen.Field) string {
	switch f.Desc.Kind() {
	case protoreflect.BoolKind:
		return expr
	case protoreflect.StringKind:
		return expr + ` != ""`
	case protoreflect.BytesKind:
		return "len(" + expr + ") > 0"
	default:
		return expr + " != 0"
	}
}

// value writes one value into the encoder.
func (w *writer) value(expr string, f *protogen.Field, sc scope) {
	g := w.g
	if f.Message != nil {
		w.wellKnown(expr, f, sc)
		return
	}

	switch f.Desc.Kind() {
	case protoreflect.BoolKind:
		g.P("e.Bool(", expr, ")")
	case protoreflect.StringKind:
		g.P("e.Str(", expr, ")")
	case protoreflect.BytesKind:
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Bytes")), "(e, ", expr, ", ", int32(sc.bytesFormat), ")")
	case protoreflect.EnumKind:
		w.enum(expr, f, sc)
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		g.P("e.Int32(", expr, ")")
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		g.P("e.UInt32(", expr, ")")
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Int64")), "(e, ", expr, ", ",
			sc.int64Format == plainjson.Int64Format_INT64_FORMAT_NUMBER, ")")
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		g.P(g.QualifiedGoIdent(pjPkg.Ident("UInt64")), "(e, ", expr, ", ",
			sc.int64Format == plainjson.Int64Format_INT64_FORMAT_NUMBER, ")")
	case protoreflect.FloatKind:
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Float32")), "(e, ", expr, ")")
	case protoreflect.DoubleKind:
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Float64")), "(e, ", expr, ")")
	default:
		g.P("e.Null() // unsupported kind ", f.Desc.Kind().String())
	}
}

// wellKnown writes a well-known type in its protojson form, or in the format
// the scope asks for.
func (w *writer) wellKnown(expr string, f *protogen.Field, sc scope) {
	g := w.g
	switch f.Message.Desc.FullName() {
	case "google.protobuf.Timestamp":
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Timestamp")), "(e, ", expr, ", ", int32(sc.timeFormat), ")")
	case "google.protobuf.Duration":
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Duration")), "(e, ", expr, ", ", int32(sc.durationFormat), ")")
	case "google.protobuf.FieldMask":
		g.P(g.QualifiedGoIdent(pjPkg.Ident("FieldMask")), "(e, ", expr, ", 0)")
	case "google.protobuf.Empty":
		g.P("e.ObjStart()")
		g.P("e.ObjEnd()")
	case "google.protobuf.StringValue":
		g.P("e.Str(", expr, ".GetValue())")
	case "google.protobuf.BoolValue":
		g.P("e.Bool(", expr, ".GetValue())")
	case "google.protobuf.BytesValue":
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Bytes")), "(e, ", expr, ".GetValue(), ", int32(sc.bytesFormat), ")")
	case "google.protobuf.Int32Value":
		g.P("e.Int32(", expr, ".GetValue())")
	case "google.protobuf.UInt32Value":
		g.P("e.UInt32(", expr, ".GetValue())")
	case "google.protobuf.Int64Value":
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Int64")), "(e, ", expr, ".GetValue(), ",
			sc.int64Format == plainjson.Int64Format_INT64_FORMAT_NUMBER, ")")
	case "google.protobuf.UInt64Value":
		g.P(g.QualifiedGoIdent(pjPkg.Ident("UInt64")), "(e, ", expr, ".GetValue(), ",
			sc.int64Format == plainjson.Int64Format_INT64_FORMAT_NUMBER, ")")
	case "google.protobuf.FloatValue":
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Float32")), "(e, ", expr, ".GetValue())")
	case "google.protobuf.DoubleValue":
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Float64")), "(e, ", expr, ".GetValue())")
	case "google.protobuf.Struct":
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Struct")), "(e, ", expr, ")")
	case "google.protobuf.Value":
		g.P(g.QualifiedGoIdent(pjPkg.Ident("Value")), "(e, ", expr, ")")
	case "google.protobuf.ListValue":
		g.P(g.QualifiedGoIdent(pjPkg.Ident("ListValue")), "(e, ", expr, ")")
	case "google.protobuf.Any":
		g.P("if err := ", g.QualifiedGoIdent(pjPkg.Ident("Any")), "(e, ", expr, "); err != nil {")
		g.P("return err")
		g.P("}")
	default:
		// Any other message reached as a leaf is written as an empty object:
		// there is nothing generated to describe it.
		g.P("e.ObjStart()")
		g.P("e.ObjEnd()")
	}
}

// enum writes an enum value as a name or a number, honouring the enum-level
// and value-level options through a generated helper.
func (w *writer) enum(expr string, f *protogen.Field, sc scope) {
	g := w.g
	if sc.enumFormat == plainjson.EnumFormat_ENUM_FORMAT_NUMBER {
		g.P("e.Int32(int32(", expr, "))")
		return
	}
	g.P("e.Str(", w.em.enumNamer(f.Enum), "(", expr, "))")
}

// enumNamer returns the helper rendering an enum's JSON names. The helper
// itself is written by declareEnums before any message body, since a Go
// function cannot be declared inside another one.
func (em *emitter) enumNamer(enum *protogen.Enum) string {
	return em.enums[string(enum.Desc.FullName())]
}

// declareEnum writes the name helper for one enum.
func (em *emitter) declareEnum(enum *protogen.Enum) {
	key := string(enum.Desc.FullName())
	if _, ok := em.enums[key]; ok {
		return
	}
	name := em.uniqueEnumName("plainjsonEnumName_" + goEnumSymbol(enum))
	em.enums[key] = name

	g := em.g
	opts := enumOptions(enum)
	g.P("// ", name, " renders ", enum.Desc.FullName(), " the way its options ask for.")
	g.P("func ", name, "(v ", enum.GoIdent, ") string {")
	g.P("switch v {")
	for _, value := range enum.Values {
		rendered := string(value.Desc.Name())
		if vo := enumValueOptions(value); vo.GetName() != "" {
			rendered = vo.GetName()
		} else if opts.GetStripPrefix() {
			rendered = stripEnumPrefix(enum, rendered)
		}
		g.P("case ", value.GoIdent, ":")
		g.P("return ", strconv.Quote(rendered))
	}
	g.P("}")
	g.P("return v.String()")
	g.P("}")
	g.P()
}

// uniqueEnumName keeps helper names distinct when two enums from different
// packages share a Go name.
func (em *emitter) uniqueEnumName(name string) string {
	taken := func(candidate string) bool {
		for _, used := range em.enums {
			if used == candidate {
				return true
			}
		}
		return false
	}
	unique := name
	for i := 2; taken(unique); i++ {
		unique = name + "_" + strconv.Itoa(i)
	}
	return unique
}

// enumOmitter returns the predicate reporting values that are not emitted, if
// the enum has any.
func (em *emitter) enumOmitter(enum *protogen.Enum) (string, bool) {
	if enum == nil {
		return "", false
	}
	name, ok := em.enums["omit|"+string(enum.Desc.FullName())]
	return name, ok
}

// declareEnumOmitter writes the predicate for values an enum drops entirely.
func (em *emitter) declareEnumOmitter(enum *protogen.Enum) {
	var omitted []*protogen.EnumValue
	for _, value := range enum.Values {
		if enumValueOptions(value).GetOmit() {
			omitted = append(omitted, value)
		}
	}
	key := "omit|" + string(enum.Desc.FullName())
	if len(omitted) == 0 {
		return
	}
	if _, ok := em.enums[key]; ok {
		return
	}
	name := em.uniqueEnumName("plainjsonEnumOmit_" + goEnumSymbol(enum))
	em.enums[key] = name

	g := em.g
	g.P("// ", name, " reports values of ", enum.Desc.FullName(), " that are not emitted.")
	g.P("func ", name, "(v ", enum.GoIdent, ") bool {")
	g.P("switch v {")
	for _, value := range omitted {
		g.P("case ", value.GoIdent, ":")
		g.P("return true")
	}
	g.P("}")
	g.P("return false")
	g.P("}")
	g.P()
}

// goEnumSymbol renders an enum's name as a Go identifier fragment.
func goEnumSymbol(enum *protogen.Enum) string {
	return fmt.Sprintf("%s", enum.GoIdent.GoName)
}

// stripEnumPrefix removes the SCREAMING_SNAKE type prefix from a value name.
func stripEnumPrefix(enum *protogen.Enum, value string) string {
	prefix := screamingSnake(string(enum.Desc.Name())) + "_"
	if len(value) > len(prefix) && value[:len(prefix)] == prefix {
		return value[len(prefix):]
	}
	return value
}

// screamingSnake renders a name in SCREAMING_SNAKE_CASE.
func screamingSnake(name string) string {
	words := splitWords(name)
	for i, w := range words {
		words[i] = upperASCII(w)
	}
	out := ""
	for i, w := range words {
		if i > 0 {
			out += "_"
		}
		out += w
	}
	return out
}

// upperASCII uppercases an ASCII word.
func upperASCII(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'a' && c <= 'z' {
			out[i] = c - 'a' + 'A'
		}
	}
	return string(out)
}

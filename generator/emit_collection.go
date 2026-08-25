package generator

import (
	"strconv"

	"github.com/gopherex/protoc-gen-go-plainjson/plainjson"
	"google.golang.org/protobuf/compiler/protogen"
)

// collection emits a repeated or map field under the cardinality its scope
// asks for. Cardinality describes how many values a field holds, so the same
// modes cover repeated fields and maps alike.
func (w *writer) collection(e *entry) error {
	expr := chain(e.path)
	isMap := e.leaf.Desc.IsMap()

	switch e.sc.cardinality {
	case plainjson.Cardinality_CARDINALITY_COUNT:
		w.guardNonEmpty(e, expr, func() {
			w.write(e, func() { w.g.P("e.Int(len(", expr, "))") })
		})
	case plainjson.Cardinality_CARDINALITY_FIRST, plainjson.Cardinality_CARDINALITY_LAST:
		return w.collectionPick(e, expr, isMap)
	case plainjson.Cardinality_CARDINALITY_JOIN:
		return w.collectionJoin(e, expr, isMap)
	case plainjson.Cardinality_CARDINALITY_KEYS:
		w.guardNonEmpty(e, expr, func() {
			w.write(e, func() {
				w.g.P("e.ArrStart()")
				w.g.P("for _, k := range ", w.sortedKeys(expr), " {")
				w.g.P("e.Str(", w.g.QualifiedGoIdent(pjPkg.Ident("KeyString")), "(k))")
				w.g.P("}")
				w.g.P("e.ArrEnd()")
			})
		})
	case plainjson.Cardinality_CARDINALITY_VALUES:
		w.guardNonEmpty(e, expr, func() {
			w.write(e, func() {
				w.g.P("e.ArrStart()")
				w.g.P("for _, k := range ", w.sortedKeys(expr), " {")
				w.element(e, expr+"[k]", mapValueField(e.leaf))
				w.g.P("}")
				w.g.P("e.ArrEnd()")
			})
		})
	case plainjson.Cardinality_CARDINALITY_INLINE_KEYS:
		return w.collectionInlineKeys(e, expr)
	case plainjson.Cardinality_CARDINALITY_INDEXED:
		return w.collectionIndexed(e, expr, isMap)
	case plainjson.Cardinality_CARDINALITY_EXPLODE:
		return w.collectionExplode(e, expr)
	default:
		return w.collectionKeep(e, expr, isMap)
	}
	return nil
}

// collectionValue writes a collection as a value, without a key or any of the
// collision machinery. Used where a collection lands somewhere that already
// decided on the key, such as a merge rule.
func (w *writer) collectionValue(e *entry, expr string, field *protogen.Field) {
	g := w.g
	isMap := field.Desc.IsMap()
	elem := field
	if isMap {
		elem = mapValueField(field)
	}

	switch e.sc.cardinality {
	case plainjson.Cardinality_CARDINALITY_COUNT:
		g.P("e.Int(len(", expr, "))")
	case plainjson.Cardinality_CARDINALITY_JOIN:
		g.P("{")
		g.P("var sb ", g.QualifiedGoIdent(stringsPkg.Ident("Builder")))
		if isMap {
			g.P("for i, k := range ", w.sortedKeys(expr), " {")
			g.P("if i > 0 {")
			g.P("sb.WriteString(", strconv.Quote(e.sc.joinSep), ")")
			g.P("}")
			g.P("sb.WriteString(", w.joinString(e, expr+"[k]", elem), ")")
			g.P("}")
		} else {
			g.P("for i, item := range ", expr, " {")
			g.P("if i > 0 {")
			g.P("sb.WriteString(", strconv.Quote(e.sc.joinSep), ")")
			g.P("}")
			g.P("sb.WriteString(", w.joinString(e, "item", elem), ")")
			g.P("}")
		}
		g.P("e.Str(sb.String())")
		g.P("}")
	default:
		if isMap {
			g.P("e.ObjStart()")
			g.P("for _, k := range ", w.sortedKeys(expr), " {")
			g.P("e.FieldStart(", g.QualifiedGoIdent(pjPkg.Ident("KeyString")), "(k))")
			w.element(e, expr+"[k]", elem)
			g.P("}")
			g.P("e.ObjEnd()")
			return
		}
		g.P("e.ArrStart()")
		g.P("for _, item := range ", expr, " {")
		w.element(e, "item", elem)
		g.P("}")
		g.P("e.ArrEnd()")
	}
}

// guardNonEmpty wraps a write in the emptiness check, unless emit_empty asked
// for the key regardless.
func (w *writer) guardNonEmpty(e *entry, expr string, body func()) {
	if e.sc.emitEmpty {
		body()
		return
	}
	w.g.P("if len(", expr, ") > 0 {")
	body()
	w.g.P("}")
}

// collectionKeep writes a repeated field as an array and a map as an object.
func (w *writer) collectionKeep(e *entry, expr string, isMap bool) error {
	g := w.g
	w.guardNonEmpty(e, expr, func() {
		w.write(e, func() {
			if isMap {
				g.P("e.ObjStart()")
				g.P("for _, k := range ", w.sortedKeys(expr), " {")
				g.P("e.FieldStart(", g.QualifiedGoIdent(pjPkg.Ident("KeyString")), "(k))")
				w.element(e, expr+"[k]", mapValueField(e.leaf))
				g.P("}")
				g.P("e.ObjEnd()")
				return
			}
			g.P("e.ArrStart()")
			g.P("for _, item := range ", expr, " {")
			w.element(e, "item", e.leaf)
			g.P("}")
			g.P("e.ArrEnd()")
		})
	})
	return nil
}

// collectionPick writes a single element: the first or the last one. Maps
// iterate sorted by key, so this is the lowest or highest key.
func (w *writer) collectionPick(e *entry, expr string, isMap bool) error {
	g := w.g
	last := e.sc.cardinality == plainjson.Cardinality_CARDINALITY_LAST

	g.P("if len(", expr, ") > 0 {")
	if isMap {
		g.P("ks := ", w.sortedKeys(expr))
		idx := "0"
		if last {
			idx = "len(ks)-1"
		}
		w.write(e, func() { w.element(e, expr+"[ks["+idx+"]]", mapValueField(e.leaf)) })
	} else {
		idx := "0"
		if last {
			idx = "len(" + expr + ")-1"
		}
		w.write(e, func() { w.element(e, expr+"["+idx+"]", e.leaf) })
	}
	g.P("}")
	return nil
}

// collectionJoin concatenates scalar elements into one string.
func (w *writer) collectionJoin(e *entry, expr string, isMap bool) error {
	g := w.g
	elem := e.leaf
	if isMap {
		elem = mapValueField(e.leaf)
	}

	w.guardNonEmpty(e, expr, func() {
		w.write(e, func() {
			g.P("{")
			g.P("var sb ", g.QualifiedGoIdent(stringsPkg.Ident("Builder")))
			if isMap {
				g.P("for i, k := range ", w.sortedKeys(expr), " {")
				g.P("if i > 0 {")
				g.P("sb.WriteString(", strconv.Quote(e.sc.joinSep), ")")
				g.P("}")
				g.P("sb.WriteString(", w.joinString(e, expr+"[k]", elem), ")")
				g.P("}")
			} else {
				g.P("for i, item := range ", expr, " {")
				g.P("if i > 0 {")
				g.P("sb.WriteString(", strconv.Quote(e.sc.joinSep), ")")
				g.P("}")
				g.P("sb.WriteString(", w.joinString(e, "item", elem), ")")
				g.P("}")
			}
			g.P("e.Str(sb.String())")
			g.P("}")
		})
	})
	return nil
}

// collectionInlineKeys promotes a map's keys to keys of the parent object.
// The keys only exist at run time, so they go through the key tracker.
func (w *writer) collectionInlineKeys(e *entry, expr string) error {
	g := w.g
	g.P("for _, k := range ", w.sortedKeys(expr), " {")
	g.P("key := ", strconv.Quote(e.keyPrefix), " + ", g.QualifiedGoIdent(pjPkg.Ident("KeyString")), "(k)")
	w.writeDynamic(e, "key", func() { w.element(e, expr+"[k]", mapValueField(e.leaf)) })
	g.P("}")
	return nil
}

// collectionIndexed writes one key per element, numbered by position.
func (w *writer) collectionIndexed(e *entry, expr string, isMap bool) error {
	g := w.g
	base := e.key + e.sc.indexSep

	if isMap {
		g.P("for i, k := range ", w.sortedKeys(expr), " {")
		g.P("key := ", strconv.Quote(base), " + ", g.QualifiedGoIdent(strconvPkg.Ident("Itoa")), "(i)")
		w.writeDynamic(e, "key", func() { w.element(e, expr+"[k]", mapValueField(e.leaf)) })
		g.P("}")
		return nil
	}

	elemMsg := e.leaf.Message
	if elemMsg != nil && !isWellKnown(elemMsg) {
		// Message elements keep flattening, with the index folded into the
		// prefix of every key they produce.
		fn := w.em.encoderName(elemMsg, e.sc)
		g.P("for i, item := range ", expr, " {")
		g.P("prefix := ", strconv.Quote(base), " + ", g.QualifiedGoIdent(strconvPkg.Ident("Itoa")), "(i) + ",
			strconv.Quote(e.sc.indexSep))
		g.P("raw, err := ", g.QualifiedGoIdent(pjPkg.Ident("Encoded")), "(func(e *",
			g.QualifiedGoIdent(jxPkg.Ident("Encoder")), ") error { return ", fn, "(item, e) })")
		g.P("if err != nil {")
		g.P("return err")
		g.P("}")
		w.spread(e, "raw", "prefix")
		g.P("}")
		return nil
	}

	g.P("for i, item := range ", expr, " {")
	g.P("key := ", strconv.Quote(base), " + ", g.QualifiedGoIdent(strconvPkg.Ident("Itoa")), "(i)")
	w.writeDynamic(e, "key", func() { w.element(e, "item", e.leaf) })
	g.P("}")
	return nil
}

// collectionExplode merges every element's keys into the parent object.
func (w *writer) collectionExplode(e *entry, expr string) error {
	g := w.g
	elemMsg := e.leaf.Message
	if elemMsg == nil || isWellKnown(elemMsg) {
		// Scalar elements have no keys of their own; fall back to the field's
		// own key, so only the first element can win.
		return w.collectionPick(e, expr, e.leaf.Desc.IsMap())
	}

	fn := w.em.encoderName(elemMsg, e.sc)
	g.P("for _, item := range ", expr, " {")
	g.P("raw, err := ", g.QualifiedGoIdent(pjPkg.Ident("Encoded")), "(func(e *",
		g.QualifiedGoIdent(jxPkg.Ident("Encoder")), ") error { return ", fn, "(item, e) })")
	g.P("if err != nil {")
	g.P("return err")
	g.P("}")
	w.spread(e, "raw", `""`)
	g.P("}")
	return nil
}

// spread copies the pairs of an already encoded object into the current one,
// under the collision machinery in force.
func (w *writer) spread(e *entry, raw, prefix string) {
	g := w.g
	target := "e"
	if w.plan.needsBuffer {
		target = "obj"
	}
	fn := "Spread"
	if w.plan.needsBuffer {
		fn = "SpreadTo"
	}
	g.P("if err := ", g.QualifiedGoIdent(pjPkg.Ident(fn)), "(", target, ", ",
		w.keysArg(), ", ", raw, ", ", prefix, ", ", strconv.Quote(e.source), "); err != nil {")
	g.P("return err")
	g.P("}")
}

// writeDynamic writes a key computed at run time.
func (w *writer) writeDynamic(e *entry, keyVar string, value func()) {
	g := w.g
	g.P("if ok, err := keys.Claim(", keyVar, ", ", strconv.Quote(e.source), "); err != nil {")
	g.P("return err")
	g.P("} else if ok {")
	if w.plan.needsBuffer {
		g.P("if err := obj.Set(", keyVar, ", func(e *", g.QualifiedGoIdent(jxPkg.Ident("Encoder")), ") error {")
		value()
		g.P("return nil")
		g.P("}); err != nil {")
		g.P("return err")
		g.P("}")
	} else {
		g.P("e.FieldStart(", keyVar, ")")
		value()
	}
	g.P("}")
}

// keysArg renders the key tracker argument, which is nil when the plan does
// not need one.
func (w *writer) keysArg() string {
	if w.plan.needsTracker {
		return "keys"
	}
	return "nil"
}

// element writes one element of a collection, following the pick path when
// one reduces the element to a value.
func (w *writer) element(e *entry, expr string, field *protogen.Field) {
	if len(e.pickPath) > 0 {
		expr, field = pickExpr(expr, e.pickPath)
	}
	if field.Message != nil && !isWellKnown(field.Message) {
		fn := w.em.encoderName(field.Message, e.sc)
		w.g.P("if err := ", fn, "(", expr, ", e); err != nil {")
		w.g.P("return err")
		w.g.P("}")
		return
	}
	w.value(expr, field, e.sc)
}

// sortedKeys renders the call listing a map's keys in a stable order.
func (w *writer) sortedKeys(expr string) string {
	return w.g.QualifiedGoIdent(pjPkg.Ident("SortedKeys")) + "(" + expr + ")"
}

// joinString renders one element as a Go string expression, applying the pick
// path first when there is one.
func (w *writer) joinString(e *entry, expr string, field *protogen.Field) string {
	if len(e.pickPath) > 0 {
		expr, field = pickExpr(expr, e.pickPath)
	}
	return w.stringOf(expr, field, e.sc)
}

// stringOf renders a scalar as a Go string expression, for JOIN.
func (w *writer) stringOf(expr string, field *protogen.Field, sc scope) string {
	g := w.g
	switch field.Desc.Kind() {
	case 9: // string
		return expr
	case 14: // enum
		if sc.enumFormat == plainjson.EnumFormat_ENUM_FORMAT_NUMBER {
			return g.QualifiedGoIdent(strconvPkg.Ident("Itoa")) + "(int(" + expr + "))"
		}
		return w.em.enumNamer(field.Enum) + "(" + expr + ")"
	case 8: // bool
		return g.QualifiedGoIdent(strconvPkg.Ident("FormatBool")) + "(" + expr + ")"
	case 1, 2: // double, float
		return g.QualifiedGoIdent(strconvPkg.Ident("FormatFloat")) + "(float64(" + expr + "), 'g', -1, 64)"
	case 4, 6, 8 + 100: // uint64 family placeholder
		return g.QualifiedGoIdent(strconvPkg.Ident("FormatUint")) + "(uint64(" + expr + "), 10)"
	default:
		return g.QualifiedGoIdent(strconvPkg.Ident("FormatInt")) + "(int64(" + expr + "), 10)"
	}
}

// pickExpr applies a pick path to an element expression.
func pickExpr(expr string, path []*protogen.Field) (string, *protogen.Field) {
	for _, f := range path {
		expr += ".Get" + f.GoName + "()"
	}
	return expr, path[len(path)-1]
}

// mapValueField returns the field describing a map's value.
func mapValueField(field *protogen.Field) *protogen.Field {
	return field.Message.Fields[1]
}

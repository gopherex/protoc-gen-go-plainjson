package generator

import (
	"strconv"

	"github.com/gopherex/protoc-gen-go-plainjson/plainjson"
	"google.golang.org/protobuf/compiler/protogen"
)

// buildPlanWith resolves a message reached with an inherited formatting scope.
// Shape, depth and collision handling belong to the message; value formats are
// inherited from wherever it is used, then refined by its own options.
func buildPlanWith(msg *protogen.Message, outer scope) (*plan, error) {
	own := interiorScope(msg)

	root := outer
	root.flatten = own.flatten
	if outer.depthExhausted {
		root.flatten = plainjson.FlattenMode_FLATTEN_MODE_NONE
	}
	root.maxDepth = own.maxDepth
	root.collisionPolicy = own.collisionPolicy
	root.collisionWins = own.collisionWins
	root = root.applyMessage(messageOptions(msg))
	if outer.depthExhausted {
		root.flatten = plainjson.FlattenMode_FLATTEN_MODE_NONE
		root.depthExhausted = true
	}

	return buildPlanIn(msg, root)
}

// object emits a message field kept behind its own key.
func (w *writer) object(e *entry) error {
	fn := w.em.encoderName(e.leaf.Message, e.sc)
	g := w.g

	if e.sc.emitEmpty {
		w.write(e, func() {
			g.P("if err := ", fn, "(", chain(e.path), ", e); err != nil {")
			g.P("return err")
			g.P("}")
		})
		return nil
	}

	g.P("if v := ", chain(e.path), "; v != nil {")
	w.write(e, func() {
		g.P("if err := ", fn, "(v, e); err != nil {")
		g.P("return err")
		g.P("}")
	})
	g.P("}")
	return nil
}

// discriminator emits the tag naming a oneof's active branch.
func (w *writer) discriminator(e *entry) error {
	g := w.g
	g.P("switch ", oneofOwner(e), ".Get", e.oneof.GoName, "().(type) {")
	for _, branch := range e.oneof.Fields {
		g.P("case *", branch.GoIdent, ":")
		tag := e.branchTags[branch]
		w.write(e, func() { g.P("e.Str(", strconv.Quote(tag), ")") })
	}
	if !e.omitIfUnset {
		g.P("default:")
		w.write(e, func() { g.P("e.Null()") })
	}
	g.P("}")
	return nil
}

// oneofValue emits the active branch's value under a single key.
func (w *writer) oneofValue(e *entry) error {
	g := w.g
	g.P("switch b := ", oneofOwner(e), ".Get", e.oneof.GoName, "().(type) {")
	for _, branch := range e.oneof.Fields {
		g.P("case *", branch.GoIdent, ":")
		expr := "b." + branch.GoName
		w.write(e, func() {
			if branch.Message != nil && !isWellKnown(branch.Message) {
				fn := w.em.encoderName(branch.Message, e.sc)
				g.P("if err := ", fn, "(", expr, ", e); err != nil {")
				g.P("return err")
				g.P("}")
				return
			}
			w.value(expr, branch, e.sc)
		})
	}
	if !e.omitIfUnset {
		g.P("default:")
		w.write(e, func() { g.P("e.Null()") })
	}
	g.P("}")
	return nil
}

// oneofOwner renders the expression holding the oneof: the receiver, or the
// nested message the walk reached it through.
func oneofOwner(e *entry) string {
	if len(e.path) == 0 {
		return "m"
	}
	return chain(e.path)
}

// merge emits a merge rule: the first (or last) source that holds a value
// wins, and MERGE_CONFLICT_ERROR reports several live sources.
func (w *writer) merge(e *entry) error {
	g := w.g

	if e.mergeConflict == plainjson.MergeConflict_MERGE_CONFLICT_ERROR {
		g.P("{")
		g.P("var live []string")
		for _, src := range e.mergeSources {
			g.P("if ", presenceTest(src), " {")
			g.P("live = append(live, ", strconv.Quote(pathString(src)), ")")
			g.P("}")
		}
		g.P("if len(live) > 1 {")
		g.P("return &", g.QualifiedGoIdent(pjPkg.Ident("MergeConflictError")), "{Key: ",
			strconv.Quote(e.key), ", Paths: live}")
		g.P("}")
		g.P("}")
	}

	sources := e.mergeSources
	if e.mergeConflict == plainjson.MergeConflict_MERGE_CONFLICT_LAST_NON_EMPTY {
		sources = make([][]*protogen.Field, len(e.mergeSources))
		for i, src := range e.mergeSources {
			sources[len(e.mergeSources)-1-i] = src
		}
	}

	g.P("switch {")
	for _, src := range sources {
		leaf := src[len(src)-1]
		g.P("case ", presenceTest(src), ":")
		w.write(e, func() {
			if leaf.Desc.IsList() || leaf.Desc.IsMap() {
				w.collectionValue(e, chain(src), leaf)
				return
			}
			w.value(mergeExpr(src), leaf, e.sc)
		})
	}
	if e.sc.emitEmpty {
		g.P("default:")
		w.write(e, func() { g.P("e.Null()") })
	}
	g.P("}")
	return nil
}

// mergeExpr renders the expression reading a merge source.
func mergeExpr(path []*protogen.Field) string {
	leaf := path[len(path)-1]
	if leaf.Desc.HasPresence() && leaf.Message == nil {
		// Presence-carrying scalars are read through the parent pointer.
		return "*" + parentChain(path) + "." + leaf.GoName
	}
	return chain(path)
}

// presenceTest renders the condition under which a source holds a value.
func presenceTest(path []*protogen.Field) string {
	leaf := path[len(path)-1]
	switch {
	case leaf.Message != nil:
		return chain(path) + " != nil"
	case leaf.Desc.IsList() || leaf.Desc.IsMap():
		return "len(" + chain(path) + ") > 0"
	case leaf.Desc.HasPresence():
		return parentChain(path) + " != nil && " + parentChain(path) + "." + leaf.GoName + " != nil"
	default:
		return zeroTest(chain(path), leaf)
	}
}

// structPick emits a value resolved inside a well-known Struct at run time.
func (w *writer) structPick(e *entry) error {
	g := w.g
	g.P("if picked, ok := ", g.QualifiedGoIdent(pjPkg.Ident("PickStruct")), "(", chain(e.path), ", ",
		strconv.Quote(e.structPath), "); ok {")
	w.write(e, func() { g.P(g.QualifiedGoIdent(pjPkg.Ident("Value")), "(e, picked)") })
	g.P("}")
	return nil
}

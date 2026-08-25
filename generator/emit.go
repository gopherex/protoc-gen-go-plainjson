package generator

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gopherex/protoc-gen-go-plainjson/plainjson"
	"google.golang.org/protobuf/compiler/protogen"
)

const (
	jxPkg      = protogen.GoImportPath("github.com/go-faster/jx")
	pjPkg      = protogen.GoImportPath("github.com/gopherex/protoc-gen-go-plainjson/plainjsonpb")
	strconvPkg = protogen.GoImportPath("strconv")
	stringsPkg = protogen.GoImportPath("strings")
)

// emitter writes the generated file for one proto file.
//
// Every message body is emitted as a package-level function rather than inline,
// so a nested object, a collection element and a recursive type all share one
// implementation. Functions are keyed by the message *and* the formatting
// scope it is reached with, since formats propagate down a subtree.
type emitter struct {
	g     *protogen.GeneratedFile
	names map[string]string
	queue []queued
	enums map[string]string
}

// queued is a message body still to be written.
type queued struct {
	name string
	msg  *protogen.Message
	sc   scope
}

// encoderName returns the function encoding msg under sc, queueing it the
// first time that combination is seen.
func (em *emitter) encoderName(msg *protogen.Message, sc scope) string {
	key := string(msg.Desc.FullName()) + "|" + sc.fingerprint()
	if name, ok := em.names[key]; ok {
		return name
	}
	name := "encodePlainJSON_" + goSymbol(msg)
	if len(em.names) > 0 {
		for _, taken := range em.names {
			if taken == name {
				name = fmt.Sprintf("%s_%d", name, len(em.names))
				break
			}
		}
	}
	em.names[key] = name
	em.queue = append(em.queue, queued{name: name, msg: msg, sc: sc})
	return name
}

// fingerprint identifies the formatting options that reach a nested message,
// so two use sites with different formats get different encoders.
func (s scope) fingerprint() string {
	return strings.Join([]string{
		strconv.Itoa(int(s.keyFrom)), strconv.Itoa(int(s.keyCase)),
		strconv.FormatBool(s.emitEmpty), strconv.Itoa(int(s.cardinality)),
		s.joinSep, s.indexSep,
		strconv.Itoa(int(s.enumFormat)), strconv.Itoa(int(s.int64Format)),
		strconv.Itoa(int(s.bytesFormat)), strconv.Itoa(int(s.timeFormat)),
		strconv.Itoa(int(s.durationFormat)),
		strconv.FormatBool(s.depthExhausted),
	}, ",")
}

// goSymbol renders a message's full name as a Go identifier fragment.
func goSymbol(msg *protogen.Message) string {
	name := strings.TrimPrefix(string(msg.Desc.FullName()), string(msg.Desc.ParentFile().Package())+".")
	return strings.ReplaceAll(name, ".", "_")
}

// ---------------------------------------------------------------------------
// Message bodies
// ---------------------------------------------------------------------------

// emitMethods writes the exported entry points for a generated message.
func (em *emitter) emitMethods(msg *protogen.Message) error {
	sc := interiorScope(msg)
	fn := em.encoderName(msg, sc)
	g := em.g

	g.P("// EncodePlainJSON writes the flattened form of ", msg.GoIdent.GoName, " into e.")
	g.P("func (m *", msg.GoIdent, ") EncodePlainJSON(e *", g.QualifiedGoIdent(jxPkg.Ident("Encoder")), ") error {")
	g.P("return ", fn, "(m, e)")
	g.P("}")
	g.P()

	g.P("// MarshalPlainJSON returns the flattened form of ", msg.GoIdent.GoName, ".")
	g.P("//")
	g.P("// The transformation is one-way: nesting, branch identity and dropped")
	g.P("// fields cannot be recovered from the result.")
	g.P("func (m *", msg.GoIdent, ") MarshalPlainJSON() ([]byte, error) {")
	g.P("var e ", g.QualifiedGoIdent(jxPkg.Ident("Encoder")))
	g.P("if err := ", fn, "(m, &e); err != nil {")
	g.P("return nil, err")
	g.P("}")
	g.P("return e.Bytes(), nil")
	g.P("}")
	g.P()

	if overridesMarshalJSON(msg) {
		g.P("// MarshalJSON makes encoding/json produce the flattened form too.")
		g.P("func (m *", msg.GoIdent, ") MarshalJSON() ([]byte, error) {")
		g.P("return m.MarshalPlainJSON()")
		g.P("}")
		g.P()
	}
	return nil
}

// flush writes every queued message body, including ones queued while writing.
func (em *emitter) flush() error {
	for len(em.queue) > 0 {
		q := em.queue[0]
		em.queue = em.queue[1:]
		if err := em.emitBody(q); err != nil {
			return err
		}
	}
	return nil
}

// emitBody writes one message encoder.
func (em *emitter) emitBody(q queued) error {
	p, err := buildPlanWith(q.msg, q.sc)
	if err != nil {
		return err
	}
	g := em.g

	g.P("// ", q.name, " writes ", q.msg.Desc.FullName(), " as a flat JSON object.")
	g.P("func ", q.name, "(m *", q.msg.GoIdent, ", e *", g.QualifiedGoIdent(jxPkg.Ident("Encoder")), ") error {")
	g.P("if m == nil {")
	g.P("e.Null()")
	g.P("return nil")
	g.P("}")

	w := &writer{em: em, g: g, plan: p}
	w.open()
	for _, e := range p.entries {
		if err := w.entry(e); err != nil {
			return err
		}
	}
	w.close()

	g.P("return nil")
	g.P("}")
	g.P()
	return nil
}

// writer emits the body of one encoder, tracking the collision machinery the
// plan asked for.
type writer struct {
	em   *emitter
	g    *protogen.GeneratedFile
	plan *plan
	// dst is the encoder variable writes go to.
	dst string
}

// open sets up the object and whatever the collision policy needs.
func (w *writer) open() {
	g := w.g
	w.dst = "e"

	if w.plan.needsTracker {
		strict := w.plan.root.collisionPolicy == plainjson.CollisionPolicy_COLLISION_POLICY_ERROR_RUNTIME
		g.P("// The collision policy needs the written keys at run time.")
		g.P("keys := ", g.QualifiedGoIdent(pjPkg.Ident("NewKeys")), "(", strict, ")")
	}
	for key := range w.plan.guarded {
		g.P("var ", guardVar(key), " bool // first non-empty writer of ", strconv.Quote(key), " wins")
	}

	if w.plan.needsBuffer {
		g.P("// COLLISION_WINS_LAST: a later write replaces an earlier one.")
		g.P("obj := ", g.QualifiedGoIdent(pjPkg.Ident("NewObject")), "()")
		return
	}
	g.P("e.ObjStart()")
}

// close finishes the object.
func (w *writer) close() {
	if w.plan.needsBuffer {
		w.g.P("obj.Encode(e)")
		return
	}
	w.g.P("e.ObjEnd()")
}

// guardVar names the "already written" flag for a key.
func guardVar(key string) string {
	var b strings.Builder
	b.WriteString("wrote")
	for _, word := range splitWords(key) {
		b.WriteString(strings.ToUpper(word[:1]))
		b.WriteString(word[1:])
	}
	return b.String()
}

// write emits one key/value pair, honouring the guard, tracker and buffer the
// plan set up. value writes the value into the encoder named by w.dst.
func (w *writer) write(e *entry, value func()) {
	g := w.g

	closers := 0
	if w.plan.guarded[e.key] {
		g.P("if !", guardVar(e.key), " {")
		closers++
	}
	if w.plan.needsTracker {
		g.P("if ok, err := keys.Claim(", strconv.Quote(e.key), ", ", strconv.Quote(e.source), "); err != nil {")
		g.P("return err")
		g.P("} else if ok {")
		closers++
	}

	if w.plan.needsBuffer {
		g.P("if err := obj.Set(", strconv.Quote(e.key), ", func(e *", g.QualifiedGoIdent(jxPkg.Ident("Encoder")), ") error {")
		value()
		g.P("return nil")
		g.P("}); err != nil {")
		g.P("return err")
		g.P("}")
	} else {
		g.P("e.FieldStart(", strconv.Quote(e.key), ")")
		value()
	}

	if w.plan.guarded[e.key] {
		g.P(guardVar(e.key), " = true")
	}
	for i := 0; i < closers; i++ {
		g.P("}")
	}
}

// entry emits one plan entry.
func (w *writer) entry(e *entry) error {
	w.g.P("// ", e.key, " <- ", e.source)

	// A value reached through a oneof branch only exists while that branch is
	// active, whatever emit_empty says.
	if closers := w.openBranchGuards(e); closers > 0 {
		defer func() {
			for i := 0; i < closers; i++ {
				w.g.P("}")
			}
		}()
	}

	switch e.kind {
	case entryConstant:
		w.write(e, func() {
			w.g.P("e.Raw([]byte(", strconv.Quote(e.constJSON), "))")
		})
		return nil
	case entryScalar:
		return w.scalar(e)
	case entryObject:
		return w.object(e)
	case entryCollection:
		return w.collection(e)
	case entryDiscriminator:
		return w.discriminator(e)
	case entryOneofValue:
		return w.oneofValue(e)
	case entryMerge:
		return w.merge(e)
	case entryStructPick:
		return w.structPick(e)
	default:
		return fmt.Errorf("unsupported entry kind %d", e.kind)
	}
}

// ---------------------------------------------------------------------------
// Access paths
// ---------------------------------------------------------------------------

// openBranchGuards wraps an entry in the checks that its oneof branches are
// active, and reports how many blocks it opened.
func (w *writer) openBranchGuards(e *entry) int {
	if !e.sc.emitEmpty || len(e.path) == 0 {
		return 0
	}
	opened := 0
	for i, f := range e.path {
		if f.Oneof == nil || f.Oneof.Desc.IsSynthetic() {
			continue
		}
		if i == len(e.path)-1 && f.Message == nil {
			continue // a scalar branch is asserted by the entry itself
		}
		w.g.P("if ", chain(e.path[:i+1]), " != nil {")
		opened++
	}
	return opened
}

// chain renders the getter chain reaching a path from the receiver.
func chain(path []*protogen.Field) string {
	var b strings.Builder
	b.WriteString("m")
	for _, f := range path {
		b.WriteString(".Get")
		b.WriteString(f.GoName)
		b.WriteString("()")
	}
	return b.String()
}

// parentChain renders the getter chain of a path's parent.
func parentChain(path []*protogen.Field) string {
	return chain(path[:len(path)-1])
}

// needsPresence reports whether a leaf can hold a value that the zero check
// would wrongly drop: proto3 optional, and every message-shaped leaf.
func needsPresence(f *protogen.Field) bool {
	if f.Desc.HasPresence() && f.Message == nil {
		return true
	}
	return f.Message != nil
}

// declareEnums writes every enum helper the file can need, before any message
// body: Go has no nested function declarations, and bodies are emitted as they
// are discovered.
func (em *emitter) declareEnums(msgs []*protogen.Message) {
	seen := map[string]bool{}
	var walk func(msg *protogen.Message)
	walk = func(msg *protogen.Message) {
		if msg == nil || seen[string(msg.Desc.FullName())] {
			return
		}
		seen[string(msg.Desc.FullName())] = true
		for _, f := range msg.Fields {
			if f.Enum != nil {
				em.declareEnum(f.Enum)
				em.declareEnumOmitter(f.Enum)
			}
			walk(f.Message)
		}
	}
	for _, msg := range msgs {
		walk(msg)
	}
}

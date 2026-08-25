package generator

import (
	"strings"
	"unicode"

	"github.com/gopherex/protoc-gen-go-plainjson/plainjson"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
)

// scope is every option in effect at one point of the walk. Options resolve
// nearest-wins along file -> message -> oneof -> field; unset enums and unset
// optional bools mean "inherit".
//
// flatten is the exception: it is a boundary decision that does not propagate.
// The walk replaces it with the field type's own mode on every descent.
type scope struct {
	flatten     plainjson.FlattenMode
	keyFrom     plainjson.KeyFrom
	keyCase     plainjson.KeyCase
	maxDepth    uint32
	emitEmpty   bool
	cardinality plainjson.Cardinality
	joinSep     string
	indexSep    string

	enumFormat     plainjson.EnumFormat
	int64Format    plainjson.Int64Format
	bytesFormat    plainjson.BytesFormat
	timeFormat     plainjson.TimeFormat
	durationFormat plainjson.DurationFormat

	collisionPolicy plainjson.CollisionPolicy
	collisionWins   plainjson.CollisionWins

	// depthExhausted marks a subtree past its max_depth bound: it keeps its
	// protojson shape however its type is configured.
	depthExhausted bool
}

// builtinDefaults is the bottom of the inheritance chain: protojson value
// formatting, deep flattening, leaf keys, and silent collision handling.
func builtinDefaults() scope {
	return scope{
		flatten:         plainjson.FlattenMode_FLATTEN_MODE_DEEP,
		keyFrom:         plainjson.KeyFrom_KEY_FROM_LEAF,
		keyCase:         plainjson.KeyCase_KEY_CASE_CAMEL,
		cardinality:     plainjson.Cardinality_CARDINALITY_KEEP,
		joinSep:         ",",
		indexSep:        "_",
		enumFormat:      plainjson.EnumFormat_ENUM_FORMAT_NAME,
		int64Format:     plainjson.Int64Format_INT64_FORMAT_STRING,
		bytesFormat:     plainjson.BytesFormat_BYTES_FORMAT_BASE64,
		timeFormat:      plainjson.TimeFormat_TIME_FORMAT_RFC3339,
		durationFormat:  plainjson.DurationFormat_DURATION_FORMAT_PROTOJSON,
		collisionPolicy: plainjson.CollisionPolicy_COLLISION_POLICY_IGNORE,
		collisionWins:   plainjson.CollisionWins_COLLISION_WINS_FIRST,
	}
}

// ---------------------------------------------------------------------------
// Option lookup
// ---------------------------------------------------------------------------

func fileOptions(f *protogen.File) *plainjson.FileOptions {
	opts, _ := proto.GetExtension(f.Desc.Options(), plainjson.E_File).(*plainjson.FileOptions)
	return opts
}

func messageOptions(m *protogen.Message) *plainjson.MessageOptions {
	opts, _ := proto.GetExtension(m.Desc.Options(), plainjson.E_Message).(*plainjson.MessageOptions)
	return opts
}

func fieldOptions(f *protogen.Field) *plainjson.FieldOptions {
	opts, _ := proto.GetExtension(f.Desc.Options(), plainjson.E_Field).(*plainjson.FieldOptions)
	return opts
}

func oneofOptions(o *protogen.Oneof) *plainjson.OneofOptions {
	opts, _ := proto.GetExtension(o.Desc.Options(), plainjson.E_Oneof).(*plainjson.OneofOptions)
	return opts
}

func enumOptions(e *protogen.Enum) *plainjson.EnumOptions {
	opts, _ := proto.GetExtension(e.Desc.Options(), plainjson.E_Enum).(*plainjson.EnumOptions)
	return opts
}

func enumValueOptions(v *protogen.EnumValue) *plainjson.EnumValueOptions {
	opts, _ := proto.GetExtension(v.Desc.Options(), plainjson.E_EnumValue).(*plainjson.EnumValueOptions)
	return opts
}

// ---------------------------------------------------------------------------
// Inheritance
// ---------------------------------------------------------------------------

func (s scope) applyFile(o *plainjson.FileOptions) scope {
	if o == nil {
		return s
	}
	s.setFlatten(o.GetFlatten())
	s.setLayout(o.GetKeyFrom(), o.GetKeyCase(), o.MaxDepth)
	if o.EmitEmpty != nil {
		s.emitEmpty = o.GetEmitEmpty()
	}
	s.setCollections(o.GetCardinality(), o.GetJoinSeparator(), o.GetIndexSeparator())
	s.setFormats(o.GetEnumFormat(), o.GetInt64Format(), o.GetBytesFormat(),
		o.GetTimeFormat(), o.GetDurationFormat())
	s.setCollisions(o.GetCollisionPolicy(), o.GetCollisionWins())
	return s
}

func (s scope) applyMessage(o *plainjson.MessageOptions) scope {
	if o == nil {
		return s
	}
	s.setFlatten(o.GetFlatten())
	s.setLayout(o.GetKeyFrom(), o.GetKeyCase(), o.MaxDepth)
	if o.EmitEmpty != nil {
		s.emitEmpty = o.GetEmitEmpty()
	}
	s.setCollections(o.GetCardinality(), o.GetJoinSeparator(), o.GetIndexSeparator())
	s.setFormats(o.GetEnumFormat(), o.GetInt64Format(), o.GetBytesFormat(),
		o.GetTimeFormat(), o.GetDurationFormat())
	s.setCollisions(o.GetCollisionPolicy(), o.GetCollisionWins())
	return s
}

func (s scope) applyOneof(o *plainjson.OneofOptions) scope {
	if o == nil {
		return s
	}
	s.setLayout(o.GetKeyFrom(), o.GetKeyCase(), nil)
	if o.EmitEmpty != nil {
		s.emitEmpty = o.GetEmitEmpty()
	}
	s.setCollections(o.GetCardinality(), o.GetJoinSeparator(), o.GetIndexSeparator())
	s.setFormats(o.GetEnumFormat(), o.GetInt64Format(), o.GetBytesFormat(),
		o.GetTimeFormat(), o.GetDurationFormat())
	return s
}

// applyField applies everything a field option carries except flatten, which
// the caller handles as a boundary decision.
func (s scope) applyField(o *plainjson.FieldOptions) scope {
	if o == nil {
		return s
	}
	s.setLayout(o.GetKeyFrom(), o.GetKeyCase(), o.MaxDepth)
	if o.EmitEmpty != nil {
		s.emitEmpty = o.GetEmitEmpty()
	}
	s.setCollections(o.GetCardinality(), o.GetJoinSeparator(), o.GetIndexSeparator())
	s.setFormats(o.GetEnumFormat(), o.GetInt64Format(), o.GetBytesFormat(),
		o.GetTimeFormat(), o.GetDurationFormat())
	return s
}

func (s *scope) setFlatten(v plainjson.FlattenMode) {
	if v != plainjson.FlattenMode_FLATTEN_MODE_UNSPECIFIED {
		s.flatten = v
	}
}

// setLayout applies key derivation and the depth bound. depth is a pointer so
// an explicit 0 can lift an inherited bound.
func (s *scope) setLayout(from plainjson.KeyFrom, kc plainjson.KeyCase, depth *uint32) {
	if from != plainjson.KeyFrom_KEY_FROM_UNSPECIFIED {
		s.keyFrom = from
	}
	if kc != plainjson.KeyCase_KEY_CASE_UNSPECIFIED {
		s.keyCase = kc
	}
	if depth != nil {
		s.maxDepth = *depth
	}
}

func (s *scope) setCollections(c plainjson.Cardinality, join, index string) {
	if c != plainjson.Cardinality_CARDINALITY_UNSPECIFIED {
		s.cardinality = c
	}
	if join != "" {
		s.joinSep = join
	}
	if index != "" {
		s.indexSep = index
	}
}

func (s *scope) setFormats(
	enum plainjson.EnumFormat,
	i64 plainjson.Int64Format,
	by plainjson.BytesFormat,
	ts plainjson.TimeFormat,
	dur plainjson.DurationFormat,
) {
	if enum != plainjson.EnumFormat_ENUM_FORMAT_UNSPECIFIED {
		s.enumFormat = enum
	}
	if i64 != plainjson.Int64Format_INT64_FORMAT_UNSPECIFIED {
		s.int64Format = i64
	}
	if by != plainjson.BytesFormat_BYTES_FORMAT_UNSPECIFIED {
		s.bytesFormat = by
	}
	if ts != plainjson.TimeFormat_TIME_FORMAT_UNSPECIFIED {
		s.timeFormat = ts
	}
	if dur != plainjson.DurationFormat_DURATION_FORMAT_UNSPECIFIED {
		s.durationFormat = dur
	}
}

func (s *scope) setCollisions(p plainjson.CollisionPolicy, w plainjson.CollisionWins) {
	if p != plainjson.CollisionPolicy_COLLISION_POLICY_UNSPECIFIED {
		s.collisionPolicy = p
	}
	if w != plainjson.CollisionWins_COLLISION_WINS_UNSPECIFIED {
		s.collisionWins = w
	}
}

// interiorScope is the scope in effect *inside* a message type: the type's own
// file and message options, ignoring wherever it is used from.
func interiorScope(m *protogen.Message) scope {
	return builtinDefaults().
		applyFile(fileOptionsOf(m)).
		applyMessage(messageOptions(m))
}

// fileOptionsOf reaches the plainjson file options of a message's own file.
func fileOptionsOf(m *protogen.Message) *plainjson.FileOptions {
	opts, _ := proto.GetExtension(
		m.Desc.ParentFile().Options(), plainjson.E_File).(*plainjson.FileOptions)
	return opts
}

// generates reports whether the plugin emits marshalers for a message: an
// explicit generate wins, otherwise the file's generate_all decides.
func generates(m *protogen.Message) bool {
	if o := messageOptions(m); o != nil && o.Generate != nil {
		return o.GetGenerate()
	}
	return fileOptionsOf(m).GetGenerateAll()
}

// overridesMarshalJSON reports whether MarshalJSON is emitted too.
func overridesMarshalJSON(m *protogen.Message) bool {
	if o := messageOptions(m); o != nil && o.OverrideMarshalJson != nil {
		return o.GetOverrideMarshalJson()
	}
	return fileOptionsOf(m).GetOverrideMarshalJson()
}

// ---------------------------------------------------------------------------
// Key naming
// ---------------------------------------------------------------------------

// keyParts accumulates what a key is built from as the walk descends.
type keyParts struct {
	// prefixes and suffixes accumulate outermost-first.
	prefixes []string
	suffixes []string
	// segments are the field names along the path, used by KEY_FROM_PATH.
	segments []string
}

func (k keyParts) push(prefix, suffix, segment string) keyParts {
	out := keyParts{
		prefixes: k.prefixes,
		suffixes: k.suffixes,
		segments: k.segments,
	}
	if prefix != "" {
		out.prefixes = appendCopy(k.prefixes, prefix)
	}
	if suffix != "" {
		out.suffixes = appendCopy(k.suffixes, suffix)
	}
	if segment != "" {
		out.segments = appendCopy(k.segments, segment)
	}
	return out
}

func appendCopy(in []string, v string) []string {
	out := make([]string, len(in), len(in)+1)
	copy(out, in)
	return append(out, v)
}

// key renders the JSON key for a leaf. Prefixes and suffixes apply before
// casing, so "ship_" + "name" becomes shipName under KEY_CASE_CAMEL.
func (k keyParts) key(s scope) string {
	var b strings.Builder
	for _, p := range k.prefixes {
		b.WriteString(p)
	}
	switch s.keyFrom {
	case plainjson.KeyFrom_KEY_FROM_PATH:
		b.WriteString(strings.Join(k.segments, "_"))
	default:
		if n := len(k.segments); n > 0 {
			b.WriteString(k.segments[n-1])
		}
	}
	for i := len(k.suffixes) - 1; i >= 0; i-- {
		b.WriteString(k.suffixes[i])
	}
	return applyCase(b.String(), s.keyCase)
}

// applyCase renders a raw key in the requested casing.
func applyCase(raw string, kc plainjson.KeyCase) string {
	switch kc {
	case plainjson.KeyCase_KEY_CASE_ORIGINAL:
		return raw
	case plainjson.KeyCase_KEY_CASE_SNAKE:
		return strings.Join(splitWords(raw), "_")
	default:
		return camel(splitWords(raw))
	}
}

// splitWords breaks a key on underscores and camel-case boundaries, lowering
// every word, so casing is idempotent whatever the input spelling was.
func splitWords(raw string) []string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	runes := []rune(raw)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == ' ':
			flush()
		case unicode.IsUpper(r):
			// A new word starts at a lower->upper boundary, and at the last
			// upper of an acronym run followed by a lower ("JSONValue").
			prevLower := i > 0 && unicode.IsLower(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || (nextLower && cur.Len() > 0) {
				flush()
			}
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return words
}

// camel joins words in lowerCamelCase.
func camel(words []string) string {
	var b strings.Builder
	for i, w := range words {
		if w == "" {
			continue
		}
		if i == 0 {
			b.WriteString(w)
			continue
		}
		b.WriteString(strings.ToUpper(w[:1]))
		b.WriteString(w[1:])
	}
	return b.String()
}

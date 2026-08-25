package generator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gopherex/protoc-gen-go-plainjson/plainjson"
	"google.golang.org/protobuf/compiler/protogen"
)

// ---------------------------------------------------------------------------
// Paths
// ---------------------------------------------------------------------------

// resolvePath turns a dot-separated option path into a chain of fields.
func resolvePath(msg *protogen.Message, path string, what string) ([]*protogen.Field, error) {
	var out []*protogen.Field
	cur := msg
	for _, part := range strings.Split(path, ".") {
		if cur == nil {
			return nil, fmt.Errorf("%s %q on %s: %q is not a message",
				what, path, msg.Desc.FullName(), pathString(out))
		}
		field := fieldByName(cur, part)
		if field == nil {
			return nil, fmt.Errorf("%s %q on %s: field %q not found in %s",
				what, path, msg.Desc.FullName(), part, cur.Desc.FullName())
		}
		out = append(out, field)
		cur = field.Message
		if isWellKnown(field.Message) {
			cur = nil
		}
	}
	return out, nil
}

// fieldByName looks a field up by its proto name.
func fieldByName(msg *protogen.Message, name string) *protogen.Field {
	for _, f := range msg.Fields {
		if string(f.Desc.Name()) == name {
			return f
		}
	}
	return nil
}

// requireLeaf rejects a path that stops on a message, since only values can be
// picked, lifted or merged.
func requireLeaf(path []*protogen.Field, spec, what string, owner *protogen.Message) error {
	last := path[len(path)-1]
	if !isLeafKind(last) && !last.Desc.IsList() && !last.Desc.IsMap() {
		return fmt.Errorf("%s %q on %s: resolves to a message; pick a scalar or use lift",
			what, spec, owner.Desc.FullName())
	}
	return nil
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// addConstants appends the message's fixed key/value pairs, which are written
// before anything derived from the message itself.
func (p *plan) addConstants(opts *plainjson.MessageOptions) error {
	for _, c := range opts.GetConstants() {
		if !json.Valid([]byte(c.GetValueJson())) {
			return fmt.Errorf("%s: constant %q: value_json is not valid JSON: %s",
				p.msg.Desc.FullName(), c.GetKey(), c.GetValueJson())
		}
		key := c.GetKey()
		if !c.GetRawKey() {
			key = applyCase(key, p.root.keyCase)
		}
		p.add(&entry{
			kind:      entryConstant,
			key:       key,
			sc:        p.root,
			constJSON: c.GetValueJson(),
			source:    fmt.Sprintf("constant %q", c.GetKey()),
		})
	}
	return nil
}

// ---------------------------------------------------------------------------
// Merge rules
// ---------------------------------------------------------------------------

// mergeRule is a resolved (plainjson.MergeRule).
type mergeRule struct {
	key        string
	sources    [][]*protogen.Field
	onConflict plainjson.MergeConflict
	sc         scope
	emitEmpty  bool
}

// resolveMerges validates every merge rule and resolves its source paths.
func resolveMerges(msg *protogen.Message, opts *plainjson.MessageOptions) ([]*mergeRule, error) {
	rules := opts.GetMerge()
	if len(rules) == 0 {
		return nil, nil
	}

	root := interiorScope(msg)
	seenKeys := map[string]bool{}
	seenPaths := map[string]string{}

	out := make([]*mergeRule, 0, len(rules))
	for _, r := range rules {
		if seenKeys[r.GetKey()] {
			return nil, fmt.Errorf("%s: merge key %q declared twice",
				msg.Desc.FullName(), r.GetKey())
		}
		seenKeys[r.GetKey()] = true

		sc := root
		sc.setCollections(r.GetCardinality(), r.GetJoinSeparator(), "")
		sc.setFormats(r.GetEnumFormat(), r.GetInt64Format(), r.GetBytesFormat(),
			r.GetTimeFormat(), r.GetDurationFormat())
		emitEmpty := sc.emitEmpty
		if r.EmitEmpty != nil {
			emitEmpty = r.GetEmitEmpty()
		}

		sc.emitEmpty = emitEmpty
		rule := &mergeRule{
			key:        r.GetKey(),
			onConflict: r.GetOnConflict(),
			sc:         sc,
			emitEmpty:  emitEmpty,
		}
		if !r.GetRawKey() {
			rule.key = applyCase(rule.key, root.keyCase)
		}

		for _, spec := range r.GetFrom() {
			if prev, dup := seenPaths[spec]; dup {
				return nil, fmt.Errorf("%s: merge path %q used by rules %q and %q",
					msg.Desc.FullName(), spec, prev, r.GetKey())
			}
			seenPaths[spec] = r.GetKey()

			path, err := resolvePath(msg, spec, "merge")
			if err != nil {
				return nil, fmt.Errorf("%s: %w", msg.Desc.FullName(), err)
			}
			rule.sources = append(rule.sources, path)
		}
		if err := checkMergeSourceTypes(msg, rule); err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, nil
}

// checkMergeSourceTypes rejects sources that cannot land on one JSON key.
func checkMergeSourceTypes(msg *protogen.Message, rule *mergeRule) error {
	kinds := map[string]bool{}
	var names []string
	for _, path := range rule.sources {
		k := jsonShape(path[len(path)-1], rule.sc)
		if !kinds[k] {
			kinds[k] = true
			names = append(names, k)
		}
	}
	if len(kinds) > 1 {
		return fmt.Errorf("%s: merge %q: sources have different JSON types (%s)",
			msg.Desc.FullName(), rule.key, strings.Join(names, ", "))
	}
	return nil
}

// consumedByMerge reports whether a path is fed to a merge rule, in which case
// it leaves the normal plan.
func consumedByMerge(rules []*mergeRule, path []*protogen.Field) bool {
	for _, r := range rules {
		for _, src := range r.sources {
			if samePath(src, path) {
				return true
			}
		}
	}
	return false
}

// samePath compares two field chains.
func samePath(a, b []*protogen.Field) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// appendMerges writes merged keys after the flatten plan, in declaration order.
func (p *plan) appendMerges(rules []*mergeRule) {
	for _, r := range rules {
		p.add(&entry{
			kind:          entryMerge,
			key:           r.key,
			sc:            r.sc,
			mergeSources:  r.sources,
			mergeConflict: r.onConflict,
			source:        fmt.Sprintf("merge %q", r.key),
		})
	}
}

// ---------------------------------------------------------------------------
// Exclusive groups
// ---------------------------------------------------------------------------

// exclusiveGroups maps a declared field path to its group id. Entries sharing
// an id are never reported as colliding.
type exclusiveGroups struct {
	paths map[string]int
}

// resolveExclusiveGroups validates the declared groups.
func resolveExclusiveGroups(msg *protogen.Message, opts *plainjson.MessageOptions) (exclusiveGroups, error) {
	groups := exclusiveGroups{paths: map[string]int{}}
	for i, g := range opts.GetExclusiveGroups() {
		for _, spec := range g.GetFields() {
			path, err := resolvePath(msg, spec, "exclusive_groups")
			if err != nil {
				return groups, fmt.Errorf("%s: %w", msg.Desc.FullName(), err)
			}
			groups.paths[pathString(path)] = i + 1
		}
	}
	return groups, nil
}

// idFor returns the group id covering a path, inheriting the enclosing one.
func (g exclusiveGroups) idFor(path []*protogen.Field, inherited int) int {
	if id, ok := g.paths[pathString(path)]; ok {
		return id
	}
	return inherited
}

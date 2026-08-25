package generator

import (
	"github.com/gopherex/protoc-gen-go-plainjson/plainjson"
	"google.golang.org/protobuf/compiler/protogen"
)

// walkOneof appends the entries produced by one oneof. Branches are mutually
// exclusive, so every entry they produce shares an exclusivity id and may
// share JSON keys freely.
func (p *plan) walkOneof(oneof *protogen.Oneof, ctx walkCtx, merges []*mergeRule, groups exclusiveGroups) error {
	opts := oneofOptions(oneof)
	sc := ctx.sc.applyOneof(opts)

	mode := opts.GetMode()
	if mode == plainjson.OneofMode_ONEOF_MODE_UNSPECIFIED {
		if ctx.sc.flatten == plainjson.FlattenMode_FLATTEN_MODE_NONE {
			mode = plainjson.OneofMode_ONEOF_MODE_BRANCH_KEY
		} else {
			mode = plainjson.OneofMode_ONEOF_MODE_INLINE
		}
	}
	if mode == plainjson.OneofMode_ONEOF_MODE_OMIT {
		return nil
	}

	branchCtx := ctx
	branchCtx.sc = sc
	branchCtx.exclusive = p.nextExclusive()

	if mode == plainjson.OneofMode_ONEOF_MODE_TAGGED ||
		mode == plainjson.OneofMode_ONEOF_MODE_DISCRIMINATOR_ONLY {
		p.add(p.discriminatorEntry(oneof, opts, sc, ctx))
	}

	switch mode {
	case plainjson.OneofMode_ONEOF_MODE_DISCRIMINATOR_ONLY:
		return nil

	case plainjson.OneofMode_ONEOF_MODE_SINGLE_KEY:
		key := opts.GetValueKey()
		if key == "" {
			key = string(oneof.Desc.Name())
		}
		p.add(&entry{
			kind:        entryOneofValue,
			key:         applyCase(key, sc.keyCase),
			sc:          sc,
			path:        ctx.path,
			oneof:       oneof,
			omitIfUnset: omitIfUnset(opts),
			exclusive:   branchCtx.exclusive,
			source:      string(oneof.Desc.FullName()),
		})
		return nil

	case plainjson.OneofMode_ONEOF_MODE_BRANCH_KEY:
		// Each branch keeps its own key: cross the boundary without inlining.
		branchCtx.sc.flatten = plainjson.FlattenMode_FLATTEN_MODE_NONE
	}

	for _, field := range oneof.Fields {
		if err := p.walkField(field, branchCtx, merges, groups); err != nil {
			return err
		}
	}
	return nil
}

// discriminatorEntry builds the entry writing the active branch's tag.
func (p *plan) discriminatorEntry(
	oneof *protogen.Oneof,
	opts *plainjson.OneofOptions,
	sc scope,
	ctx walkCtx,
) *entry {
	key := opts.GetDiscriminator()
	if key == "" {
		key = "type"
	}
	tags := make(map[*protogen.Field]string, len(oneof.Fields))
	for _, f := range oneof.Fields {
		tag := string(f.Desc.Name())
		if o := fieldOptions(f); o.GetTag() != "" {
			tag = o.GetTag()
		}
		tags[f] = tag
	}
	return &entry{
		kind:        entryDiscriminator,
		key:         applyCase(key, sc.keyCase),
		sc:          sc,
		path:        ctx.path,
		oneof:       oneof,
		branchTags:  tags,
		omitIfUnset: omitIfUnset(opts),
		exclusive:   ctx.exclusive,
		source:      string(oneof.Desc.FullName()) + " discriminator",
	}
}

// omitIfUnset resolves the oneof option whose default is true.
func omitIfUnset(opts *plainjson.OneofOptions) bool {
	if opts != nil && opts.OmitIfUnset != nil {
		return opts.GetOmitIfUnset()
	}
	return true
}

// nextExclusive hands out an exclusivity id for one oneof. Ids from declared
// exclusive groups start at 1, so oneofs start well above them.
func (p *plan) nextExclusive() int {
	p.oneofSeq++
	return 1000 + p.oneofSeq
}

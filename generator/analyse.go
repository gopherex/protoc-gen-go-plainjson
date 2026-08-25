package generator

import (
	"fmt"
	"sort"

	"github.com/gopherex/protoc-gen-go-plainjson/plainjson"
)

// analyse decides how the plan handles keys claimed by more than one entry,
// and records what the generated code needs to enforce it.
//
// Two entries collide only if both can write in the same encode: entries in
// different branches of one oneof, or in one declared exclusive group, are
// exempt — collapsing those onto a shared key is the point of flattening.
func (p *plan) analyse() error {
	byKey := map[string][]*entry{}
	var order []string
	for _, e := range p.entries {
		if e.dynamic {
			// The keys are only known at run time; nothing static to compare.
			continue
		}
		if _, seen := byKey[e.key]; !seen {
			order = append(order, e.key)
		}
		byKey[e.key] = append(byKey[e.key], e)
	}

	for _, key := range order {
		entries := byKey[key]
		first, second := firstConflict(entries)
		if first == nil {
			continue
		}
		switch p.root.collisionPolicy {
		case plainjson.CollisionPolicy_COLLISION_POLICY_ERROR_GENERATE:
			return fmt.Errorf(
				"%s: JSON key %q produced by two live sources:\n"+
					"  - %s\n  - %s\n"+
					"  fix: set (plainjson.field).prefix / .name, use KEY_FROM_PATH, add a merge rule,\n"+
					"       declare an exclusive group, or set collision_policy",
				p.msg.Desc.FullName(), key, first.source, second.source)
		case plainjson.CollisionPolicy_COLLISION_POLICY_ERROR_RUNTIME:
			p.needsTracker = true
		default:
			if p.root.collisionWins == plainjson.CollisionWins_COLLISION_WINS_LAST {
				p.needsBuffer = true
			} else {
				p.guarded[key] = true
			}
		}
	}

	for _, e := range p.entries {
		if !e.dynamic {
			continue
		}
		// A dynamic source can always clash with a static key, so it needs the
		// runtime key set either to report the clash or to drop the loser.
		p.needsTracker = true
	}
	if p.root.collisionPolicy == plainjson.CollisionPolicy_COLLISION_POLICY_ERROR_RUNTIME {
		p.needsTracker = true
	}

	sort.Strings(order)
	return nil
}

// firstConflict returns the first pair of entries on one key that are not
// mutually exclusive.
func firstConflict(entries []*entry) (*entry, *entry) {
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if exclusiveOf(entries[i], entries[j]) {
				continue
			}
			return entries[i], entries[j]
		}
	}
	return nil, nil
}

// exclusiveOf reports whether two entries can never both write.
func exclusiveOf(a, b *entry) bool {
	return a.exclusive != 0 && a.exclusive == b.exclusive
}

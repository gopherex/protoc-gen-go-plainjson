// Package conformance runs the declarative spec suite against the generated
// marshalers. It imports every spec package so their types register, then
// drives them purely by proto name.
package conformance

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopherex/protoc-gen-go-plainjson/internal/spectest"

	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/api"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/cardinality"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/collisions"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/flatten"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/inherit/collide"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/inherit/formats"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/inherit/layout"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/keys"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/merging"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/oneofs"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/presence"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/scalars"
	_ "github.com/gopherex/protoc-gen-go-plainjson/example/spec/selection"
)

// casesDir is the declarative suite, relative to this package.
var casesDir = filepath.Join("..", "..", "testdata", "cases")

func load(t *testing.T) spectest.Suite {
	t.Helper()
	suite, err := spectest.Load(casesDir)
	if err != nil {
		t.Fatalf("loading cases: %v", err)
	}
	return suite
}

// TestSpec runs every case in the suite.
func TestSpec(t *testing.T) {
	load(t).Run(t)
}

// TestEveryCaseCitesSpec keeps the suite honest as documentation: a case that
// pins down behaviour must say which part of the spec it pins down.
func TestEveryCaseCitesSpec(t *testing.T) {
	for _, c := range load(t) {
		if strings.TrimSpace(c.Spec) == "" {
			t.Errorf("case %q has no spec reference", c.Name)
		}
	}
}

// TestEveryGeneratedMessageIsCovered fails when a message the plugin generates
// for has no case: new spec protos cannot be added without asserting on them.
func TestEveryGeneratedMessageIsCovered(t *testing.T) {
	cov, err := spectest.Collect("spec.")
	if err != nil {
		t.Fatalf("collecting coverage: %v", err)
	}
	covered := load(t).Messages()

	var missing []string
	for name := range cov.Generated {
		if !covered[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d generated messages have no case:\n  %s",
			len(missing), strings.Join(sorted(missing), "\n  "))
	}
}

// TestEveryOptionFieldExercised fails when an option in plainjson.proto is
// never set anywhere in the spec protos — the suite is meant to cover the
// whole option surface, not a subset of it.
func TestEveryOptionFieldExercised(t *testing.T) {
	cov, err := spectest.Collect("spec.")
	if err != nil {
		t.Fatalf("collecting coverage: %v", err)
	}

	var missing []string
	for _, field := range spectest.AllOptionFields() {
		if !cov.OptionFields[field] {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d option fields are never exercised by the spec protos:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestEveryOptionEnumValueExercised does the same for every mode of every
// option enum. UNSPECIFIED zeros mean "inherit" and are skipped.
func TestEveryOptionEnumValueExercised(t *testing.T) {
	cov, err := spectest.Collect("spec.")
	if err != nil {
		t.Fatalf("collecting coverage: %v", err)
	}

	var missing []string
	for _, value := range spectest.AllEnumValues() {
		if !cov.EnumValues[value] {
			missing = append(missing, value)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d option enum values are never exercised by the spec protos:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

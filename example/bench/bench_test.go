package bench_test

import (
	"encoding/json"
	"testing"

	pb "github.com/gopherex/protoc-gen-go-plainjson/example/bench"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestBaselinesAgree keeps the comparison honest: the generated marshaler and
// the two hand-written flatteners must produce the same record, or the numbers
// below would be measuring different work.
//
// Content is compared rather than bytes: a hand-written flattener collects
// pairs in a map and loses key order, which is itself part of what the
// generated code buys.
func TestBaselinesAgree(t *testing.T) {
	msg := newEventPlain()

	generated, err := msg.MarshalPlainJSON()
	if err != nil {
		t.Fatalf("MarshalPlainJSON: %v", err)
	}
	viaJSON, err := flattenViaJSON(msg)
	if err != nil {
		t.Fatalf("flattenViaJSON: %v", err)
	}
	viaReflect, err := flattenViaReflect(msg)
	if err != nil {
		t.Fatalf("flattenViaReflect: %v", err)
	}

	want := decode(t, generated)
	for name, got := range map[string][]byte{
		"flattenViaJSON":    viaJSON,
		"flattenViaReflect": viaReflect,
	} {
		if diff := compare(want, decode(t, got)); diff != "" {
			t.Errorf("%s disagrees with the generated marshaler: %s\ngenerated: %s\nbaseline:  %s",
				name, diff, generated, got)
		}
	}
}

// decode parses a flat object into a map for comparison.
func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parsing %s: %v", raw, err)
	}
	return out
}

// compare returns the first difference between two flat records.
func compare(want, got map[string]any) string {
	for k, v := range want {
		other, ok := got[k]
		if !ok {
			return "missing key " + k
		}
		if toJSON(v) != toJSON(other) {
			return "key " + k + ": " + toJSON(other) + " != " + toJSON(v)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			return "extra key " + k
		}
	}
	return ""
}

func toJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

// BenchmarkFlatten compares the generated marshaler against the two ways of
// flattening the same message by hand.
func BenchmarkFlatten(b *testing.B) {
	msg := newEventPlain()

	b.Run("generated", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := msg.MarshalPlainJSON(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("hand-reflect", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := flattenViaReflect(msg); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("hand-json", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := flattenViaJSON(msg); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkEvent measures the full option set — a oneof discriminator, a merge
// rule, inlined map keys, a joined repeated field — against protojson on the
// same message. protojson produces the nested document rather than the flat
// one, so this is a floor for "serialise this message at all", not a
// like-for-like comparison.
func BenchmarkEvent(b *testing.B) {
	msg := newEvent()

	b.Run("plainjson", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := msg.MarshalPlainJSON(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("protojson", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := protojson.Marshal(msg); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkScalars is the flat case: no flattening to do, so it isolates the
// cost of writing values.
func BenchmarkScalars(b *testing.B) {
	msg := newScalars()

	b.Run("plainjson", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := msg.MarshalPlainJSON(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("protojson", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := protojson.Marshal(msg); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCollisionPolicy prices what each policy costs on the same colliding
// message: a local flag, a key tracker, or a buffered object.
func BenchmarkCollisionPolicy(b *testing.B) {
	a, b2 := newCollideParts()
	first := &pb.CollideFirst{A: a, B: b2}
	last := &pb.CollideLast{A: a, B: b2}

	// ERROR_RUNTIME rejects a real collision, so it is measured on data that
	// does not collide: what is being priced is the key tracker, not the error.
	quiet := &pb.PathB{Sha256: b2.GetSha256(), Size: b2.GetSize()}
	runtime := &pb.CollideRuntime{A: a, B: quiet}

	b.Run("ignore-first", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := first.MarshalPlainJSON(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("error-runtime", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := runtime.MarshalPlainJSON(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("wins-last", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := last.MarshalPlainJSON(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

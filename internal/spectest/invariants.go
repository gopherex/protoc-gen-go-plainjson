package spectest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/gopherex/protoc-gen-go-plainjson/plainjsonpb"
)

// checkInvariants applies the properties that must hold for every successful
// marshal, whatever options produced it. They are asserted on every case, so a
// new case exercises them for free.
func (c Case) checkInvariants(t *testing.T, m plainjsonpb.Marshaler, got []byte) {
	t.Helper()

	if !json.Valid(got) {
		t.Fatalf("output is not valid JSON: %s", got)
	}

	// The output is always a single JSON object, except for a nil receiver.
	if c.NilReceiver {
		if string(got) != "null" {
			t.Errorf("nil receiver: got %s, want null", got)
		}
		return
	}
	if len(got) == 0 || got[0] != '{' {
		t.Errorf("output is not a JSON object: %s", got)
	}

	// Marshaling twice yields identical bytes: no map iteration order leaks.
	again, err := m.MarshalPlainJSON()
	if err != nil {
		t.Fatalf("second MarshalPlainJSON: %v", err)
	}
	if !bytes.Equal(got, again) {
		t.Errorf("not deterministic:\nrun 1: %s\nrun 2: %s", got, again)
	}

	// The streaming and buffered entry points agree.
	streamed, err := encodeViaStream(m)
	if err != nil {
		t.Fatalf("EncodePlainJSON: %v", err)
	}
	if !bytes.Equal(got, streamed) {
		t.Errorf("EncodePlainJSON disagrees with MarshalPlainJSON:\nstream: %s\nbuffer: %s",
			streamed, got)
	}

	// No key is written twice: whatever the collision policy decided, the
	// result is a well-formed object a JSON reader cannot misread.
	keys, err := topLevelKeys(got)
	if err != nil {
		t.Fatalf("scanning keys: %v", err)
	}
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if seen[k] {
			t.Errorf("duplicate top-level key %q in %s", k, got)
		}
		seen[k] = true
	}

	if c.Flat {
		assertFlat(t, got)
	}

	if c.JSONMarshaler {
		jm, ok := m.(json.Marshaler)
		if !ok {
			t.Fatalf("%s: expected MarshalJSON to be generated (override_marshal_json)", c.Message)
		}
		viaJSON, err := jm.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON: %v", err)
		}
		if !bytes.Equal(got, viaJSON) {
			t.Errorf("MarshalJSON disagrees with MarshalPlainJSON:\n json: %s\nplain: %s",
				viaJSON, got)
		}
	}
}

// topLevelKeys returns the keys of a JSON object in written order, including
// any duplicates.
func topLevelKeys(b []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("not an object: %s", b)
	}

	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("object key is not a string: %v", tok)
		}
		keys = append(keys, key)
		if err := skipValue(dec); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// skipValue consumes one complete JSON value from dec.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	_ = delim
	return nil
}

// assertFlat checks that no value in the object is itself an object — the
// property a fully flattened message is supposed to have. Arrays are allowed:
// a collection kept by CARDINALITY_KEEP is still flat in the sense that no key
// is nested behind another key.
func assertFlat(t *testing.T, b []byte) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil {
		t.Fatalf("flat check: %v", err)
	}
	for k, v := range obj {
		trimmed := bytes.TrimLeft(v, " \t\r\n")
		if len(trimmed) > 0 && trimmed[0] == '{' {
			t.Errorf("key %q holds a nested object, want a flat result: %s", k, v)
		}
	}
}

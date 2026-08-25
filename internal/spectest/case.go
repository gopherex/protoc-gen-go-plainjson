// Package spectest runs the declarative conformance suite for
// protoc-gen-go-plainjson.
//
// Cases live in testdata/cases/*.json and are written against the option
// reference in README.md. A case names a message by its full proto name, gives
// an input in ordinary protojson form, and states the exact flattened output.
// Nothing in a case refers to Go types, so the suite is a description of the
// spec rather than of the implementation.
package spectest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Case is one conformance assertion.
type Case struct {
	// Name identifies the case, "section/detail" by convention.
	Name string `json:"name"`
	// Spec points at the README section the case pins down.
	Spec string `json:"spec"`
	// Message is the full proto name, e.g. "spec.flatten.DeepDefault".
	Message string `json:"message"`
	// Input is the message in ordinary protojson form. Absent means the zero
	// message.
	Input json.RawMessage `json:"input,omitempty"`
	// Want is the exact expected output, compared byte for byte: key order is
	// part of the contract.
	Want string `json:"want,omitempty"`
	// WantError names the expected failure instead of Want:
	// "key_collision" or "merge_conflict".
	WantError string `json:"want_error,omitempty"`
	// ErrorKey is the JSON key the expected error must name.
	ErrorKey string `json:"error_key,omitempty"`
	// NilReceiver marshals a typed nil pointer instead of Input.
	NilReceiver bool `json:"nil_receiver,omitempty"`
	// Flat asserts no value in the output is a JSON object.
	Flat bool `json:"flat,omitempty"`
	// JSONMarshaler asserts the message also implements json.Marshaler and
	// that it agrees with MarshalPlainJSON.
	JSONMarshaler bool `json:"json_marshaler,omitempty"`
	// NoMarshaler asserts the plugin did *not* generate for this message.
	NoMarshaler bool `json:"no_marshaler,omitempty"`
	// Skip, when set, explains why the case is not run.
	Skip string `json:"skip,omitempty"`

	// file records where the case was loaded from, for diagnostics.
	file string
}

// Suite is every case, in load order.
type Suite []Case

// Load reads every *.json file in dir as a list of cases.
func Load(dir string) (Suite, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("spectest: no case files in %s", dir)
	}

	var suite Suite
	seen := map[string]string{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var cases []Case
		dec := json.NewDecoder(newCommentStripper(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cases); err != nil {
			return nil, fmt.Errorf("spectest: %s: %w", path, err)
		}
		for i := range cases {
			c := &cases[i]
			c.file = path
			if c.Name == "" {
				return nil, fmt.Errorf("spectest: %s: case %d has no name", path, i)
			}
			if c.Message == "" {
				return nil, fmt.Errorf("spectest: %s: case %q has no message", path, c.Name)
			}
			if c.Want == "" && c.WantError == "" && !c.NoMarshaler {
				return nil, fmt.Errorf("spectest: %s: case %q states neither want nor want_error", path, c.Name)
			}
			if prev, dup := seen[c.Name]; dup {
				return nil, fmt.Errorf("spectest: duplicate case name %q in %s and %s", c.Name, prev, path)
			}
			seen[c.Name] = path
			suite = append(suite, *c)
		}
	}
	return suite, nil
}

// Messages returns the set of proto message names the suite covers.
func (s Suite) Messages() map[string]bool {
	out := make(map[string]bool, len(s))
	for _, c := range s {
		out[c.Message] = true
	}
	return out
}

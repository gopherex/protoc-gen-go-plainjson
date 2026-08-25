package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopherex/protoc-gen-go-plainjson/generator"
	"github.com/gopherex/protoc-gen-go-plainjson/internal/spectest"
)

// invalidCase is one generation-time diagnostic the plugin must produce.
// The protos live in testdata/invalid and are compiled into a descriptor set
// fixture by `make gen-invalid`, so these tests need no protoc at run time.
type invalidCase struct {
	Name      string `json:"name"`
	File      string `json:"file"`
	WantError string `json:"want_error"`
}

var (
	invalidDir     = filepath.Join("..", "..", "testdata", "invalid")
	invalidDescSet = filepath.Join(invalidDir, "descriptors.binpb")
	specDescSet    = filepath.Join("..", "..", "testdata", "spec-descriptors.binpb")
)

// TestGenerationErrors asserts every check in README#generation-time-validation
// rejects its proto, with a message that names the problem.
func TestGenerationErrors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(invalidDir, "cases.json"))
	if err != nil {
		t.Fatalf("reading invalid cases: %v", err)
	}
	var cases []invalidCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parsing invalid cases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("no invalid cases declared")
	}

	descs, err := spectest.LoadDescriptors(invalidDescSet)
	if err != nil {
		t.Fatalf("loading descriptors: %v", err)
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			path := filepath.ToSlash(filepath.Join("testdata", "invalid", c.File))
			plugin, err := descs.PluginFor(path)
			if err != nil {
				t.Fatalf("building plugin for %s: %v", path, err)
			}

			var genErr error
			for _, f := range plugin.Files {
				if !f.Generate {
					continue
				}
				if err := generator.GenerateFile(plugin, f); err != nil {
					genErr = err
					break
				}
			}
			if genErr == nil {
				t.Fatalf("expected generation to fail with %q, but it succeeded", c.WantError)
			}
			if !strings.Contains(genErr.Error(), c.WantError) {
				t.Errorf("error does not mention the problem\n got: %v\nwant substring: %q",
					genErr, c.WantError)
			}
		})
	}
}

// TestSpecProtosGenerateCleanly is the other half: everything the suite
// exercises must generate without a diagnostic.
func TestSpecProtosGenerateCleanly(t *testing.T) {
	descs, err := spectest.LoadDescriptors(specDescSet)
	if err != nil {
		t.Fatalf("loading descriptors: %v", err)
	}

	var specFiles []string
	for _, path := range descs.Files() {
		if strings.HasPrefix(path, "example/spec/") {
			specFiles = append(specFiles, path)
		}
	}
	if len(specFiles) == 0 {
		t.Fatal("no spec protos in the descriptor fixture")
	}

	plugin, err := descs.PluginFor(specFiles...)
	if err != nil {
		t.Fatalf("building plugin: %v", err)
	}
	for _, f := range plugin.Files {
		if !f.Generate {
			continue
		}
		if err := generator.GenerateFile(plugin, f); err != nil {
			t.Errorf("%s: %v", f.Desc.Path(), err)
		}
	}

	// Generation must actually emit something for the spec protos, and the Go
	// it emits has to parse: protogen reports a syntax error instead of files.
	resp := plugin.Response()
	if resp.Error != nil {
		t.Fatalf("plugin reported an error: %s", resp.GetError())
	}
	if len(resp.File) == 0 {
		t.Error("plugin produced no files for the spec protos")
	}
	for _, f := range resp.File {
		if !strings.HasSuffix(f.GetName(), ".pb.plainjson.go") {
			t.Errorf("unexpected output file %s", f.GetName())
		}
	}
}

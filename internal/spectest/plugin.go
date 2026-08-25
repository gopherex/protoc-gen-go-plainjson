package spectest

import (
	"fmt"
	"os"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// Descriptors is a compiled FileDescriptorSet used to drive the plugin in
// tests without shelling out to protoc. The fixtures are refreshed by
// `make gen` and `make gen-invalid`.
type Descriptors struct {
	set *descriptorpb.FileDescriptorSet
}

// LoadDescriptors reads a FileDescriptorSet fixture.
func LoadDescriptors(path string) (*Descriptors, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(raw, set); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Descriptors{set: set}, nil
}

// Files lists the paths in the set, in dependency order.
func (d *Descriptors) Files() []string {
	out := make([]string, 0, len(d.set.File))
	for _, f := range d.set.File {
		out = append(out, f.GetName())
	}
	return out
}

// PluginFor builds a protogen.Plugin that generates exactly the named files,
// with everything else in the set available as an import.
func (d *Descriptors) PluginFor(paths ...string) (*protogen.Plugin, error) {
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: paths,
		ProtoFile:      d.set.File,
	}
	return protogen.Options{}.New(req)
}

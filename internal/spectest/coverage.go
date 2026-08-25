package spectest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gopherex/protoc-gen-go-plainjson/plainjson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Coverage is what the spec protos actually exercise, gathered from the
// descriptors rather than from the Go code, so it stays true as the suite
// grows.
type Coverage struct {
	// OptionFields are set option fields, as "MessageOptions.flatten".
	OptionFields map[string]bool
	// EnumValues are option enum values used, as "FlattenMode.FLATTEN_MODE_DEEP".
	EnumValues map[string]bool
	// Generated are messages the plugin is expected to generate for.
	Generated map[string]bool
}

// Collect walks every registered file under the given proto package prefix.
func Collect(prefix string) (*Coverage, error) {
	cov := &Coverage{
		OptionFields: map[string]bool{},
		EnumValues:   map[string]bool{},
		Generated:    map[string]bool{},
	}

	var walkErr error
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(fd.Package()), prefix) {
			return true
		}

		fileOpts, _ := proto.GetExtension(fd.Options(), plainjson.E_File).(*plainjson.FileOptions)
		cov.record(fileOpts)

		var walkMessages func(protoreflect.MessageDescriptors)
		walkMessages = func(msgs protoreflect.MessageDescriptors) {
			for i := 0; i < msgs.Len(); i++ {
				md := msgs.Get(i)
				if md.IsMapEntry() {
					continue
				}
				msgOpts, _ := proto.GetExtension(md.Options(), plainjson.E_Message).(*plainjson.MessageOptions)
				cov.record(msgOpts)
				if generates(fileOpts, msgOpts) {
					cov.Generated[string(md.FullName())] = true
				}

				for j := 0; j < md.Oneofs().Len(); j++ {
					od := md.Oneofs().Get(j)
					if od.IsSynthetic() {
						continue
					}
					oneofOpts, _ := proto.GetExtension(od.Options(), plainjson.E_Oneof).(*plainjson.OneofOptions)
					cov.record(oneofOpts)
				}
				for j := 0; j < md.Fields().Len(); j++ {
					fieldOpts, _ := proto.GetExtension(md.Fields().Get(j).Options(), plainjson.E_Field).(*plainjson.FieldOptions)
					cov.record(fieldOpts)
				}
				walkMessages(md.Messages())
			}
		}
		walkMessages(fd.Messages())

		for i := 0; i < fd.Enums().Len(); i++ {
			ed := fd.Enums().Get(i)
			enumOpts, _ := proto.GetExtension(ed.Options(), plainjson.E_Enum).(*plainjson.EnumOptions)
			cov.record(enumOpts)
			for j := 0; j < ed.Values().Len(); j++ {
				valueOpts, _ := proto.GetExtension(ed.Values().Get(j).Options(), plainjson.E_EnumValue).(*plainjson.EnumValueOptions)
				cov.record(valueOpts)
			}
		}
		return true
	})
	return cov, walkErr
}

// generates mirrors the plugin's decision of whose marshalers are emitted:
// an explicit generate wins, otherwise the file's generate_all decides.
func generates(file *plainjson.FileOptions, msg *plainjson.MessageOptions) bool {
	if msg != nil && msg.Generate != nil {
		return msg.GetGenerate()
	}
	return file.GetGenerateAll()
}

// record folds one option message into the coverage sets.
func (c *Coverage) record(m proto.Message) {
	if m == nil {
		return
	}
	r := m.ProtoReflect()
	if !r.IsValid() {
		return
	}
	name := string(r.Descriptor().Name())

	r.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		c.OptionFields[name+"."+string(fd.Name())] = true

		switch {
		case fd.IsList():
			for i := 0; i < v.List().Len(); i++ {
				c.recordValue(fd, v.List().Get(i))
			}
		case fd.IsMap():
			v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
				c.recordValue(fd, mv)
				return true
			})
		default:
			c.recordValue(fd, v)
		}
		return true
	})
}

// recordValue records enum usage and descends into nested option messages.
func (c *Coverage) recordValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) {
	switch fd.Kind() {
	case protoreflect.EnumKind:
		ed := fd.Enum()
		vd := ed.Values().ByNumber(v.Enum())
		if vd != nil {
			c.EnumValues[string(ed.Name())+"."+string(vd.Name())] = true
		}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		c.record(v.Message().Interface())
	}
}

// AllOptionFields lists every field of every plainjson option message.
func AllOptionFields() []string {
	var out []string
	for _, m := range []proto.Message{
		(*plainjson.FileOptions)(nil), (*plainjson.MessageOptions)(nil),
		(*plainjson.OneofOptions)(nil), (*plainjson.FieldOptions)(nil),
		(*plainjson.EnumOptions)(nil), (*plainjson.EnumValueOptions)(nil),
		(*plainjson.MergeRule)(nil), (*plainjson.Constant)(nil),
		(*plainjson.ExclusiveGroup)(nil), (*plainjson.Lift)(nil),
	} {
		md := m.ProtoReflect().Descriptor()
		for i := 0; i < md.Fields().Len(); i++ {
			out = append(out, fmt.Sprintf("%s.%s", md.Name(), md.Fields().Get(i).Name()))
		}
	}
	sort.Strings(out)
	return out
}

// AllEnumValues lists every option enum value except the UNSPECIFIED zeros,
// which mean "inherit" and are exercised by every case that omits the option.
func AllEnumValues() []string {
	fd, err := protoregistry.GlobalFiles.FindFileByPath("plainjson/plainjson.proto")
	if err != nil {
		return nil
	}
	var out []string
	for i := 0; i < fd.Enums().Len(); i++ {
		ed := fd.Enums().Get(i)
		for j := 0; j < ed.Values().Len(); j++ {
			vd := ed.Values().Get(j)
			if strings.HasSuffix(string(vd.Name()), "_UNSPECIFIED") {
				continue
			}
			out = append(out, fmt.Sprintf("%s.%s", ed.Name(), vd.Name()))
		}
	}
	sort.Strings(out)
	return out
}

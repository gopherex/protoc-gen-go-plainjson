package spectest

import (
	"errors"
	"reflect"
	"testing"

	"github.com/go-faster/jx"
	"github.com/gopherex/protoc-gen-go-plainjson/plainjsonpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Run executes every case as a subtest.
func (s Suite) Run(t *testing.T) {
	t.Helper()
	for _, c := range s {
		t.Run(c.Name, c.Run)
	}
}

// Run executes one case.
func (c Case) Run(t *testing.T) {
	t.Helper()
	if c.Skip != "" {
		t.Skip(c.Skip)
	}

	msg, err := c.message()
	if err != nil {
		t.Fatalf("%s: %v", c.file, err)
	}

	m, ok := msg.(plainjsonpb.Marshaler)
	if c.NoMarshaler {
		if ok {
			t.Fatalf("%s: expected no marshaler for %s, but one was generated", c.file, c.Message)
		}
		return
	}
	if !ok {
		t.Fatalf("%s: %s does not implement plainjsonpb.Marshaler; spec section %s",
			c.file, c.Message, c.Spec)
	}

	if c.NilReceiver {
		m = reflect.Zero(reflect.TypeOf(m)).Interface().(plainjsonpb.Marshaler)
	}

	got, err := m.MarshalPlainJSON()
	if c.WantError != "" {
		c.checkError(t, err)
		return
	}
	if err != nil {
		t.Fatalf("MarshalPlainJSON: unexpected error: %v", err)
	}
	if string(got) != c.Want {
		t.Errorf("output mismatch\n got: %s\nwant: %s\nspec: %s", got, c.Want, c.Spec)
	}

	c.checkInvariants(t, m, got)
}

// message builds the input message for the case.
func (c Case) message() (proto.Message, error) {
	mt, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(c.Message))
	if err != nil {
		return nil, err
	}
	msg := mt.New().Interface()
	if len(c.Input) > 0 && !c.NilReceiver {
		if err := (protojson.UnmarshalOptions{}).Unmarshal(c.Input, msg); err != nil {
			return nil, err
		}
	}
	return msg, nil
}

// checkError asserts the failure the case expects.
func (c Case) checkError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error, got success; spec: %s", c.WantError, c.Spec)
	}
	switch c.WantError {
	case "key_collision":
		var ke *plainjsonpb.KeyCollisionError
		if !errors.As(err, &ke) {
			t.Fatalf("expected *plainjsonpb.KeyCollisionError, got %T: %v", err, err)
		}
		if c.ErrorKey != "" && ke.Key != c.ErrorKey {
			t.Errorf("collision key = %q, want %q", ke.Key, c.ErrorKey)
		}
	case "merge_conflict":
		var me *plainjsonpb.MergeConflictError
		if !errors.As(err, &me) {
			t.Fatalf("expected *plainjsonpb.MergeConflictError, got %T: %v", err, err)
		}
		if c.ErrorKey != "" && me.Key != c.ErrorKey {
			t.Errorf("merge key = %q, want %q", me.Key, c.ErrorKey)
		}
		if len(me.Paths) < 2 {
			t.Errorf("merge conflict should name at least two sources, got %v", me.Paths)
		}
	default:
		t.Fatalf("%s: unknown want_error %q", c.file, c.WantError)
	}
}

// encodeViaStream runs the streaming entry point, which must agree with the
// buffered one.
func encodeViaStream(m plainjsonpb.Marshaler) ([]byte, error) {
	var e jx.Encoder
	if err := m.EncodePlainJSON(&e); err != nil {
		return nil, err
	}
	return e.Bytes(), nil
}

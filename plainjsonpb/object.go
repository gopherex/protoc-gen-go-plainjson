package plainjsonpb

import "github.com/go-faster/jx"

// Keys tracks the keys an object has written, so a message can enforce its
// collision policy at run time. Generated code only creates one when the plan
// needs it: a dynamic source, or COLLISION_POLICY_ERROR_RUNTIME.
type Keys struct {
	seen   map[string]string
	strict bool
}

// NewKeys returns a tracker. When strict, a repeated key is an error instead
// of a silently dropped write.
func NewKeys(strict bool) *Keys {
	return &Keys{seen: make(map[string]string, 16), strict: strict}
}

// Claim registers a key before it is written. It reports whether the write
// should happen, and fails when a strict tracker sees a duplicate.
func (k *Keys) Claim(key, source string) (bool, error) {
	if prev, ok := k.seen[key]; ok {
		if k.strict {
			return false, &KeyCollisionError{Key: key, First: prev, Second: source}
		}
		return false, nil
	}
	k.seen[key] = source
	return true, nil
}

// Object buffers an object so a later write can replace an earlier one, as
// COLLISION_WINS_LAST requires. Key order follows the first write of each key.
type Object struct {
	order   []string
	values  map[string][]byte
	scratch jx.Encoder
}

// NewObject returns an empty buffered object.
func NewObject() *Object {
	return &Object{values: make(map[string][]byte, 16)}
}

// Set records a key's value, replacing any previous value in place.
func (o *Object) Set(key string, write func(e *jx.Encoder) error) error {
	o.scratch.Reset()
	if err := write(&o.scratch); err != nil {
		return err
	}
	raw := append([]byte(nil), o.scratch.Bytes()...)
	if _, seen := o.values[key]; !seen {
		o.order = append(o.order, key)
	}
	o.values[key] = raw
	return nil
}

// Encode writes the buffered object.
func (o *Object) Encode(e *jx.Encoder) {
	e.ObjStart()
	for _, key := range o.order {
		e.FieldStart(key)
		e.Raw(o.values[key])
	}
	e.ObjEnd()
}

// Encoded runs a write into a scratch encoder and returns its bytes. Used by
// the modes that need an element's object before deciding what to do with its
// keys.
func Encoded(write func(e *jx.Encoder) error) ([]byte, error) {
	var e jx.Encoder
	if err := write(&e); err != nil {
		return nil, err
	}
	return e.Bytes(), nil
}

// Spread copies the pairs of an already encoded object into the current one,
// prefixing every key and honouring the key tracker when there is one.
func Spread(e *jx.Encoder, keys *Keys, raw []byte, prefix, source string) error {
	return spread(raw, prefix, keys, source, func(key string, value []byte) {
		e.FieldStart(key)
		e.Raw(value)
	})
}

// SpreadTo is Spread for a buffered object.
func SpreadTo(o *Object, keys *Keys, raw []byte, prefix, source string) error {
	var setErr error
	err := spread(raw, prefix, keys, source, func(key string, value []byte) {
		if setErr != nil {
			return
		}
		setErr = o.Set(key, func(e *jx.Encoder) error {
			e.Raw(value)
			return nil
		})
	})
	if err != nil {
		return err
	}
	return setErr
}

// spread walks the pairs of an encoded object, applying the tracker's verdict
// before handing each surviving pair to write.
func spread(raw []byte, prefix string, keys *Keys, source string, write func(key string, value []byte)) error {
	d := jx.DecodeBytes(raw)
	return d.Obj(func(d *jx.Decoder, key string) error {
		value, err := d.Raw()
		if err != nil {
			return err
		}
		full := prefix + key
		if keys != nil {
			ok, err := keys.Claim(full, source)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}
		write(full, value)
		return nil
	})
}

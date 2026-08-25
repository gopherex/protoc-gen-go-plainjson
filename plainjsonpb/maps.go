package plainjsonpb

import (
	"sort"
	"strconv"
)

// MapKey is every type protobuf allows as a map key.
type MapKey interface {
	~string | ~int32 | ~int64 | ~uint32 | ~uint64 | ~bool
}

// SortedKeys returns a map's keys in a stable order, so output bytes do not
// depend on Go's map iteration.
func SortedKeys[K MapKey, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keyLess(keys[i], keys[j]) })
	return keys
}

// keyLess orders map keys by their natural order.
func keyLess[K MapKey](a, b K) bool {
	switch x := any(a).(type) {
	case string:
		return x < any(b).(string)
	case int32:
		return x < any(b).(int32)
	case int64:
		return x < any(b).(int64)
	case uint32:
		return x < any(b).(uint32)
	case uint64:
		return x < any(b).(uint64)
	case bool:
		return !x && any(b).(bool)
	default:
		return false
	}
}

// KeyString renders a map key the way protojson spells it in an object key.
func KeyString[K MapKey](k K) string {
	switch x := any(k).(type) {
	case string:
		return x
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		return ""
	}
}

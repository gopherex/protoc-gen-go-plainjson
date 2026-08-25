package plainjsonpb

import (
	"encoding/base64"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/jx"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The formats below mirror the plainjson option enums. Generated code passes
// the numeric value straight through, so the constants must stay in step with
// plainjson/plainjson.proto.
const (
	BytesBase64    = 1
	BytesBase64URL = 2
	BytesHex       = 3
	BytesArray     = 4

	TimeRFC3339     = 1
	TimeUnixSeconds = 2
	TimeUnixMilli   = 3
	TimeUnixMicro   = 4
	TimeUnixNano    = 5

	DurationProtojson = 1
	DurationSeconds   = 2
	DurationMillis    = 3
	DurationNanos     = 4
)

// Float64 writes a double the protojson way: NaN and the infinities become
// strings, and a negative zero keeps its sign.
func Float64(e *jx.Encoder, v float64) {
	switch {
	case math.IsNaN(v):
		e.Str("NaN")
	case math.IsInf(v, 1):
		e.Str("Infinity")
	case math.IsInf(v, -1):
		e.Str("-Infinity")
	case v == 0 && math.Signbit(v):
		e.Raw([]byte("-0"))
	default:
		e.Raw([]byte(strconv.FormatFloat(v, 'g', -1, 64)))
	}
}

// Float32 is Float64 for single precision.
func Float32(e *jx.Encoder, v float32) {
	f := float64(v)
	switch {
	case math.IsNaN(f):
		e.Str("NaN")
	case math.IsInf(f, 1):
		e.Str("Infinity")
	case math.IsInf(f, -1):
		e.Str("-Infinity")
	case f == 0 && math.Signbit(f):
		e.Raw([]byte("-0"))
	default:
		e.Raw([]byte(strconv.FormatFloat(f, 'g', -1, 32)))
	}
}

// Bytes writes a bytes field in the requested format.
func Bytes(e *jx.Encoder, v []byte, format int32) {
	switch format {
	case BytesBase64URL:
		e.Str(base64.RawURLEncoding.EncodeToString(v))
	case BytesHex:
		e.Str(hex.EncodeToString(v))
	case BytesArray:
		e.ArrStart()
		for _, b := range v {
			e.UInt(uint(b))
		}
		e.ArrEnd()
	default:
		e.Str(base64.StdEncoding.EncodeToString(v))
	}
}

// Timestamp writes a google.protobuf.Timestamp in the requested format.
func Timestamp(e *jx.Encoder, ts *timestamppb.Timestamp, format int32) {
	if ts == nil {
		e.Null()
		return
	}
	switch format {
	case TimeUnixSeconds:
		e.Int64(ts.GetSeconds())
	case TimeUnixMilli:
		e.Int64(ts.GetSeconds()*1e3 + int64(ts.GetNanos())/1e6)
	case TimeUnixMicro:
		e.Int64(ts.GetSeconds()*1e6 + int64(ts.GetNanos())/1e3)
	case TimeUnixNano:
		e.Int64(ts.GetSeconds()*1e9 + int64(ts.GetNanos()))
	default:
		t := ts.AsTime().UTC()
		e.Str(formatRFC3339(t))
	}
}

// formatRFC3339 renders a timestamp the way protojson does: no fractional part
// when it is zero, otherwise 3, 6 or 9 digits.
func formatRFC3339(t time.Time) string {
	switch nanos := t.Nanosecond(); {
	case nanos == 0:
		return t.Format("2006-01-02T15:04:05Z")
	case nanos%1e6 == 0:
		return t.Format("2006-01-02T15:04:05.000Z")
	case nanos%1e3 == 0:
		return t.Format("2006-01-02T15:04:05.000000Z")
	default:
		return t.Format("2006-01-02T15:04:05.000000000Z")
	}
}

// Duration writes a google.protobuf.Duration in the requested format.
func Duration(e *jx.Encoder, d *durationpb.Duration, format int32) {
	if d == nil {
		e.Null()
		return
	}
	switch format {
	case DurationSeconds:
		Float64(e, float64(d.GetSeconds())+float64(d.GetNanos())/1e9)
	case DurationMillis:
		e.Int64(d.GetSeconds()*1e3 + int64(d.GetNanos())/1e6)
	case DurationNanos:
		e.Int64(d.GetSeconds()*1e9 + int64(d.GetNanos()))
	default:
		e.Str(protojsonDuration(d))
	}
}

// protojsonDuration renders "1.500s", matching protojson's digit grouping.
func protojsonDuration(d *durationpb.Duration) string {
	secs, nanos := d.GetSeconds(), d.GetNanos()
	sign := ""
	if secs < 0 || nanos < 0 {
		sign = "-"
		secs, nanos = -secs, -nanos
	}
	switch {
	case nanos == 0:
		return sign + strconv.FormatInt(secs, 10) + "s"
	case nanos%1e6 == 0:
		return sign + strconv.FormatInt(secs, 10) + "." + pad(nanos/1e6, 3) + "s"
	case nanos%1e3 == 0:
		return sign + strconv.FormatInt(secs, 10) + "." + pad(nanos/1e3, 6) + "s"
	default:
		return sign + strconv.FormatInt(secs, 10) + "." + pad(nanos, 9) + "s"
	}
}

// pad renders n zero-padded to width digits.
func pad(n int32, width int) string {
	s := strconv.FormatInt(int64(n), 10)
	if len(s) >= width {
		return s
	}
	return strings.Repeat("0", width-len(s)) + s
}

// FieldMask writes a google.protobuf.FieldMask as a lowerCamel comma list.
func FieldMask(e *jx.Encoder, fm *fieldmaskpb.FieldMask, _ int32) {
	if fm == nil {
		e.Null()
		return
	}
	paths := make([]string, 0, len(fm.GetPaths()))
	for _, p := range fm.GetPaths() {
		paths = append(paths, lowerCamelPath(p))
	}
	e.Str(strings.Join(paths, ","))
}

// lowerCamelPath converts a field mask path to its JSON spelling.
func lowerCamelPath(path string) string {
	var b strings.Builder
	upper := false
	for _, r := range path {
		switch {
		case r == '_':
			upper = true
		case upper:
			b.WriteRune(upperRune(r))
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func upperRune(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 'a' + 'A'
	}
	return r
}

// Int64 writes a 64-bit signed integer as a protojson string or a number.
func Int64(e *jx.Encoder, v int64, asNumber bool) {
	if asNumber {
		e.Int64(v)
		return
	}
	e.Str(strconv.FormatInt(v, 10))
}

// UInt64 writes a 64-bit unsigned integer as a protojson string or a number.
func UInt64(e *jx.Encoder, v uint64, asNumber bool) {
	if asNumber {
		e.UInt64(v)
		return
	}
	e.Str(strconv.FormatUint(v, 10))
}

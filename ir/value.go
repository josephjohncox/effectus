package ir

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// NormalizeValue validates a value against a closed checked type and returns an
// owned, JSON-safe copy. Integers are signed int64, floats are finite float64,
// and bytes are canonical padded base64 strings (raw []byte is also accepted).
// Lists accept Go slices/arrays; maps and objects require string keys. Null is
// accepted only by the null type. Values deeper than 64 levels are rejected.
// The environment is declarations only; no callbacks are invoked.
func NormalizeValue(environment Environment, typeName string, value any) (any, error) {
	environment, err := normalizeEnvironment(environment)
	if err != nil {
		return nil, fmt.Errorf("environment: %w", err)
	}
	checker := typeChecker{environment: environment}
	ref, err := checker.parse(typeName, false)
	if err != nil {
		return nil, err
	}
	nodes := 0
	return checker.normalizeValue(ref, value, 0, &nodes)
}

func (c typeChecker) normalizeValue(ref *typeRef, value any, depth int, nodes *int) (any, error) {
	*nodes++
	if depth > 64 || *nodes > 10000 {
		return nil, fmt.Errorf("value depth or node limit exceeded")
	}
	resolved, err := c.resolve(ref, make(map[string]struct{}))
	if err != nil {
		return nil, err
	}
	mismatch := func() (any, error) { return nil, fmt.Errorf("got %T, want %s", value, c.describe(ref)) }
	switch resolved.kind {
	case typeNull:
		if value == nil {
			return nil, nil
		}
	case typeBool:
		if v, ok := value.(bool); ok {
			return v, nil
		}
	case typeString:
		if v, ok := value.(string); ok && utf8.ValidString(v) {
			return v, nil
		}
	case typeBytes:
		switch v := value.(type) {
		case []byte:
			return base64.StdEncoding.EncodeToString(v), nil
		case string:
			decoded, err := base64.StdEncoding.Strict().DecodeString(v)
			if err != nil || base64.StdEncoding.EncodeToString(decoded) != v {
				return nil, fmt.Errorf("bytes must be canonical padded base64")
			}
			return v, nil
		}
	case typeInt:
		if v, ok := value.(json.Number); ok {
			rational, err := exactJSONNumber(v)
			if err == nil && rational.IsInt() && rational.Num().IsInt64() {
				return rational.Num().Int64(), nil
			}
		} else if value != nil {
			rv := reflect.ValueOf(value)
			switch rv.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				return rv.Int(), nil
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				if rv.Uint() <= math.MaxInt64 {
					return int64(rv.Uint()), nil
				}
			case reflect.Float32, reflect.Float64:
				f := rv.Float()
				// The upper boundary is exclusive: float64(MaxInt64) rounds to 2^63.
				if f >= -0x1p63 && f < 0x1p63 && f == math.Trunc(f) {
					return int64(f), nil
				}
			}
		}
	case typeFloat:
		var f float64
		var ok bool
		if v, number := value.(json.Number); number {
			if _, err := exactJSONNumber(v); err == nil {
				f, err = v.Float64()
				ok = err == nil
			}
		} else if value != nil {
			rv := reflect.ValueOf(value)
			switch rv.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				f, ok = float64(rv.Int()), true
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				f, ok = float64(rv.Uint()), true
			case reflect.Float32, reflect.Float64:
				f, ok = rv.Float(), true
			}
		}
		if ok && !math.IsInf(f, 0) && !math.IsNaN(f) {
			return f, nil
		}
	case typeList:
		rv := reflect.ValueOf(value)
		if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
			return mismatch()
		}
		if rv.Len() > 10000 {
			return nil, fmt.Errorf("list item limit exceeded")
		}
		result := make([]any, rv.Len())
		for i := range result {
			result[i], err = c.normalizeValue(resolved.element, rv.Index(i).Interface(), depth+1, nodes)
			if err != nil {
				return nil, fmt.Errorf("list item %d: %w", i, err)
			}
		}
		return result, nil
	case typeMap, typeObject:
		rv := reflect.ValueOf(value)
		if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
			return mismatch()
		}
		if rv.Len() > 10000 {
			return nil, fmt.Errorf("object/map field limit exceeded")
		}
		keys := rv.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		result := make(map[string]any, len(keys))
		for _, key := range keys {
			name := key.String()
			if !utf8.ValidString(name) {
				return nil, fmt.Errorf("field name is not valid UTF-8")
			}
			expected := resolved.element
			if resolved.kind == typeObject {
				expected = resolved.fields[name]
				if expected == nil {
					return nil, fmt.Errorf("unknown field %q", name)
				}
			}
			result[name], err = c.normalizeValue(expected, rv.MapIndex(key).Interface(), depth+1, nodes)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", name, err)
			}
		}
		if resolved.kind == typeObject {
			for _, required := range c.environment.Types[resolved.name].RequiredFields {
				if _, ok := result[required]; !ok {
					return nil, fmt.Errorf("missing required field %q", required)
				}
			}
		}
		return result, nil
	}
	return mismatch()
}

// Bound exponent expansion before big.Rat allocation. This comfortably covers
// int64 and finite float64, without permitting adversarial billion-digit powers.
func exactJSONNumber(value json.Number) (*big.Rat, error) {
	text := string(value)
	if len(text) > 1024 || !json.Valid([]byte(text)) {
		return nil, fmt.Errorf("invalid JSON number")
	}
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(text[index+1:])
		if err != nil || exponent < -400 || exponent > 400 {
			return nil, fmt.Errorf("number exponent out of range")
		}
	}
	rational, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("invalid JSON number")
	}
	return rational, nil
}

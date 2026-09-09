package schema

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
)

// cloneBufValue owns JSON-shaped containers without changing numeric types.
// A depth limit rejects recursive values without invoking user marshalers.
func cloneBufValue(value reflect.Value, depth int) (reflect.Value, error) {
	if depth > 64 {
		return reflect.Value{}, fmt.Errorf("schema value exceeds maximum depth or contains a cycle")
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		child, err := cloneBufValue(value.Elem(), depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		result := reflect.New(value.Type()).Elem()
		result.Set(child)
		return result, nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf("schema object keys must be strings")
		}
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			child, err := cloneBufValue(iter.Value(), depth+1)
			if err != nil {
				return reflect.Value{}, err
			}
			result.SetMapIndex(iter.Key(), child)
		}
		return result, nil
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		var result reflect.Value
		if value.Kind() == reflect.Slice {
			result = reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		} else {
			result = reflect.New(value.Type()).Elem()
		}
		for i := 0; i < value.Len(); i++ {
			child, err := cloneBufValue(value.Index(i), depth+1)
			if err != nil {
				return reflect.Value{}, err
			}
			result.Index(i).Set(child)
		}
		return result, nil
	case reflect.Float32, reflect.Float64:
		if math.IsNaN(value.Float()) || math.IsInf(value.Float(), 0) {
			return reflect.Value{}, fmt.Errorf("schema numbers must be finite")
		}
		return value, nil
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return value, nil
	default:
		return reflect.Value{}, fmt.Errorf("unsupported schema value type %s", value.Type())
	}
}

func cloneBufMap(value map[string]interface{}) (map[string]interface{}, error) {
	copy, err := cloneBufValue(reflect.ValueOf(value), 0)
	if err != nil {
		return nil, err
	}
	return copy.Interface().(map[string]interface{}), nil
}

func cloneBufVerb(source *VerbSchema) (*VerbSchema, error) {
	copy := *source
	var err error
	copy.InputSchema, err = cloneBufMap(source.InputSchema)
	if err != nil {
		return nil, err
	}
	copy.OutputSchema, err = cloneBufMap(source.OutputSchema)
	if err != nil {
		return nil, err
	}
	copy.RequiredCapabilities = slices.Clone(source.RequiredCapabilities)
	return &copy, nil
}

func cloneBufFact(source *FactSchema) (*FactSchema, error) {
	copy := *source
	var err error
	copy.Schema, err = cloneBufMap(source.Schema)
	if err != nil {
		return nil, err
	}
	copy.Indexes = slices.Clone(source.Indexes)
	for i := range copy.Indexes {
		copy.Indexes[i].Fields = slices.Clone(source.Indexes[i].Fields)
		copy.Indexes[i].Options = maps.Clone(source.Indexes[i].Options)
	}
	copy.PrivacyRules = slices.Clone(source.PrivacyRules)
	for i := range copy.PrivacyRules {
		copy.PrivacyRules[i].AllowedRoles = slices.Clone(source.PrivacyRules[i].AllowedRoles)
		copy.PrivacyRules[i].Conditions = maps.Clone(source.PrivacyRules[i].Conditions)
	}
	if source.RetentionPolicy != nil {
		retention := *source.RetentionPolicy
		retention.Conditions = maps.Clone(source.RetentionPolicy.Conditions)
		copy.RetentionPolicy = &retention
	}
	return &copy, nil
}

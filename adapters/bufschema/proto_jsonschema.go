package bufschema

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/effectus/effectus-go/adapters"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func (p *Provider) generateSchemasFromProto(ctx context.Context) ([]adapters.SchemaDefinition, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	descriptorPath, cleanup, err := p.buildDescriptorSet(ctx)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}

	set, err := readDescriptorSet(descriptorPath)
	if err != nil {
		return nil, err
	}

	files, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, fmt.Errorf("loading descriptor set: %w", err)
	}

	msgIndex := make(map[protoreflect.FullName]protoreflect.MessageDescriptor)
	topLevel := make([]protoreflect.MessageDescriptor, 0)

	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !packageAllowed(string(fd.Package()), p.config) {
			return true
		}
		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			msg := msgs.Get(i)
			collectMessages(msg, msgIndex)
			topLevel = append(topLevel, msg)
		}
		return true
	})

	defs := make([]adapters.SchemaDefinition, 0)
	seen := make(map[string]struct{})
	for _, msg := range topLevel {
		if !packageAllowed(string(msg.ParentFile().Package()), p.config) {
			continue
		}
		name := string(msg.FullName())
		if _, ok := seen[name]; ok {
			continue
		}
		schema := messageToJSONSchema(msg, msgIndex, map[protoreflect.FullName]bool{})
		payload, err := jsonMarshal(schema)
		if err != nil {
			return nil, fmt.Errorf("encoding schema for %s: %w", name, err)
		}
		defs = append(defs, adapters.SchemaDefinition{
			Name:    name,
			Version: p.config.SchemaVersion,
			Format:  adapters.SchemaFormatJSONSchema,
			Data:    payload,
			Source:  p.config.Module,
		})
		seen[name] = struct{}{}
	}

	if len(defs) == 0 {
		return nil, fmt.Errorf("no proto messages found to convert")
	}

	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs, nil
}

func (p *Provider) buildDescriptorSet(ctx context.Context) (string, func(), error) {
	if p.config.Module == "" && p.config.Dir == "" {
		return "", nil, fmt.Errorf("proto generation requires module or dir")
	}

	file, err := os.CreateTemp("", "effectus-descriptor-*.pb")
	if err != nil {
		return "", nil, fmt.Errorf("creating descriptor temp file: %w", err)
	}
	path := file.Name()
	_ = file.Close()
	cleanup := func() { _ = os.Remove(path) }

	args := []string{"build"}
	module := p.config.Module
	if module != "" {
		if p.config.Ref != "" && !strings.Contains(module, ":") && !strings.Contains(module, "@") {
			module = fmt.Sprintf("%s:%s", module, p.config.Ref)
		}
		args = append(args, module)
	}
	args = append(args, "-o", path)

	cmd := exec.CommandContext(ctx, "buf", args...)
	if module == "" {
		cmd.Dir = p.config.Dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("buf build failed: %v: %s", err, strings.TrimSpace(string(output)))
	}

	return path, cleanup, nil
}

func readDescriptorSet(path string) (*descriptorpb.FileDescriptorSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading descriptor set: %w", err)
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(data, set); err != nil {
		return nil, fmt.Errorf("parsing descriptor set: %w", err)
	}
	return set, nil
}

func collectMessages(msg protoreflect.MessageDescriptor, index map[protoreflect.FullName]protoreflect.MessageDescriptor) {
	index[msg.FullName()] = msg
	nested := msg.Messages()
	for i := 0; i < nested.Len(); i++ {
		collectMessages(nested.Get(i), index)
	}
}

func packageAllowed(pkg string, cfg *Config) bool {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return true
	}
	if strings.HasPrefix(pkg, "google.protobuf") || strings.HasPrefix(pkg, "buf.") {
		return false
	}
	if len(cfg.IncludePkgs) > 0 {
		allowed := false
		for _, prefix := range cfg.IncludePkgs {
			prefix = strings.TrimSpace(prefix)
			if prefix == "" {
				continue
			}
			if strings.HasPrefix(pkg, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	for _, prefix := range cfg.ExcludePkgs {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		if strings.HasPrefix(pkg, prefix) {
			return false
		}
	}
	return true
}

func messageToJSONSchema(msg protoreflect.MessageDescriptor, index map[protoreflect.FullName]protoreflect.MessageDescriptor, visited map[protoreflect.FullName]bool) map[string]interface{} {
	if visited[msg.FullName()] {
		return map[string]interface{}{"type": "object"}
	}
	visited[msg.FullName()] = true
	defer delete(visited, msg.FullName())

	properties := make(map[string]interface{})
	required := make([]string, 0)

	fields := msg.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		name := string(field.JSONName())
		properties[name] = fieldToSchema(field, index, visited)
		if field.Cardinality() == protoreflect.Required {
			required = append(required, name)
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func fieldToSchema(field protoreflect.FieldDescriptor, index map[protoreflect.FullName]protoreflect.MessageDescriptor, visited map[protoreflect.FullName]bool) map[string]interface{} {
	if field.IsMap() {
		valueSchema := fieldKindToSchema(field.MapValue(), index, visited)
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": valueSchema,
		}
	}
	if field.IsList() {
		return map[string]interface{}{
			"type":  "array",
			"items": fieldKindToSchema(field, index, visited),
		}
	}
	return fieldKindToSchema(field, index, visited)
}

func fieldKindToSchema(field protoreflect.FieldDescriptor, index map[protoreflect.FullName]protoreflect.MessageDescriptor, visited map[protoreflect.FullName]bool) map[string]interface{} {
	switch field.Kind() {
	case protoreflect.BoolKind:
		return map[string]interface{}{"type": "boolean"}
	case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Uint32Kind, protoreflect.Uint64Kind, protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind,
		protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		return map[string]interface{}{"type": "integer"}
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return map[string]interface{}{"type": "number"}
	case protoreflect.StringKind:
		return map[string]interface{}{"type": "string"}
	case protoreflect.BytesKind:
		return map[string]interface{}{"type": "string"}
	case protoreflect.EnumKind:
		values := field.Enum().Values()
		enums := make([]string, 0, values.Len())
		for i := 0; i < values.Len(); i++ {
			enums = append(enums, string(values.Get(i).Name()))
		}
		schema := map[string]interface{}{"type": "string"}
		if len(enums) > 0 {
			schema["enum"] = enums
		}
		return schema
	case protoreflect.MessageKind, protoreflect.GroupKind:
		msg := field.Message()
		if msg == nil {
			return map[string]interface{}{"type": "object"}
		}
		if wellKnown := wellKnownSchema(msg.FullName()); wellKnown != nil {
			return wellKnown
		}
		if descriptor, ok := index[msg.FullName()]; ok {
			return messageToJSONSchema(descriptor, index, visited)
		}
		return map[string]interface{}{"type": "object"}
	default:
		return map[string]interface{}{"type": "object"}
	}
}

func wellKnownSchema(name protoreflect.FullName) map[string]interface{} {
	switch string(name) {
	case "google.protobuf.Timestamp", "google.protobuf.Duration":
		return map[string]interface{}{"type": "string"}
	case "google.protobuf.Any", "google.protobuf.Struct", "google.protobuf.Value":
		return map[string]interface{}{"type": "object"}
	case "google.protobuf.ListValue":
		return map[string]interface{}{"type": "array"}
	default:
		return nil
	}
}

func jsonMarshal(schema map[string]interface{}) ([]byte, error) {
	if schema == nil {
		return nil, fmt.Errorf("empty schema")
	}
	return json.Marshal(schema)
}

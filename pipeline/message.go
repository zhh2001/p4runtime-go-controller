package pipeline

import (
	"fmt"
	"reflect"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Validate before cloning. Protobuf cloning materializes nil message values
// and recursively follows pointers without detecting cycles.
func validateMessageGraph(message proto.Message) error {
	// A false value marks an active visit. A true value marks a completed one.
	seen := make(map[proto.Message]bool)
	var visit func(protoreflect.Message, string) error
	visit = func(m protoreflect.Message, path string) error {
		if !m.IsValid() {
			return fmt.Errorf("%s: missing P4Info message", path)
		}
		key := m.Interface()
		if done, ok := seen[key]; ok {
			if done {
				return nil
			}
			return fmt.Errorf("%s: recursive P4Info message", path)
		}
		seen[key] = false
		// Typed nil oneof wrappers are invisible to protobuf Range.
		value := reflect.ValueOf(key).Elem()
		for i := 0; i < value.NumField(); i++ {
			if name, ok := value.Type().Field(i).Tag.Lookup("protobuf_oneof"); ok {
				field := value.Field(i)
				if !field.IsNil() && reflect.ValueOf(field.Interface()).IsNil() {
					return fmt.Errorf("%s.%s: missing P4Info oneof message", path, name)
				}
			}
		}
		var err error
		m.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
			fieldPath := path + "." + string(field.Name())
			switch {
			case field.IsMap():
				if field.MapValue().Kind() == protoreflect.MessageKind {
					value.Map().Range(func(key protoreflect.MapKey, value protoreflect.Value) bool {
						err = visit(value.Message(), fmt.Sprintf("%s[%s]", fieldPath, key.String()))
						return err == nil
					})
				}
			case field.IsList() && field.Kind() == protoreflect.MessageKind:
				list := value.List()
				for i := 0; i < list.Len() && err == nil; i++ {
					err = visit(list.Get(i).Message(), fmt.Sprintf("%s[%d]", fieldPath, i))
				}
			case field.Kind() == protoreflect.MessageKind:
				child := value.Message()
				if !child.IsValid() && field.ContainingOneof() != nil {
					err = fmt.Errorf("%s: missing %s declaration", fieldPath, field.Name())
				} else {
					err = visit(child, fieldPath)
				}
			}
			return err == nil
		})
		if err == nil {
			seen[key] = true
		}
		return err
	}
	return visit(message.ProtoReflect(), "P4Info")
}

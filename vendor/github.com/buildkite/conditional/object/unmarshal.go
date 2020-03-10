package object

import (
	"fmt"
	"reflect"
	"strings"
)

const (
	tagName = `conditional`
)

// Unmarshal a golang objects into an Object
func Unmarshal(data interface{}, v Object) error {
	var handled bool

	// log.Printf("Unmarshal(%v, %v)", data, v)

	// handle basic types
	switch dt := data.(type) {
	case string:
		vt, ok := v.(*String)
		if ok {
			vt.Value = dt
			handled = true
		}
	case int:
		vt, ok := v.(*Integer)
		if ok {
			vt.Value = int64(dt)
			handled = true
		}
	case int64:
		vt, ok := v.(*Integer)
		if ok {
			vt.Value = dt
			handled = true
		}
	case bool:
		vt, ok := v.(*Boolean)
		if ok {
			vt.Value = dt
			handled = true
		}
	case map[string]interface{}:
		vt, ok := v.(Struct)
		if ok {
			if err := unmarshalInterfaceMap(dt, vt); err != nil {
				return err
			}
			handled = true
		}
	}

	// handle structs
	if isStruct(data) {
		vt, ok := v.(Struct)
		if ok {
			if err := unmarshalStruct(data, vt); err != nil {
				return err
			}
			handled = true
		}
	}

	if !handled {
		return fmt.Errorf("Unable to unmarshal %T into %T", data, v)
	}

	// log.Printf("Returning: %#v", v)

	return nil
}

func unmarshalInterfaceMap(data map[string]interface{}, into Struct) error {
	// log.Printf("unmarshalInterfaceMap: %#v", data)

	for k, v := range data {
		// log.Printf("Walking map field %s=>%v", k, v)

		switch vi := v.(type) {
		case string:
			into.Set(k, &String{vi})
		case int:
			into.Set(k, &Integer{int64(vi)})
		case int64:
			into.Set(k, &Integer{vi})
		case bool:
			into.Set(k, &Boolean{vi})
		default:
			if isStruct(v) || isMap(v) {
				val, exists := into[k]
				if !exists {
					val = Struct{}
				}
				if err := Unmarshal(v, val); err != nil {
					return err
				}
				into[k] = val
			} else {
				return fmt.Errorf("Unable to unmarshal field %s of type %T", k, vi)
			}
		}
	}
	return nil
}

func isMap(i interface{}) bool {
	return reflect.TypeOf(i).Kind() == reflect.Map
}

func isStruct(i interface{}) bool {
	return reflect.TypeOf(i).Kind() == reflect.Struct
}

func unmarshalStruct(data interface{}, into Struct) error {
	t := reflect.TypeOf(data)
	v := reflect.ValueOf(data)

	// Iterate over fields and use tags if we can
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get(tagName)

		// Use lowercase string if no tag
		if tag == "" {
			tag = strings.ToLower(field.Name)
		}

		// log.Printf("Walking struct field %d %v (%v)", i, field.Name, tag)

		// Handle basic types
		switch dt := v.Field(i).Interface().(type) {
		case string:
			into[tag] = &String{dt}
		case int:
			into[tag] = &Integer{int64(dt)}
		case int64:
			into[tag] = &Integer{int64(dt)}
		case bool:
			into[tag] = &Boolean{dt}
		default:
			if isStruct(dt) || isMap(dt) {
				val, exists := into[tag]
				if !exists {
					val = Struct{}
				}
				if err := Unmarshal(dt, val); err != nil {
					return err
				}
				into[tag] = val
			} else {
				return fmt.Errorf("Unable to unmarshal field %s of type %T (%v)", field.Name, dt, reflect.TypeOf(dt).Kind())
			}
		}
	}

	return nil
}

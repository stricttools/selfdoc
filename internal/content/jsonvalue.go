package content

import (
	"github.com/stricttools/selfdoc/internal/extractors"
)

// object is one decoded JSON object, remembering the order its keys appeared
// in.
//
// Order is read in one place -- an OpenAPI operation whose content declares
// several media types and no application/json, where the renderer documents
// the FIRST one the document declares -- and a Go map has no order to read.
type object = extractors.JSONObject

// asJSONObject is value as an object, or nil when it is anything else. A
// malformed fragment reads as an absent one rather than aborting the page,
// which is what the Python's `.get()` chains did.
func asJSONObject(value any) *object {
	obj, _ := value.(*object)
	return obj
}

// jsonGet is the value at key, or nil when obj is nil or carries no such key.
func jsonGet(obj *object, key string) any {
	if obj == nil {
		return nil
	}
	value, _ := obj.Get(key)
	return value
}

// jsonString is the string at key, or "" when the key is absent or its value
// is not a string.
func jsonString(obj *object, key string) string {
	value, _ := jsonGet(obj, key).(string)
	return value
}

// jsonObject is the object at key, or nil when the key is absent or its value
// is not an object.
func jsonObject(obj *object, key string) *object {
	return asJSONObject(jsonGet(obj, key))
}

// jsonList is the list at key, or nil when the key is absent or its value is
// not a list.
func jsonList(obj *object, key string) []any {
	items, _ := jsonGet(obj, key).([]any)
	return items
}

// jsonHas reports whether obj carries key at all.
func jsonHas(obj *object, key string) bool {
	if obj == nil {
		return false
	}
	_, ok := obj.Get(key)
	return ok
}

// jsonKeys lists an object's keys in declaration order, and none for a nil
// object -- which is what an absent fragment decodes to, and which the
// underlying type's own method would panic on.
func jsonKeys(obj *object) []string {
	if obj == nil {
		return nil
	}
	return obj.Keys()
}

// jsonLen is how many keys an object carries, and zero for a nil object.
func jsonLen(obj *object) int {
	if obj == nil {
		return 0
	}
	return obj.Len()
}

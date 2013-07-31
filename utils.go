package dbgp

import (
	"reflect"
)

func getFieldValueByName(obj interface{}, fieldName string) (interface{}, error) {
	return reflect.ValueOf(obj).FieldByName(fieldName), nil
}

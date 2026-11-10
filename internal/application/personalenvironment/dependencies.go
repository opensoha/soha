package personalenvironment

import (
	"context"
	"reflect"

	identity "github.com/opensoha/soha/internal/domain/identity"
)

type CurrentIdentity interface {
	CurrentPrincipal(context.Context, string) (identity.Principal, error)
}

func missingPort(port any) bool {
	value := reflect.ValueOf(port)
	switch value.Kind() {
	case reflect.Invalid:
		return true
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

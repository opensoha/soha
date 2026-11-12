package proxypublication

import "testing"

func fixtureValue[T any](t *testing.T, input any) T {
	t.Helper()
	value, ok := input.(T)
	if !ok {
		t.Fatalf("unexpected fixture type %T", input)
	}
	return value
}

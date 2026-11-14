package apiresponse

import "testing"

func TestItemAndItems(t *testing.T) {
	t.Parallel()

	if got := Item("value")["data"]; got != "value" {
		t.Fatalf("Item() data = %v, want value", got)
	}
	if got := Items([]string{"a"})["items"]; got == nil {
		t.Fatal("Items() items is nil")
	}
}

func TestError(t *testing.T) {
	t.Parallel()

	payload := Error("invalid_argument", "bad input", "request-1")
	body, ok := payload["error"].(ErrorBody)
	if !ok {
		t.Fatalf("Error() error body type = %T, want ErrorBody", payload["error"])
	}
	if body.Code != "invalid_argument" || body.Message != "bad input" || body.RequestID != "request-1" {
		t.Fatalf("Error() body = %#v", body)
	}
}

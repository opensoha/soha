package apiresponse

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func Item(data any) map[string]any {
	return map[string]any{"data": data}
}

func Items(items any) map[string]any {
	return map[string]any{"items": items}
}

func Error(code, message, requestID string) map[string]any {
	return map[string]any{
		"error": ErrorBody{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	}
}

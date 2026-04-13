package dto

type ListResponse[T any] struct {
	Items []T `json:"items"`
}

type DataResponse[T any] struct {
	Data T `json:"data"`
}

type ApplyResourceYAMLRequest struct {
	Content string `json:"content"`
}

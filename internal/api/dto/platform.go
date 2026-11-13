package dto

import "github.com/opensoha/soha/contracts/helmrelease"

type ApplyResourceYAMLRequest struct {
	Content string `json:"content"`
}

type ApplyHelmValuesRequest = helmrelease.ValuesUpdateRequest

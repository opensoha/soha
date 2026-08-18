package errors

import (
	"errors"
	"net/http"
	"strings"

	legacy "github.com/opensoha/soha/internal/platform/apperrors"
)

var (
	ErrUnauthorized         = legacy.ErrUnauthorized
	ErrAccessDenied         = legacy.ErrAccessDenied
	ErrMFARequired          = legacy.ErrMFARequired
	ErrConflict             = legacy.ErrConflict
	ErrNotFound             = legacy.ErrNotFound
	ErrClusterUnready       = legacy.ErrClusterUnready
	ErrServiceUnavailable   = legacy.ErrServiceUnavailable
	ErrInvalidArgument      = legacy.ErrInvalidArgument
	ErrUnsupportedOperation = legacy.ErrUnsupportedOperation
)

func RequestLanguage(request *http.Request) string {
	if request == nil {
		return ""
	}
	return request.Header.Get("Accept-Language")
}

func StatusCode(err error) int {
	switch {
	case errors.Is(err, ErrUnsupportedOperation):
		return http.StatusNotImplemented
	case errors.Is(err, ErrInvalidArgument):
		return http.StatusBadRequest
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, ErrAccessDenied):
		return http.StatusForbidden
	case errors.Is(err, ErrMFARequired):
		return http.StatusForbidden
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrClusterUnready):
		return http.StatusBadGateway
	case errors.Is(err, ErrServiceUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func Code(err error) string {
	var business *legacy.BusinessError
	if errors.As(err, &business) && business.Code() != "" {
		return business.Code()
	}
	switch {
	case errors.Is(err, ErrUnsupportedOperation):
		return "unsupported_operation"
	case errors.Is(err, ErrInvalidArgument):
		return "invalid_argument"
	case errors.Is(err, ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, ErrAccessDenied):
		return "access_denied"
	case errors.Is(err, ErrMFARequired):
		return "mfa_required"
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrClusterUnready):
		return "cluster_unavailable"
	case errors.Is(err, ErrServiceUnavailable):
		return "service_unavailable"
	default:
		return "internal_error"
	}
}

func Message(err error, acceptLanguage ...string) string {
	language := ""
	if len(acceptLanguage) > 0 {
		language = acceptLanguage[0]
	}
	var business *legacy.BusinessError
	if errors.As(err, &business) {
		return business.Message(language)
	}
	chinese := strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "zh")
	switch {
	case errors.Is(err, ErrUnsupportedOperation):
		return localizedMessage(chinese, "operation is not supported", "操作暂不支持")
	case errors.Is(err, ErrInvalidArgument):
		return localizedMessage(chinese, "invalid request", "请求参数不正确")
	case errors.Is(err, ErrUnauthorized):
		return localizedMessage(chinese, "authentication required", "请先登录")
	case errors.Is(err, ErrAccessDenied):
		return localizedMessage(chinese, "access denied", "没有执行此操作的权限")
	case errors.Is(err, ErrMFARequired):
		return localizedMessage(chinese, "multi-factor authentication is required", "需要完成多因素认证")
	case errors.Is(err, ErrConflict):
		return localizedMessage(chinese, "resource conflict", "资源状态冲突")
	case errors.Is(err, ErrNotFound):
		return localizedMessage(chinese, "resource not found", "未找到对应资源")
	case errors.Is(err, ErrClusterUnready):
		return localizedMessage(chinese, "cluster unavailable", "集群当前不可用")
	case errors.Is(err, ErrServiceUnavailable):
		return localizedMessage(chinese, "service unavailable", "服务暂时不可用")
	default:
		return localizedMessage(chinese, "internal server error", "服务器内部错误")
	}
}

func localizedMessage(chinese bool, english, translated string) string {
	if chinese {
		return translated
	}
	return english
}

package errors

import (
	"fmt"
	"net/http"
	"testing"

	platformerrors "github.com/opensoha/soha/internal/platform/apperrors"
)

func TestBusinessErrorOverridesPublicCodeAndMessage(t *testing.T) {
	err := fmt.Errorf("private dependency detail: %w", platformerrors.NewBusiness(
		platformerrors.ErrInvalidArgument,
		"agent_installation_unavailable",
		"Agent installation service is not ready",
		"Agent 安装服务尚未就绪，请检查服务端加密密钥和对外访问地址配置",
	))

	if got := StatusCode(err); got != http.StatusBadRequest {
		t.Fatalf("StatusCode() = %d", got)
	}
	if got := Code(err); got != "agent_installation_unavailable" {
		t.Fatalf("Code() = %q", got)
	}
	if got := Message(err); got != "Agent installation service is not ready" {
		t.Fatalf("Message() = %q", got)
	}
	if got := Message(err, "zh-CN"); got != "Agent 安装服务尚未就绪，请检查服务端加密密钥和对外访问地址配置" {
		t.Fatalf("Message(zh-CN) = %q", got)
	}
}

func TestGenericMessageUsesRequestedLanguage(t *testing.T) {
	if got := Message(ErrNotFound, "zh-CN"); got != "未找到对应资源" {
		t.Fatalf("Message(ErrNotFound, zh-CN) = %q", got)
	}
	if got := StatusCode(ErrServiceUnavailable); got != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode(ErrServiceUnavailable) = %d", got)
	}
	if got := Code(ErrServiceUnavailable); got != "service_unavailable" {
		t.Fatalf("Code(ErrServiceUnavailable) = %q", got)
	}
	if got := Message(ErrServiceUnavailable, "zh-CN"); got != "服务暂时不可用" {
		t.Fatalf("Message(ErrServiceUnavailable, zh-CN) = %q", got)
	}
}

package apperrors

import (
	"errors"
	"fmt"
	"testing"
)

func TestBusinessErrorKeepsCategoryAndLocalizedPublicDetails(t *testing.T) {
	business := NewBusiness(
		ErrInvalidArgument,
		"public_access_url_not_configured",
		"Soha public access URL is not configured",
		"Soha 对外访问地址尚未配置，请先在系统设置中配置后重试",
	)
	err := fmt.Errorf("load private server configuration: %w", business)

	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("errors.Is() = false, want invalid argument")
	}
	var resolved *BusinessError
	if !errors.As(err, &resolved) {
		t.Fatal("errors.As() = false, want BusinessError")
	}
	if resolved.Code() != "public_access_url_not_configured" {
		t.Fatalf("Code() = %q", resolved.Code())
	}
	if got := resolved.Message("zh-CN,zh;q=0.9"); got != "Soha 对外访问地址尚未配置，请先在系统设置中配置后重试" {
		t.Fatalf("Message(zh-CN) = %q", got)
	}
	if got := resolved.Message("en-US"); got != "Soha public access URL is not configured" {
		t.Fatalf("Message(en-US) = %q", got)
	}
}

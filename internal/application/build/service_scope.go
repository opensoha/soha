package build

import (
	"context"
	"fmt"
	"strings"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) validateServiceBuild(ctx context.Context, input *domainbuild.TriggerInput, source *domainapp.BuildSource, image string) error {
	if input.ServiceID == "" {
		return nil
	}
	reader, ok := s.apps.(interface {
		GetService(context.Context, string, string) (domainapp.Service, error)
	})
	if !ok {
		return fmt.Errorf("%w: service build scope is unavailable", apperrors.ErrInvalidArgument)
	}
	service, err := reader.GetService(ctx, input.ApplicationID, input.ServiceID)
	if err != nil {
		return err
	}
	if service.ApplicationID != input.ApplicationID || source == nil || source.ID != service.BuildSourceID {
		return fmt.Errorf("%w: build source must be bound to the selected service", apperrors.ErrInvalidArgument)
	}
	input.BuildSourceID = source.ID
	selected, err := domaindelivery.ImmutableImageReference(image, "sha256:"+strings.Repeat("0", 64))
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	for _, container := range service.Containers {
		output, err := domaindelivery.ImmutableImageReference(container.ImageRepository, "sha256:"+strings.Repeat("0", 64))
		if err == nil && output == selected {
			return nil
		}
	}
	return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_build_output_mismatch", "Build output must match a selected service container repository; update the service container image repository.", "构建输出与服务容器的镜像仓库不一致，请在服务配置中关联相同的镜像仓库。")
}

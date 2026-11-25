package apphosting

import (
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func validDynamicDataSchema(database bool, dataSchema int) bool {
	if !database {
		return dataSchema == 0
	}
	return dataSchema == 1 || dataSchema == 2
}

func previousDynamicBuildPhase(phase, imageRef string) (string, error) {
	if imageRef != "" && !validDynamicImageRef(imageRef) {
		return "", apperrors.ErrInvalidArgument
	}
	switch phase {
	case "queued":
		return "queued", nil
	case "building":
		return "queued", nil
	case "starting":
		return "building", nil
	default:
		return "", apperrors.ErrInvalidArgument
	}
}

func validDynamicImageRef(imageRef string) bool {
	named, err := reference.ParseNamed(imageRef)
	if err != nil || reference.Domain(named) == "" {
		return false
	}
	digested, ok := named.(reference.Digested)
	return ok && digested.Digest().Algorithm() == "sha256"
}

func hasActiveDynamicClaim(rev domainapphosting.Revision, claimID string, now time.Time) bool {
	return rev.Kind == sohaapi.HostedAppKindDynamic && rev.Status == sohaapi.HostedAppRevisionStatusValidating && hasRevisionClaim(rev, claimID, now)
}

func hasRevisionClaim(rev domainapphosting.Revision, claimID string, now time.Time) bool {
	return claimID != "" && rev.ClaimID == claimID && rev.ClaimExpiresAt.After(now)
}

func dynamicCleanupStatus(rev domainapphosting.Revision) bool {
	return rev.Kind == sohaapi.HostedAppKindDynamic && (rev.Status == sohaapi.HostedAppRevisionStatusFailed || rev.Status == sohaapi.HostedAppRevisionStatusEvicted)
}

func checkRevisionActivation(app domainapphosting.App, rev domainapphosting.Revision, input appapphosting.ActivateRevisionInput) error {
	if rev.Kind != app.Kind {
		return apperrors.ErrConflict
	}
	if rev.Kind != sohaapi.HostedAppKindDynamic {
		if input.DynamicClaimID != "" || rev.Status != sohaapi.HostedAppRevisionStatusUploading {
			return apperrors.ErrConflict
		}
		return nil
	}
	if !hasActiveDynamicClaim(rev, input.DynamicClaimID, time.Now()) || rev.BuildPhase != "starting" || !validDynamicImageRef(rev.ImageRef) {
		return apperrors.ErrConflict
	}
	if strings.TrimSpace(input.ActorUserID) == "" || input.ActorUserID != rev.CreatedBy {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func clearDynamicRevisionWork(rev *domainapphosting.Revision) {
	rev.BuildPhase = ""
	rev.ClaimID = ""
	rev.ClaimExpiresAt = time.Time{}
	rev.NextCheckAt = time.Time{}
}

func canPublishDynamicRevision(app domainapphosting.App, config domainapphosting.RuntimeConfig, actor string, database, hasEnvironment bool) bool {
	return app.IsOwnerOrMaintainer(actor) && ((!database && !hasEnvironment) || config.AllowsSensitivePublisher(app, actor))
}

func revisionActivationRejection(app domainapphosting.App, rev domainapphosting.Revision, input appapphosting.ActivateRevisionInput, config domainapphosting.RuntimeConfig) (string, error) {
	actor := strings.TrimSpace(input.ActorUserID)
	if actor != "" && !canPublishDynamicRevision(app, config, actor, rev.Database, len(rev.RuntimeEnvironment) > 0) {
		return "uploader lost owner or maintainer permission before activation", apperrors.NewBusiness(apperrors.ErrAccessDenied, "publisher_permission_revoked", "uploader is no longer an authorized owner or maintainer of this application", "上传期间您的作者或协作者权限已变更，本次版本激活已取消")
	}
	if !config.ContainsEnvironmentSnapshot(rev.RuntimeEnvironment) {
		return "referenced environment credentials changed before activation", revisionEnvironmentRevoked()
	}
	if app.Status == sohaapi.HostedAppStatusOffline || app.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
		return "application became offline or suspended before activation", apperrors.NewBusiness(apperrors.ErrConflict, "app_not_deployable", "application became offline or suspended before activation", "激活前应用已下线或被管理员封停")
	}
	return "", nil
}

func revisionSuperseded() (string, error) {
	return "a newer revision was already activated", apperrors.NewBusiness(apperrors.ErrConflict, "revision_superseded", "a newer revision has already been activated", "更新版本已激活，本次版本不再切换")
}

func checkRollbackRevisionKind(app domainapphosting.App, rev domainapphosting.Revision) error {
	if app.Kind != rev.Kind {
		return apperrors.NewBusiness(apperrors.ErrConflict, "revision_kind_mismatch", "target revision kind does not match this application", "目标版本类型与应用不符")
	}
	return nil
}

func checkRollbackCandidate(app domainapphosting.App, rev domainapphosting.Revision) error {
	if err := checkRollbackRevisionKind(app, rev); err != nil {
		return err
	}
	if rev.Status != sohaapi.HostedAppRevisionStatusReady || (rev.Kind == sohaapi.HostedAppKindDynamic && !validDynamicImageRef(rev.ImageRef)) {
		return apperrors.NewBusiness(apperrors.ErrConflict, "revision_not_ready", "target revision is not in ready state", "目标版本不可用于回滚")
	}
	return nil
}

func checkRollbackRuntime(app domainapphosting.App, rev domainapphosting.Revision, actor string, config domainapphosting.RuntimeConfig) error {
	if rev.Kind != sohaapi.HostedAppKindDynamic {
		return nil
	}
	if !canPublishDynamicRevision(app, config, actor, rev.Database, len(rev.RuntimeEnvironment) > 0) {
		return apperrors.ErrAccessDenied
	}
	if !config.ContainsEnvironmentSnapshot(rev.RuntimeEnvironment) {
		return revisionEnvironmentRevoked()
	}
	return nil
}

func revisionEnvironmentRevoked() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "revision_environment_revoked", "revision references replaced or removed environment credentials", "版本引用的环境变量凭据已替换或删除，请重新部署")
}

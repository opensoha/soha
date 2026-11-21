package apphosting

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	DefaultMaxBundleCompressedBytes    int64 = 50 << 20
	DefaultMaxBundleUncompressedBytes  int64 = 150 << 20
	DefaultMaxBundleFileCount          int32 = 2000
	DefaultMaxAppsPerOwner             int32 = 20
	DefaultRetainedRevisionsPerApp     int32 = 10
	DefaultPublicScheme                      = "https"
	DefaultStoragePrefix                     = "app-hosting"
	DefaultAllowOrganizationVisibility       = true
	DefaultAuthTicketTTL                     = 30 * time.Second
	DefaultGatewaySessionTTL                 = 8 * time.Hour
)

var (
	ErrPayloadTooLarge = errors.New("app hosting bundle exceeds size or file count limit")
	ErrBundleRejected  = errors.New("app hosting bundle failed security or structure validation")
	ErrQuotaExceeded   = errors.New("app hosting owner quota exceeded")
	ErrVisitorDisabled = errors.New("app hosting visitor account is inactive")
)

type Settings struct {
	Enabled                      bool
	AppsDomain                   string
	PublicScheme                 string
	StorageIntegrationID         string
	StoragePrefix                string
	AllowOrganizationVisibility  bool
	OrganizationVisibilityPolicy sohaapi.AppHostingOrganizationVisibilityPolicy
	PolicyVersion                int64
	MaxBundleCompressedBytes     int64
	MaxBundleUncompressedBytes   int64
	MaxBundleFileCount           int32
	MaxAppsPerOwner              int32
	RetainedRevisionsPerApp      int32
	GatewayTokenSHA256           string
	GatewayTokenRotatedAt        *time.Time
	UpdatedBy                    string
	UpdatedAt                    *time.Time
}

func DefaultSettings() Settings {
	return Settings{
		Enabled:                      false,
		AppsDomain:                   "",
		PublicScheme:                 DefaultPublicScheme,
		StorageIntegrationID:         "",
		StoragePrefix:                DefaultStoragePrefix,
		AllowOrganizationVisibility:  DefaultAllowOrganizationVisibility,
		OrganizationVisibilityPolicy: sohaapi.AppHostingOrganizationVisibilityPolicyAllowDirect,
		PolicyVersion:                1,
		MaxBundleCompressedBytes:     DefaultMaxBundleCompressedBytes,
		MaxBundleUncompressedBytes:   DefaultMaxBundleUncompressedBytes,
		MaxBundleFileCount:           DefaultMaxBundleFileCount,
		MaxAppsPerOwner:              DefaultMaxAppsPerOwner,
		RetainedRevisionsPerApp:      DefaultRetainedRevisionsPerApp,
	}
}

func (s Settings) WithDefaults() Settings {
	out := s
	if strings.TrimSpace(out.PublicScheme) == "" {
		out.PublicScheme = DefaultPublicScheme
	}
	out.StoragePrefix = normalizeStoragePrefix(out.StoragePrefix)
	out.OrganizationVisibilityPolicy, out.AllowOrganizationVisibility = normalizeOrganizationPolicy(out.OrganizationVisibilityPolicy, out.AllowOrganizationVisibility)
	if out.PolicyVersion < 1 {
		out.PolicyVersion = 1
	}
	if out.MaxBundleCompressedBytes <= 0 {
		out.MaxBundleCompressedBytes = DefaultMaxBundleCompressedBytes
	}
	if out.MaxBundleUncompressedBytes <= 0 {
		out.MaxBundleUncompressedBytes = DefaultMaxBundleUncompressedBytes
	}
	if out.MaxBundleFileCount <= 0 {
		out.MaxBundleFileCount = DefaultMaxBundleFileCount
	}
	if out.MaxAppsPerOwner <= 0 {
		out.MaxAppsPerOwner = DefaultMaxAppsPerOwner
	}
	if out.RetainedRevisionsPerApp <= 0 {
		out.RetainedRevisionsPerApp = DefaultRetainedRevisionsPerApp
	}
	out.AppsDomain = NormalizeAppsDomain(out.AppsDomain)
	out.StorageIntegrationID = strings.TrimSpace(out.StorageIntegrationID)
	return out
}

func normalizeOrganizationPolicy(policy sohaapi.AppHostingOrganizationVisibilityPolicy, allowBool bool) (sohaapi.AppHostingOrganizationVisibilityPolicy, bool) {
	if policy == "" {
		if allowBool {
			return sohaapi.AppHostingOrganizationVisibilityPolicyAllowDirect, true
		}
		return sohaapi.AppHostingOrganizationVisibilityPolicyDisabled, false
	}
	return policy, policy != sohaapi.AppHostingOrganizationVisibilityPolicyDisabled
}

func (s Settings) EffectiveOrganizationPolicy() sohaapi.AppHostingOrganizationVisibilityPolicy {
	if !s.AllowOrganizationVisibility || s.OrganizationVisibilityPolicy == sohaapi.AppHostingOrganizationVisibilityPolicyDisabled {
		return sohaapi.AppHostingOrganizationVisibilityPolicyDisabled
	}
	if s.OrganizationVisibilityPolicy == sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval {
		return sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval
	}
	return sohaapi.AppHostingOrganizationVisibilityPolicyAllowDirect
}

func (s Settings) Validate() error {
	scheme := strings.ToLower(strings.TrimSpace(s.PublicScheme))
	if scheme != "https" && scheme != "http" {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_public_scheme", "publicScheme must be https or http", "公开协议必须为 https 或 http")
	}
	if s.OrganizationVisibilityPolicy != "" && !s.OrganizationVisibilityPolicy.Valid() {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_organization_visibility_policy", "organizationVisibilityPolicy must be allow_direct, require_approval, or disabled", "全公司公开策略必须为 allow_direct、require_approval 或 disabled")
	}
	if s.PolicyVersion != 0 && s.PolicyVersion < 1 {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_policy_version", "policyVersion must be at least 1", "策略版本号必须大于等于 1")
	}
	if s.AppsDomain != "" {
		if err := ValidateAppsDomain(s.AppsDomain); err != nil {
			return err
		}
	}
	if strings.Contains(s.StoragePrefix, "..") || strings.ContainsRune(s.StoragePrefix, '\\') || len(s.StoragePrefix) > 256 {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_storage_prefix", "storagePrefix contains invalid path characters", "对象存储前缀格式不合法")
	}
	if len(s.StorageIntegrationID) > 128 {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_storage_integration", "storageIntegrationId exceeds maximum length", "对象存储集成标识过长")
	}
	return s.validateGovernanceQuotas()
}

func (s Settings) validateGovernanceQuotas() error {
	if s.MaxBundleCompressedBytes <= 0 || s.MaxBundleUncompressedBytes <= 0 || s.MaxBundleFileCount <= 0 || s.MaxAppsPerOwner <= 0 || s.RetainedRevisionsPerApp <= 0 {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_governance_limits", "governance limits must be positive integers", "平台配额与包大小限制必须为正整数")
	}
	if s.MaxBundleUncompressedBytes < s.MaxBundleCompressedBytes {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_bundle_limits", "maxBundleUncompressedBytes must be greater than or equal to maxBundleCompressedBytes", "解压上限不能小于压缩包上限")
	}
	if s.Enabled && (s.AppsDomain == "" || s.StorageIntegrationID == "") {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "incomplete_app_hosting_settings", "appsDomain and storageIntegrationId are required when enabling App Hosting", "启用应用托管前必须配置泛域名后缀与对象存储集成")
	}
	return nil
}

func (s Settings) ToAPI(hasFallbackGatewayToken bool) sohaapi.AppHostingSettings {
	normalized := s.WithDefaults()
	var updatedAt *time.Time
	if normalized.UpdatedAt != nil {
		copied := normalized.UpdatedAt.UTC()
		updatedAt = &copied
	}
	return sohaapi.AppHostingSettings{
		Enabled:                      normalized.Enabled,
		AppsDomain:                   normalized.AppsDomain,
		PublicScheme:                 normalized.PublicScheme,
		StorageIntegrationID:         normalized.StorageIntegrationID,
		StoragePrefix:                normalized.StoragePrefix,
		GatewayTokenConfigured:       normalized.GatewayTokenSHA256 != "" || hasFallbackGatewayToken,
		AllowOrganizationVisibility:  normalized.AllowOrganizationVisibility,
		OrganizationVisibilityPolicy: normalized.OrganizationVisibilityPolicy,
		PolicyVersion:                normalized.PolicyVersion,
		MaxBundleCompressedBytes:     normalized.MaxBundleCompressedBytes,
		MaxBundleUncompressedBytes:   normalized.MaxBundleUncompressedBytes,
		MaxBundleFileCount:           normalized.MaxBundleFileCount,
		MaxAppsPerOwner:              normalized.MaxAppsPerOwner,
		RetainedRevisionsPerApp:      normalized.RetainedRevisionsPerApp,
		UpdatedBy:                    normalized.UpdatedBy,
		UpdatedAt:                    updatedAt,
	}
}

func normalizeStoragePrefix(prefix string) string {
	trimmed := strings.Trim(strings.TrimSpace(prefix), "/")
	if trimmed == "" {
		return DefaultStoragePrefix
	}
	return trimmed
}

type App struct {
	ID                 string
	Name               string
	Description        string
	Kind               sohaapi.HostedAppKind
	DNSLabel           string
	SpaFallback        bool
	OwnerUserID        string
	MaintainerUserIDs  []string
	Visibility         sohaapi.HostedAppVisibilityScope
	Status             sohaapi.HostedAppStatus
	SuspendedReason    string
	ActiveRevisionID   string
	RouteGeneration    int64
	IdempotencyKey     string
	RequestFingerprint string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (a App) IsOwner(userID string) bool {
	trimmed := strings.TrimSpace(userID)
	return trimmed != "" && a.OwnerUserID == trimmed
}

func (a App) IsOwnerOrMaintainer(userID string) bool {
	trimmed := strings.TrimSpace(userID)
	if trimmed == "" {
		return false
	}
	return a.OwnerUserID == trimmed || slices.Contains(a.MaintainerUserIDs, trimmed)
}

func (a App) Host(appsDomain string) string {
	domain := NormalizeAppsDomain(appsDomain)
	if domain == "" {
		return a.DNSLabel + ".localhost"
	}
	return a.DNSLabel + "." + domain
}

func (a App) URL(settings Settings) string {
	scheme := strings.ToLower(strings.TrimSpace(settings.PublicScheme))
	if scheme != "http" && scheme != "https" {
		scheme = DefaultPublicScheme
	}
	return scheme + "://" + a.Host(settings.AppsDomain)
}

func (a App) ToAPI(settings Settings) sohaapi.HostedApp {
	maintainers := make([]string, 0, len(a.MaintainerUserIDs))
	for _, id := range a.MaintainerUserIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" && !slices.Contains(maintainers, trimmed) {
			maintainers = append(maintainers, trimmed)
		}
	}
	return sohaapi.HostedApp{
		ID:                a.ID,
		Name:              a.Name,
		Description:       a.Description,
		Kind:              a.Kind,
		DNSLabel:          a.DNSLabel,
		Host:              a.Host(settings.AppsDomain),
		URL:               a.URL(settings),
		SpaFallback:       a.SpaFallback,
		OwnerUserID:       a.OwnerUserID,
		MaintainerUserIDs: maintainers,
		Visibility:        CloneVisibilityScope(a.Visibility),
		Status:            a.Status,
		SuspendedReason:   a.SuspendedReason,
		ActiveRevisionID:  a.ActiveRevisionID,
		RouteGeneration:   a.RouteGeneration,
		CreatedAt:         a.CreatedAt.UTC(),
		UpdatedAt:         a.UpdatedAt.UTC(),
	}
}

type Revision struct {
	ID                   string
	AppID                string
	RevisionNumber       int64
	Description          string
	Kind                 sohaapi.HostedAppKind
	Status               sohaapi.HostedAppRevisionStatus
	BundleSha256         string
	FileCount            int32
	TotalBytes           int64
	SpaFallback          bool
	StorageIntegrationID string
	StoragePrefix        string
	Files                []sohaapi.HostedAppManifestFile
	FailureStage         string
	FailureReason        string
	IdempotencyKey       string
	CreatedBy            string
	CreatedAt            time.Time
	ActivatedAt          *time.Time
}

func (r Revision) ToAPI() sohaapi.HostedAppRevision {
	var activatedAt *time.Time
	if r.ActivatedAt != nil {
		copied := r.ActivatedAt.UTC()
		activatedAt = &copied
	}
	return sohaapi.HostedAppRevision{
		ID:                   r.ID,
		AppID:                r.AppID,
		RevisionNumber:       r.RevisionNumber,
		Description:          r.Description,
		Kind:                 r.Kind,
		Status:               r.Status,
		BundleSha256:         r.BundleSha256,
		FileCount:            r.FileCount,
		TotalBytes:           r.TotalBytes,
		SpaFallback:          r.SpaFallback,
		StorageIntegrationID: r.StorageIntegrationID,
		StoragePrefix:        r.StoragePrefix,
		FailureStage:         r.FailureStage,
		FailureReason:        r.FailureReason,
		IdempotencyKey:       r.IdempotencyKey,
		CreatedBy:            r.CreatedBy,
		CreatedAt:            r.CreatedAt.UTC(),
		ActivatedAt:          activatedAt,
	}
}

type AuthTicket struct {
	TicketSHA256       string
	AppID              string
	DNSLabel           string
	Host               string
	RouteGeneration    int64
	ReturnPath         string
	SourceSessionID    string
	GatewayStateSHA256 string
	UserID             string
	Username           string
	DisplayName        string
	Email              string
	Teams              []string
	ExpiresAt          time.Time
	ConsumedAt         *time.Time
	CreatedAt          time.Time
}

type RollbackIdempotencyRecord struct {
	AppID            string
	IdempotencyKey   string
	TargetRevisionID string
	RouteGeneration  int64
	CreatedAt        time.Time
}

type VisitorProfile struct {
	ID          string
	Username    string
	DisplayName string
	Email       string
	TeamIDs     []string
}

var defaultPairwiseVisitorKey = []byte("soha-apphosting-default-visitor-hmac-v1")

// DerivePairwiseVisitorID returns an app-scoped stable identifier ("ahv_" + 32 hex chars)
// so different hosted apps never share global employee user IDs by default.
func DerivePairwiseVisitorID(secret []byte, appID, userID string) string {
	key := secret
	if len(key) == 0 {
		key = defaultPairwiseVisitorKey
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("soha-apphosting-visitor:v1:"))
	_, _ = mac.Write([]byte(strings.TrimSpace(appID)))
	_, _ = mac.Write([]byte(":"))
	_, _ = mac.Write([]byte(strings.TrimSpace(userID)))
	sum := mac.Sum(nil)
	return "ahv_" + hex.EncodeToString(sum[:16])
}

func (v VisitorProfile) ToPairwiseVisitorIdentity(secret []byte, appID string) sohaapi.HostedAppVisitorIdentity {
	username := strings.TrimSpace(v.Username)
	if username == "" {
		username = strings.TrimSpace(v.ID)
	}
	displayName := strings.TrimSpace(v.DisplayName)
	if displayName == "" {
		displayName = username
	}
	return sohaapi.HostedAppVisitorIdentity{
		ID:          DerivePairwiseVisitorID(secret, appID, v.ID),
		DisplayName: displayName,
	}
}

type VisibilityApproval struct {
	ID                  string
	AppID               string
	AppName             string
	AppDNSLabel         string
	RequesterUserID     string
	CurrentVisibility   sohaapi.HostedAppVisibilityScope
	RequestedVisibility sohaapi.HostedAppVisibilityScope
	BaseRouteGeneration int64
	PolicyVersion       int64
	Status              sohaapi.HostedAppVisibilityApprovalStatus
	Reason              string
	ReviewerUserID      string
	ReviewComment       string
	InvalidationReason  string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DecidedAt           *time.Time
}

func (v VisibilityApproval) ToAPI(requesterProfile *sohaapi.HostedAppUserSummary) sohaapi.HostedAppVisibilityApproval {
	var decidedAt *time.Time
	if v.DecidedAt != nil {
		copied := v.DecidedAt.UTC()
		decidedAt = &copied
	}
	var profileCopy *sohaapi.HostedAppUserSummary
	if requesterProfile != nil {
		cloned := CloneUserSummary(*requesterProfile)
		profileCopy = &cloned
	}
	return sohaapi.HostedAppVisibilityApproval{
		ID:                  v.ID,
		AppID:               v.AppID,
		AppName:             v.AppName,
		AppDNSLabel:         v.AppDNSLabel,
		RequesterUserID:     v.RequesterUserID,
		RequesterProfile:    profileCopy,
		CurrentVisibility:   CloneVisibilityScope(v.CurrentVisibility),
		RequestedVisibility: CloneVisibilityScope(v.RequestedVisibility),
		BaseRouteGeneration: v.BaseRouteGeneration,
		PolicyVersion:       v.PolicyVersion,
		Status:              v.Status,
		Reason:              v.Reason,
		ReviewerUserID:      v.ReviewerUserID,
		ReviewComment:       v.ReviewComment,
		InvalidationReason:  v.InvalidationReason,
		CreatedAt:           v.CreatedAt.UTC(),
		UpdatedAt:           v.UpdatedAt.UTC(),
		DecidedAt:           decidedAt,
	}
}

func CloneUserSummary(summary sohaapi.HostedAppUserSummary) sohaapi.HostedAppUserSummary {
	teamNames := make([]string, 0, len(summary.TeamNames))
	for _, name := range summary.TeamNames {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			teamNames = append(teamNames, trimmed)
		}
	}
	return sohaapi.HostedAppUserSummary{
		UserID:      strings.TrimSpace(summary.UserID),
		DisplayName: firstDisplayString(summary.DisplayName, summary.UserID),
		AvatarURL:   strings.TrimSpace(summary.AvatarURL),
		TeamNames:   teamNames,
	}
}

func firstDisplayString(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return "Unknown"
}

type AppListFilter struct {
	Scope             sohaapi.HostedAppListScope
	MemberUserID      string
	OwnerUserID       string
	TeamID            string
	TeamMemberUserIDs []string
	TeamFilterActive  bool
	Query             string
	Status            sohaapi.HostedAppStatus
	Limit             int
	Offset            int
}

type VisibilityApprovalFilter struct {
	Scope           sohaapi.HostedAppApprovalListScope
	RequesterUserID string
	Status          sohaapi.HostedAppVisibilityApprovalStatus
	AppID           string
	Limit           int
	Offset          int
}

type UploadRevisionInput struct {
	IdempotencyKey string
	SpaFallback    *bool
	Description    string
}

type BrowserAuthStartInput struct {
	Host            string
	ReturnPath      string
	GatewayState    string
	SourceSessionID string
}

type BrowserAuthStartResult struct {
	LoginRequired bool
	RedirectURL   string
}

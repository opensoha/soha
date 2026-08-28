package software

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const MaxPackageBytes int64 = 4 << 30

var identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type Package struct {
	ID                   string    `json:"id"`
	SoftwareID           string    `json:"softwareId"`
	Name                 string    `json:"name"`
	Description          string    `json:"description,omitempty"`
	Publisher            string    `json:"publisher"`
	Category             string    `json:"category,omitempty"`
	TenantID             string    `json:"tenantId,omitempty"`
	WorkspaceID          string    `json:"workspaceId,omitempty"`
	Visibility           string    `json:"visibility,omitempty"`
	Status               string    `json:"status,omitempty"`
	Version              string    `json:"version"`
	Platform             string    `json:"platform"`
	Arch                 string    `json:"arch"`
	FileName             string    `json:"fileName"`
	SizeBytes            int64     `json:"sizeBytes"`
	SHA256               string    `json:"sha256"`
	DownloadPath         string    `json:"downloadPath"`
	DownloadCount        int64     `json:"downloadCount"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
	StorageIntegrationID string    `json:"storageIntegrationId,omitempty"`
	ObjectKey            string    `json:"-"`
}

type UploadInput struct {
	StorageIntegrationID string
	SoftwareID           string
	Name                 string
	Description          string
	Publisher            string
	Category             string
	TenantID             string
	WorkspaceID          string
	Visibility           string
	Version              string
	Platform             string
	Arch                 string
	FileName             string
}

type URLImportInput struct {
	UploadInput
	URL string
}

type Filter struct {
	StorageIntegrationID string
	Platform             string
	Arch                 string
	Cursor               string
	Limit                int
}

type Storage struct {
	Backend       string     `json:"backend"`
	IntegrationID string     `json:"integrationId,omitempty"`
	ProviderType  string     `json:"providerType,omitempty"`
	Endpoint      string     `json:"endpoint,omitempty"`
	Bucket        string     `json:"bucket,omitempty"`
	Region        string     `json:"region,omitempty"`
	HealthStatus  string     `json:"healthStatus,omitempty"`
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
	ObjectCount   int64      `json:"objectCount"`
	TotalBytes    int64      `json:"totalBytes"`
	Items         []Package  `json:"items"`
	NextCursor    string     `json:"nextCursor,omitempty"`
}

type StorageBackend struct {
	IntegrationID string
	ProviderType  string
	Endpoint      string
	Bucket        string
	Region        string
	HealthStatus  string
	LastCheckedAt *time.Time
}

type BlobStore interface {
	Active(context.Context, string) (StorageBackend, error)
	Put(context.Context, string, string, io.Reader) (int64, string, error)
	Open(context.Context, string, string) (io.ReadCloser, error)
	Delete(context.Context, string, string) error
}

type RemoteFile struct {
	FileName string
	Content  io.ReadCloser
}

type Store interface {
	List(context.Context, Filter) ([]Package, string, error)
	Storage(context.Context, string, string, int) (Storage, error)
	Create(context.Context, UploadInput, io.Reader) (Package, error)
	Open(context.Context, string) (Package, io.ReadCloser, error)
	IncrementDownloadCount(context.Context, string) error
	Delete(context.Context, string) error
}

type URLFetcher interface {
	Fetch(context.Context, string) (RemoteFile, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
	ListAuthorized(context.Context, domainidentity.Principal, domainaudit.Filter) ([]domainaudit.Entry, error)
}

type DownloadRecord struct {
	ID           string    `json:"id"`
	ActorID      string    `json:"actorId"`
	ActorName    string    `json:"actorName,omitempty"`
	DownloadedAt time.Time `json:"downloadedAt"`
	DurationMS   int64     `json:"durationMs"`
	SourceIP     string    `json:"sourceIp,omitempty"`
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	store       Store
	fetcher     URLFetcher
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
}

func New(store Store, fetcher URLFetcher, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder) *Service {
	return &Service{store: store, fetcher: fetcher, permissions: permissions, audit: audit, operations: operations}
}

func (s *Service) Storage(ctx context.Context, principal domainidentity.Principal, storageIntegrationID, cursor string, limit int) (Storage, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSoftwarePackageView); err != nil {
		return Storage{}, err
	}
	storageIntegrationID = strings.TrimSpace(storageIntegrationID)
	cursor = strings.TrimSpace(cursor)
	if limit == 0 {
		limit = 50
	}
	if len(storageIntegrationID) > 128 || len(cursor) > 256 || limit < 1 || limit > 200 {
		return Storage{}, fmt.Errorf("%w: invalid software storage page", apperrors.ErrInvalidArgument)
	}
	return s.store.Storage(ctx, storageIntegrationID, cursor, limit)
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, filter Filter) ([]Package, string, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSoftwarePackageView); err != nil {
		return nil, "", err
	}
	filter.Platform = strings.TrimSpace(filter.Platform)
	filter.Arch = strings.TrimSpace(filter.Arch)
	filter.StorageIntegrationID = strings.TrimSpace(filter.StorageIntegrationID)
	filter.Cursor = strings.TrimSpace(filter.Cursor)
	if len(filter.StorageIntegrationID) > 128 || filter.Platform != "" && !identifierPattern.MatchString(filter.Platform) || filter.Arch != "" && !identifierPattern.MatchString(filter.Arch) {
		return nil, "", fmt.Errorf("%w: invalid software package filter", apperrors.ErrInvalidArgument)
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit < 1 || filter.Limit > 200 {
		return nil, "", fmt.Errorf("%w: limit must be between 1 and 200", apperrors.ErrInvalidArgument)
	}
	return s.store.List(ctx, filter)
}

func (s *Service) Upload(ctx context.Context, principal domainidentity.Principal, input UploadInput, content io.Reader) (Package, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSoftwarePackageCreate); err != nil {
		return Package{}, err
	}
	return s.create(ctx, principal, input, content, "Uploaded software package", "upload")
}

func (s *Service) ImportURL(ctx context.Context, principal domainidentity.Principal, input URLImportInput) (Package, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSoftwarePackageCreate); err != nil {
		return Package{}, err
	}
	input.UploadInput = normalizeUpload(input.UploadInput)
	input.URL = strings.TrimSpace(input.URL)
	if input.URL == "" || len(input.URL) > 2048 {
		return Package{}, fmt.Errorf("%w: installer URL is required", apperrors.ErrInvalidArgument)
	}
	if err := validateUploadMetadata(input.UploadInput, false); err != nil {
		return Package{}, err
	}
	if s.fetcher == nil {
		return Package{}, fmt.Errorf("software package URL importer is unavailable")
	}
	remote, err := s.fetcher.Fetch(ctx, input.URL)
	if err != nil {
		return Package{}, err
	}
	if remote.Content == nil {
		return Package{}, fmt.Errorf("software package URL importer returned no content")
	}
	defer remote.Content.Close()
	if input.FileName == "" {
		input.FileName = remote.FileName
	}
	return s.create(ctx, principal, input.UploadInput, remote.Content, "Imported software package from URL", "url")
}

func (s *Service) create(ctx context.Context, principal domainidentity.Principal, input UploadInput, content io.Reader, summary, source string) (Package, error) {
	input = normalizeUpload(input)
	if err := validateUpload(input, content); err != nil {
		return Package{}, err
	}
	item, err := s.store.Create(ctx, input, content)
	if err != nil {
		return Package{}, err
	}
	s.recordAudit(ctx, principal, "create", item.ID, item.Name, summary, map[string]any{
		"softwareId": item.SoftwareID, "version": item.Version, "sha256": item.SHA256, "source": source,
	})
	return item, nil
}

func (s *Service) Open(ctx context.Context, principal domainidentity.Principal, id string) (Package, io.ReadCloser, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSoftwarePackageView); err != nil {
		return Package{}, nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 {
		return Package{}, nil, fmt.Errorf("%w: software package id is required", apperrors.ErrInvalidArgument)
	}
	return s.store.Open(ctx, id)
}

func (s *Service) CompleteDownload(ctx context.Context, principal domainidentity.Principal, item Package, bytesSent int64, duration time.Duration, transferErr error) error {
	request := requestctx.FromContext(ctx)
	result := "failed"
	summary := "Software package download failed"
	var countErr error
	if transferErr == nil && bytesSent == item.SizeBytes {
		result = "success"
		summary = "Downloaded software package"
		detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		countErr = s.store.IncrementDownloadCount(detached, item.ID)
		cancel()
	}
	if s.audit == nil {
		return countErr
	}
	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	auditErr := s.audit.Record(detached, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: "SoftwarePackage", ResourceName: item.ID, Action: "download", Result: result,
		Summary: summary, RequestPath: request.Path, RequestMethod: request.Method,
		RequestID: request.RequestID, SourceIP: request.SourceIP,
		Metadata: map[string]any{"packageId": item.ID, "durationMs": float64(max(duration.Milliseconds(), 0)), "bytesSent": bytesSent},
	})
	return errors.Join(countErr, auditErr)
}

func (s *Service) DownloadRecords(ctx context.Context, principal domainidentity.Principal, id string, limit int) ([]DownloadRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 {
		return nil, fmt.Errorf("%w: software package id is required", apperrors.ErrInvalidArgument)
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 100", apperrors.ErrInvalidArgument)
	}
	if s.audit == nil {
		return nil, fmt.Errorf("software download audit is unavailable")
	}
	entries, err := s.audit.ListAuthorized(ctx, principal, domainaudit.Filter{
		ResourceKind: "SoftwarePackage", ResourceName: id, Action: "download", Result: "success", Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	records := make([]DownloadRecord, 0, len(entries))
	for _, entry := range entries {
		duration, _ := entry.Metadata["durationMs"].(float64)
		records = append(records, DownloadRecord{
			ID: entry.ID, ActorID: entry.ActorID, ActorName: entry.ActorName, DownloadedAt: entry.CreatedAt,
			DurationMS: int64(duration), SourceIP: entry.SourceIP,
		})
	}
	return records, nil
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermSoftwarePackageDelete); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 {
		return fmt.Errorf("%w: software package id is required", apperrors.ErrInvalidArgument)
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.recordAudit(ctx, principal, "delete", id, id, "Deleted software package", nil)
	return nil
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permission string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permission)
}

func normalizeUpload(input UploadInput) UploadInput {
	input.StorageIntegrationID = strings.TrimSpace(input.StorageIntegrationID)
	input.SoftwareID = strings.TrimSpace(input.SoftwareID)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Publisher = strings.TrimSpace(input.Publisher)
	input.Category = strings.TrimSpace(input.Category)
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.Visibility = strings.ToLower(strings.TrimSpace(input.Visibility))
	if input.TenantID == "" {
		input.TenantID = "default"
	}
	if input.WorkspaceID == "" {
		input.WorkspaceID = "default"
	}
	if input.Visibility == "" {
		input.Visibility = "workspace"
	}
	input.Version = strings.TrimSpace(input.Version)
	input.Platform = strings.TrimSpace(input.Platform)
	input.Arch = strings.TrimSpace(input.Arch)
	input.FileName = strings.TrimSpace(input.FileName)
	return input
}

func validateUpload(input UploadInput, content io.Reader) error {
	if content == nil {
		return fmt.Errorf("%w: installer file is required", apperrors.ErrInvalidArgument)
	}
	return validateUploadMetadata(input, true)
}

func validateUploadMetadata(input UploadInput, requireFileName bool) error {
	if len(input.StorageIntegrationID) > 128 {
		return fmt.Errorf("%w: invalid storage integration id", apperrors.ErrInvalidArgument)
	}
	if !identifierPattern.MatchString(input.SoftwareID) || !identifierPattern.MatchString(input.Platform) || !identifierPattern.MatchString(input.Arch) {
		return fmt.Errorf("%w: invalid software, platform, or architecture identifier", apperrors.ErrInvalidArgument)
	}
	if !validText(input.Name, 100, true) || !validText(input.Publisher, 100, true) || !validText(input.Version, 64, true) || !validText(input.Description, 500, false) || !validText(input.Category, 50, false) {
		return fmt.Errorf("%w: invalid software package metadata", apperrors.ErrInvalidArgument)
	}
	if input.TenantID != "default" || input.WorkspaceID != "default" || input.Visibility != "workspace" {
		return fmt.Errorf("%w: invalid software package visibility", apperrors.ErrInvalidArgument)
	}
	if !validText(input.FileName, 255, requireFileName) || input.FileName != "" && (filepath.Base(input.FileName) != input.FileName || strings.ContainsAny(input.FileName, `/\\`)) {
		return fmt.Errorf("%w: invalid installer file name", apperrors.ErrInvalidArgument)
	}
	if input.FileName == "" {
		return nil
	}
	allowed := map[string]map[string]bool{
		"darwin":  {".dmg": true, ".pkg": true},
		"windows": {".exe": true, ".msi": true},
		"linux":   {".appimage": true, ".deb": true, ".rpm": true},
	}
	if !allowed[input.Platform][strings.ToLower(filepath.Ext(input.FileName))] {
		return fmt.Errorf("%w: installer type does not match platform", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validText(value string, max int, required bool) bool {
	length := utf8.RuneCountInString(value)
	return (!required || length > 0) && length <= max
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, action, id, name, summary string, metadata map[string]any) {
	request := requestctx.FromContext(ctx)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["packageId"] = id
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{
			ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
			ResourceKind: "SoftwarePackage", ResourceName: name, Action: action, Result: "success", Summary: summary,
			RequestPath: request.Path, RequestMethod: request.Method, RequestID: request.RequestID, SourceIP: request.SourceIP,
			Metadata: metadata,
		})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(ctx, principal, "software.package."+action, map[string]any{
			"module": "software", "resourceKind": "SoftwarePackage", "targetId": id, "targetLabel": name,
		}, "success", summary, metadata))
	}
}

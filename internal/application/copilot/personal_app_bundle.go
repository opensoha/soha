package copilot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/opensoha/soha/contracts/apphosting/sourceignore"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	maxWorkspaceBundleUncompressedBytes int64 = 160 << 20
	maxWorkspaceBundleFileCount               = 2500
	maxArtifactContentBytes                   = 8 << 20
	inMemoryArchiveThreshold            int64 = 2 << 20
)

var skippedWorkspaceNoiseSegments = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"__pycache__":  {},
	".venv":        {},
	".ds_store":    {},
}

var allowedArtifactEntryExts = map[string]struct{}{
	".html": {}, ".htm": {}, ".md": {}, ".json": {}, ".txt": {},
	".yaml": {}, ".yml": {}, ".csv": {}, ".xml": {}, ".svg": {},
	".js": {}, ".css": {},
}

type bundledStaticEntry struct {
	Path string
	Data []byte
}

func (s *Service) BundlePersonalWorkspace(
	ctx context.Context,
	principal domainidentity.Principal,
	sourcePath, assistantID string,
	envVersion int64,
	kind sohaapi.HostedAppKind,
) (io.ReadCloser, error) {
	if s == nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	if strings.TrimSpace(principal.UserID) == "" {
		return nil, apperrors.ErrAccessDenied
	}
	cleanSubpath, err := normalizeWorkspaceSourcePath(sourcePath)
	if err != nil {
		return nil, err
	}
	item, err := s.personalEnvironmentForChange(ctx, principal, "reconcile", "", "en-US")
	if err != nil {
		return nil, err
	}
	if err := verifyWorkspacePublishPreconditions(item, assistantID, envVersion); err != nil {
		return nil, err
	}
	if item.Backend == "vm" {
		return s.bundleVMWorkspaceSingleFile(ctx, principal, item, cleanSubpath)
	}
	return s.bundleContainerWorkspace(ctx, principal, item, cleanSubpath, kind == sohaapi.HostedAppKindDynamic)
}

func verifyWorkspacePublishPreconditions(item domain.PersonalEnvironmentRecord, assistantID string, envVersion int64) error {
	if trimmedAsst := strings.TrimSpace(assistantID); trimmedAsst != "" && item.AssistantID != trimmedAsst {
		return apperrors.NewBusiness(apperrors.ErrConflict, "assistant_mismatch", "assistantId does not match caller's personal assistant", "个人助手标识与当前账号不匹配")
	}
	if envVersion > 0 && item.Version != envVersion {
		return apperrors.NewBusiness(apperrors.ErrConflict, "environment_version_conflict", "personal environment version has changed; refresh and retry", "个人工作空间状态已更新，请刷新后重试")
	}
	if item.ActiveTaskID != "" || (item.OperationUntil != nil && item.OperationUntil.After(time.Now().UTC())) {
		return apperrors.NewBusiness(apperrors.ErrConflict, "workspace_busy", "personal workspace has an active task; wait for it to finish before publishing", "个人工作空间当前有正在执行的任务，请等待任务完成后再发布")
	}
	if !item.DataInitialized {
		return apperrors.NewBusiness(apperrors.ErrNotFound, "workspace_empty", "personal workspace has not been initialized yet", "个人工作空间尚未生成任何文件")
	}
	return nil
}

func (s *Service) bundleVMWorkspaceSingleFile(
	ctx context.Context,
	principal domainidentity.Principal,
	item domain.PersonalEnvironmentRecord,
	cleanSubpath string,
) (io.ReadCloser, error) {
	if cleanSubpath == "" || path.Ext(cleanSubpath) == "" {
		return nil, apperrors.NewBusiness(
			apperrors.ErrInvalidArgument,
			"vm_directory_bundle_unsupported",
			"VM personal workspace only supports publishing a single file path (e.g. index.html); use container workspace or soha host deploy for multi-file directories",
			"虚拟机个人工作空间当前仅支持按单文件路径（如 index.html）直接发布；多文件目录请使用容器工作空间或 soha host deploy",
		)
	}
	if item.State == "sleeping" {
		if _, err := s.ChangePersonalEnvironment(ctx, principal, "wake", "", "en-US"); err != nil {
			return nil, err
		}
	}
	fileView, err := s.ReadPersonalEnvironmentFile(ctx, principal, cleanSubpath)
	if err != nil {
		return nil, err
	}
	entries := buildSingleFileStaticEntries(path.Base(cleanSubpath), path.Base(cleanSubpath), "", []byte(fileView.Content))
	return packDeterministicTarGzReader(entries)
}

func (s *Service) bundleContainerWorkspace(
	ctx context.Context,
	principal domainidentity.Principal,
	item domain.PersonalEnvironmentRecord,
	cleanSubpath string,
	dynamic bool,
) (io.ReadCloser, error) {
	exportTargetVersion, err := s.ensureContainerSleepingForExport(ctx, principal, item)
	if err != nil {
		return nil, err
	}

	rawTar, err := openUnlinkedTempFile("soha-personal-app-export-*")
	if err != nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	defer func() { _ = rawTar.Close() }()

	exportRes, err := s.ExportPersonalEnvironmentData(ctx, principal, sohaapi.PersonalEnvironmentDataExportRequest{
		AssistantID:        item.AssistantID,
		EnvironmentVersion: exportTargetVersion,
	}, "en-US", rawTar)
	if err != nil {
		return nil, err
	}
	size, err := rawTar.Seek(0, io.SeekEnd)
	if err != nil || size != exportRes.SizeBytes {
		return nil, apperrors.ErrServiceUnavailable
	}
	if _, err := rawTar.Seek(0, io.SeekStart); err != nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	var ignores *sourceignore.Rules
	if dynamic {
		ignores, err = readWorkspaceSourceIgnores(rawTar, cleanSubpath)
		if err != nil {
			return nil, err
		}
	}
	return repackExportedWorkspaceTar(rawTar, cleanSubpath, ignores)
}

func (s *Service) ensureContainerSleepingForExport(
	ctx context.Context,
	principal domainidentity.Principal,
	item domain.PersonalEnvironmentRecord,
) (int64, error) {
	current := item
	if personalEnvironmentView(current).State == "unknown" {
		if _, err := s.ChangePersonalEnvironment(ctx, principal, "reconcile", "", "en-US"); err != nil {
			return 0, err
		}
		reloaded, err := s.personalEnvironmentForChange(ctx, principal, "reconcile", "", "en-US")
		if err != nil {
			return 0, err
		}
		current = reloaded
	}
	if current.ActiveTaskID != "" {
		return 0, apperrors.NewBusiness(apperrors.ErrConflict, "workspace_busy", "personal workspace has an active task; wait for it to finish before publishing", "个人工作空间当前有正在执行的任务，请等待任务完成后再发布")
	}
	if current.State == "sleeping" {
		return current.Version, nil
	}
	if current.State == "ready" || current.State == "quota_exceeded" {
		slept, err := s.ChangePersonalEnvironment(ctx, principal, "sleep", "", "en-US")
		if err != nil {
			return 0, err
		}
		return slept.Version, nil
	}
	return 0, apperrors.NewBusiness(
		apperrors.ErrConflict,
		"workspace_not_ready",
		fmt.Sprintf("personal workspace cannot be exported from state %q", current.State),
		"当前个人工作空间状态不支持导出发布",
	)
}

func repackExportedWorkspaceTar(rawTar io.Reader, cleanSubpath string, ignores *sourceignore.Rules) (io.ReadCloser, error) {
	tr := tar.NewReader(rawTar)
	var files []bundledStaticEntry
	var totalBytes int64
	matchedDir := cleanSubpath == ""
	var exactFile *bundledStaticEntry

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "invalid_workspace_export", "failed reading exported workspace archive", "读取工作空间导出归档失败")
		}
		if hdr.Typeflag == tar.TypeXHeader || hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		entryPath, isRootDir, err := sanitizeExportTarPath(hdr.Name, hdr.Typeflag == tar.TypeDir)
		if err != nil {
			return nil, err
		}
		if isRootDir {
			continue
		}
		relPath, inScope, exactMatch := matchWorkspaceSubpath(entryPath, cleanSubpath)
		if !inScope || skipWorkspaceBundleEntry(relPath, hdr.Typeflag == tar.TypeDir, ignores) {
			continue
		}
		if err := validateWorkspaceBundleEntry(entryPath, relPath, hdr.Typeflag, ignores != nil); err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			matchedDir = true
			continue
		}
		if len(files)+1 > maxWorkspaceBundleFileCount {
			return nil, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "workspace_too_many_files", "selected workspace path exceeds maximum file count", "所选工作空间目录文件数量超过上限")
		}
		data, err := readBoundedArchiveEntry(tr, hdr.Size, maxWorkspaceBundleUncompressedBytes-totalBytes)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(data))
		if exactMatch {
			item := bundledStaticEntry{Path: path.Base(entryPath), Data: data}
			exactFile = &item
		} else {
			matchedDir = true
			files = append(files, bundledStaticEntry{Path: relPath, Data: data})
		}
	}

	if exactFile != nil && !matchedDir {
		entries := buildSingleFileStaticEntries(exactFile.Path, exactFile.Path, "", exactFile.Data)
		return packDeterministicTarGzReader(entries)
	}
	if len(files) == 0 {
		return nil, apperrors.NewBusiness(apperrors.ErrNotFound, "workspace_path_not_found", fmt.Sprintf("workspace path %q contains no publishable files", cleanSubpath), "个人工作空间指定路径下未找到可发布文件")
	}
	return packDeterministicTarGzReader(files)
}

func validateWorkspaceBundleEntry(entryPath, relativePath string, typeFlag byte, dynamic bool) error {
	if dynamic && len(relativePath) > sourceignore.MaxPathBytes {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_workspace_archive_path",
			"workspace source path exceeds the hosting limit", "工作空间源码路径超出托管上限")
	}
	switch typeFlag {
	case tar.TypeDir, tar.TypeReg:
		return nil
	default:
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "unsupported_workspace_entry", fmt.Sprintf("workspace path %q uses a disallowed non-regular entry (such as a symlink)", entryPath), fmt.Sprintf("工作空间路径 %q 包含不允许的符号链接或特殊文件", entryPath))
	}
}

func sanitizeExportTarPath(raw string, isDir bool) (string, bool, error) {
	trimmed := strings.TrimSuffix(raw, "/")
	if trimmed == "" || trimmed == "." {
		if isDir {
			return "", true, nil
		}
		return "", false, apperrors.ErrInvalidArgument
	}
	if strings.HasPrefix(trimmed, "/") || strings.ContainsRune(trimmed, '\\') || hasWorkspaceControlRune(trimmed) {
		return "", false, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_workspace_archive_path", "exported workspace archive contains unsafe path", "工作空间归档包含非法路径")
	}
	trimmed = strings.TrimPrefix(trimmed, "./")
	if trimmed == "" || trimmed == "." {
		if isDir {
			return "", true, nil
		}
		return "", false, apperrors.ErrInvalidArgument
	}
	parts := strings.Split(trimmed, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_workspace_archive_path", "exported workspace archive contains traversal segment", "工作空间归档包含路径穿越段")
		}
	}
	return strings.Join(parts, "/"), false, nil
}

func matchWorkspaceSubpath(entryPath, cleanSubpath string) (string, bool, bool) {
	if cleanSubpath == "" {
		return entryPath, true, false
	}
	if entryPath == cleanSubpath {
		return path.Base(entryPath), true, true
	}
	prefix := cleanSubpath + "/"
	if strings.HasPrefix(entryPath, prefix) {
		return strings.TrimPrefix(entryPath, prefix), true, false
	}
	return "", false, false
}

func shouldSkipWorkspacePath(relPath string) bool {
	for _, seg := range strings.Split(relPath, "/") {
		lower := strings.ToLower(seg)
		if _, skip := skippedWorkspaceNoiseSegments[lower]; skip {
			return true
		}
		if strings.HasPrefix(lower, ".soha-") {
			return true
		}
	}
	return false
}

func readBoundedArchiveEntry(tr *tar.Reader, declaredSize, remaining int64) ([]byte, error) {
	if declaredSize < 0 || declaredSize > remaining {
		return nil, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "workspace_bundle_too_large", "selected workspace path exceeds maximum uncompressed size", "所选工作空间目录解压后大小超过上限")
	}
	data, err := io.ReadAll(io.LimitReader(tr, remaining+1))
	if err != nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	if int64(len(data)) > remaining {
		return nil, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "workspace_bundle_too_large", "selected workspace path exceeds maximum uncompressed size", "所选工作空间目录解压后大小超过上限")
	}
	return data, nil
}

func (s *Service) BundleConversationArtifact(
	ctx context.Context,
	principal domainidentity.Principal,
	sessionID, artifactID, entryFileName string,
) (io.ReadCloser, string, error) {
	if s == nil {
		return nil, "", apperrors.ErrServiceUnavailable
	}
	if strings.TrimSpace(principal.UserID) == "" {
		return nil, "", apperrors.ErrAccessDenied
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return nil, "", err
	}
	trimmedArtifactID := strings.TrimSpace(artifactID)
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedArtifactID == "" || len(trimmedArtifactID) > 128 || len(trimmedSessionID) > 128 {
		return nil, "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_artifact_ref", "artifactId is required and must be at most 128 characters", "对话成果标识无效")
	}
	cleanEntry, err := normalizeArtifactEntryFileName(entryFileName)
	if err != nil {
		return nil, "", err
	}

	artifact, err := s.resolveAuthorizedConversationArtifact(ctx, principal, trimmedSessionID, trimmedArtifactID)
	if err != nil {
		return nil, "", err
	}
	content := stringValue(artifact.DataSourceSnapshot["content"])
	if strings.TrimSpace(content) == "" || !utf8.ValidString(content) || len(content) > maxArtifactContentBytes {
		return nil, "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "empty_artifact_content", "conversation artifact does not contain publishable static content", "该对话成果不包含可发布的静态内容")
	}

	title := strings.TrimSpace(artifact.Title)
	if title == "" {
		title = "Conversation Artifact"
	}
	format := strings.ToLower(strings.TrimSpace(stringValue(artifact.DataSourceSnapshot["format"])))
	entries := buildSingleFileStaticEntries(cleanEntry, title, format, []byte(content))
	rc, err := packDeterministicTarGzReader(entries)
	if err != nil {
		return nil, "", err
	}
	return rc, title, nil
}

func (s *Service) resolveAuthorizedConversationArtifact(
	ctx context.Context,
	principal domainidentity.Principal,
	sessionID, artifactID string,
) (domain.AnalysisArtifact, error) {
	if sessionID != "" {
		return s.findArtifactInOwnedSession(ctx, principal, sessionID, artifactID)
	}
	items, err := s.ListPersonalAssistantArtifacts(ctx, principal)
	if err != nil {
		return domain.AnalysisArtifact{}, err
	}
	for _, item := range items {
		if item.ID == artifactID || matchesAnalysisArtifactRef(item.Artifact, artifactID) {
			return item.Artifact, nil
		}
	}
	return domain.AnalysisArtifact{}, apperrors.NewBusiness(apperrors.ErrNotFound, "artifact_not_found", "conversation artifact was not found or access was revoked", "未找到该对话成果或关联知识权限已变更")
}

func (s *Service) findArtifactInOwnedSession(
	ctx context.Context,
	principal domainidentity.Principal,
	sessionID, artifactID string,
) (domain.AnalysisArtifact, error) {
	session, err := s.ownedSession(ctx, principal, sessionID)
	if err != nil {
		return domain.AnalysisArtifact{}, err
	}
	if strings.TrimSpace(session.CreatedBy) != "" && session.CreatedBy != strings.TrimSpace(principal.UserID) {
		return domain.AnalysisArtifact{}, apperrors.ErrAccessDenied
	}
	messages, err := s.messages.ListMessages(ctx, session.ID, 100)
	if err != nil {
		return domain.AnalysisArtifact{}, err
	}
	for _, msg := range messages {
		if matched, ok := matchArtifactInMessage(msg, artifactID); ok {
			if _, err := s.validateMessageKnowledge(ctx, principal, msg); err != nil {
				return domain.AnalysisArtifact{}, apperrors.ErrAccessDenied
			}
			return matched, nil
		}
	}
	runs, err := s.agentRuns.ListAgentRuns(ctx, domain.AgentRunFilter{
		CreatedBy: principal.UserID,
		SessionID: session.ID,
		Limit:     50,
	})
	if err != nil {
		return domain.AnalysisArtifact{}, err
	}
	for _, run := range runs {
		visible := s.visibleKnowledgeAgentRun(ctx, principal, run)
		for idx, art := range visible.AnalysisArtifacts {
			if matchesAnalysisArtifactRef(art, artifactID) || fmt.Sprintf("task:%s:%d", run.ID, idx) == artifactID {
				return art, nil
			}
		}
	}
	return domain.AnalysisArtifact{}, apperrors.NewBusiness(apperrors.ErrNotFound, "artifact_not_found", "conversation artifact was not found in the specified session", "在当前会话中未找到指定的对话成果")
}

func matchArtifactInMessage(msg domain.Message, artifactID string) (domain.AnalysisArtifact, bool) {
	var artifacts []domain.AnalysisArtifact
	if !decodeStructuredValue(msg.Metadata["analysisArtifacts"], &artifacts) {
		return domain.AnalysisArtifact{}, false
	}
	taskID := strings.TrimSpace(stringValue(msg.Metadata["agentRunId"]))
	for idx, art := range artifacts {
		if matchesAnalysisArtifactRef(art, artifactID) ||
			fmt.Sprintf("message:%s:%d", msg.ID, idx) == artifactID ||
			(taskID != "" && fmt.Sprintf("task:%s:%d", taskID, idx) == artifactID) {
			return art, true
		}
	}
	return domain.AnalysisArtifact{}, false
}

func matchesAnalysisArtifactRef(art domain.AnalysisArtifact, artifactID string) bool {
	if art.RunID == artifactID {
		return true
	}
	snapID := strings.TrimSpace(stringValue(art.DataSourceSnapshot["artifactId"]))
	return snapID != "" && snapID == artifactID
}

func normalizeWorkspaceSourcePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "." || trimmed == "./" {
		return "", nil
	}
	if len(trimmed) > 512 || !utf8.ValidString(trimmed) || strings.HasPrefix(trimmed, "/") || strings.ContainsRune(trimmed, '\\') || hasWorkspaceControlRune(trimmed) {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_workspace_path", "sourcePath must be a clean relative path under /data", "工作空间路径必须为相对路径，且禁止包含路径穿越或控制字符")
	}
	trimmed = strings.TrimSuffix(strings.TrimPrefix(trimmed, "./"), "/")
	if trimmed == "" || trimmed == "." {
		return "", nil
	}
	parts := strings.Split(trimmed, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_workspace_path", "sourcePath contains disallowed traversal or empty path segment", "工作空间路径包含非法的路径穿越或空路径段")
		}
	}
	return strings.Join(parts, "/"), nil
}

func normalizeArtifactEntryFileName(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > 128 || !utf8.ValidString(trimmed) || strings.ContainsAny(trimmed, `/\`) || trimmed == "." || trimmed == ".." || hasWorkspaceControlRune(trimmed) {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_entry_file_name", "entryFileName must be a safe single file name", "入口文件名格式不合法")
	}
	ext := strings.ToLower(path.Ext(trimmed))
	if _, ok := allowedArtifactEntryExts[ext]; !ok {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_entry_file_name", "entryFileName extension is not supported for static hosting", "不支持的静态入口文件扩展名")
	}
	return trimmed, nil
}

func hasWorkspaceControlRune(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func buildSingleFileStaticEntries(entryName, title, format string, content []byte) []bundledStaticEntry {
	ext := strings.ToLower(path.Ext(entryName))
	if ext == ".html" || ext == ".htm" || looksLikeCompleteHTML(content) {
		if entryName == "" || strings.EqualFold(entryName, "index.html") || strings.EqualFold(entryName, "index.htm") {
			return []bundledStaticEntry{{Path: "index.html", Data: content}}
		}
		return []bundledStaticEntry{
			{Path: "index.html", Data: content},
			{Path: entryName, Data: content},
		}
	}
	renderedHTML := renderSelfContainedArtifactHTML(title, format, string(content))
	if entryName == "" || strings.EqualFold(entryName, "index.html") {
		return []bundledStaticEntry{{Path: "index.html", Data: renderedHTML}}
	}
	return []bundledStaticEntry{
		{Path: "index.html", Data: renderedHTML},
		{Path: entryName, Data: content},
	}
}

func looksLikeCompleteHTML(data []byte) bool {
	trimmed := strings.ToLower(strings.TrimSpace(string(data[:min(len(data), 512)])))
	return strings.HasPrefix(trimmed, "<!doctype html") || strings.HasPrefix(trimmed, "<html")
}

func renderSelfContainedArtifactHTML(title, format, body string) []byte {
	safeTitle := html.EscapeString(strings.TrimSpace(title))
	if safeTitle == "" {
		safeTitle = "Published Artifact"
	}
	safeFormat := html.EscapeString(strings.TrimSpace(format))
	safeBody := html.EscapeString(body)
	var badge string
	if safeFormat != "" {
		badge = `<span class="badge">` + safeFormat + `</span>`
	}
	doc := `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + safeTitle + `</title>
<style>
:root { color-scheme: light dark; }
body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #f8fafc; color: #0f172a; line-height: 1.6; }
main { max-width: 960px; margin: 32px auto; padding: 24px 28px; background: #ffffff; border: 1px solid #e2e8f0; border-radius: 12px; box-shadow: 0 1px 2px rgba(15,23,42,0.04); }
header { display: flex; align-items: center; justify-content: space-between; gap: 12px; border-bottom: 1px solid #e2e8f0; padding-bottom: 12px; margin-bottom: 16px; }
h1 { margin: 0; font-size: 1.25rem; }
.badge { font-size: 0.75rem; padding: 2px 8px; border-radius: 999px; background: #f1f5f9; color: #475569; text-transform: uppercase; }
pre { margin: 0; white-space: pre-wrap; word-break: break-word; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.9rem; }
@media (prefers-color-scheme: dark) {
  body { background: #0f172a; color: #e2e8f0; }
  main { background: #1e293b; border-color: #334155; }
  header { border-bottom-color: #334155; }
  .badge { background: #334155; color: #cbd5e1; }
}
</style>
</head>
<body>
<main>
<header><h1>` + safeTitle + `</h1>` + badge + `</header>
<pre>` + safeBody + `</pre>
</main>
</body>
</html>
`
	return []byte(doc)
}

func packDeterministicTarGzReader(entries []bundledStaticEntry) (io.ReadCloser, error) {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b bundledStaticEntry) int {
		return strings.Compare(a.Path, b.Path)
	})
	var estimated int64
	for _, e := range sorted {
		estimated += int64(len(e.Data))
	}
	if estimated > inMemoryArchiveThreshold {
		f, err := openUnlinkedTempFile("soha-app-bundle-*.tar.gz")
		if err != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		if err := writeDeterministicTarGz(f, sorted); err != nil {
			_ = f.Close()
			return nil, err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			_ = f.Close()
			return nil, apperrors.ErrServiceUnavailable
		}
		return f, nil
	}
	var buf bytes.Buffer
	if err := writeDeterministicTarGz(&buf, sorted); err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

func writeDeterministicTarGz(dst io.Writer, sorted []bundledStaticEntry) error {
	gz, err := gzip.NewWriterLevel(dst, gzip.BestSpeed)
	if err != nil {
		return err
	}
	gz.ModTime = time.Unix(0, 0).UTC()
	tw := tar.NewWriter(gz)
	epoch := time.Unix(0, 0).UTC()

	seen := make(map[string]struct{}, len(sorted))
	for _, entry := range sorted {
		if _, dup := seen[entry.Path]; dup {
			continue
		}
		seen[entry.Path] = struct{}{}
		hdr := &tar.Header{
			Name:     entry.Path,
			Mode:     0o644,
			Size:     int64(len(entry.Data)),
			ModTime:  epoch,
			Typeflag: tar.TypeReg,
			Format:   tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			return err
		}
		if _, err := tw.Write(entry.Data); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			return err
		}
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return err
	}
	return gz.Close()
}

func openUnlinkedTempFile(pattern string) (*os.File, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(file.Name()); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

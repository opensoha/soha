package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func gitOpsApplication(documents []domainmanifest.RenderedDocument) (*unstructured.Unstructured, error) {
	for _, document := range documents {
		if document.APIVersion != "argoproj.io/v1alpha1" || document.Kind != "Application" {
			continue
		}
		if len(documents) != 1 {
			return nil, fmt.Errorf("%w: GitOps requires one Application root", apperrors.ErrInvalidArgument)
		}
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal([]byte(document.Content), &object.Object); err != nil {
			return nil, err
		}
		if err := resourceruntime.ValidateArgoApplication(object); err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
		}
		return object, nil
	}
	return nil, nil
}

func (s *DeclarativeService) gitOpsRepository(ctx context.Context, app domainapp.App, application *unstructured.Unstructured) (domainapp.SourceRepository, error) {
	id := application.GetAnnotations()["delivery.soha.io/repository-id"]
	if id == "" || !slices.Contains(app.RepositoryIDs, id) || s.sources == nil {
		return domainapp.SourceRepository{}, fmt.Errorf("%w: GitOps repository must be registered to this application", apperrors.ErrAccessDenied)
	}
	repository, err := s.sources.GetRepository(ctx, id)
	if err != nil {
		return repository, err
	}
	url, _, _ := unstructured.NestedString(application.Object, "spec", "source", "repoURL")
	if repository.ID != id || repository.URL != url {
		return repository, fmt.Errorf("%w: GitOps repository URL does not match the registered repository", apperrors.ErrConflict)
	}
	return repository, nil
}

func (s *DeclarativeService) freezeGitOpsDocuments(ctx context.Context, app domainapp.App, binding domainmanifest.EnvironmentBinding, documents []domainmanifest.RenderedDocument) ([]domainmanifest.RenderedDocument, error) {
	application, err := gitOpsApplication(documents)
	if err != nil || application == nil {
		return nil, err
	}
	repository, err := s.gitOpsRepository(ctx, app, application)
	if err != nil {
		return nil, err
	}
	if s.deliveryGit == nil {
		return nil, fmt.Errorf("%w: GitOps source reader is unavailable", apperrors.ErrInvalidArgument)
	}
	commit, _, _ := unstructured.NestedString(application.Object, "spec", "source", "targetRevision")
	directory, _, _ := unstructured.NestedString(application.Object, "spec", "source", "path")
	source, err := s.deliveryGit.ReadDeliveryDocuments(ctx, repository, domaindocument.Source{
		RepositoryID: repository.ID, RefType: "commit", RefValue: commit, Path: directory,
		IncludePatterns: []string{"*.yaml", "*.yml", "*.json", "**/*.yaml", "**/*.yml", "**/*.json"},
	})
	if err != nil {
		return nil, err
	}
	if source.ResolvedCommit != commit {
		return nil, fmt.Errorf("%w: GitOps source commit changed", apperrors.ErrConflict)
	}
	files := make([]domainmanifest.File, 0, len(source.Files))
	for _, file := range source.Files {
		name := path.Base(file.Path)
		if name == ".argocd-source.yaml" || strings.HasPrefix(name, ".argocd-source-") {
			return nil, fmt.Errorf("%w: GitOps source override files would bypass frozen Application inputs", apperrors.ErrInvalidArgument)
		}
		files = append(files, domainmanifest.File{Path: file.Path, Content: file.Content})
	}
	images, _, _ := unstructured.NestedStringSlice(application.Object, "spec", "source", "kustomize", "images")
	options := &domainmanifest.KustomizeOptions{}
	for _, image := range images {
		name, image, err := resourceruntime.ParseArgoImageOverride(image)
		if err != nil {
			return nil, err
		}
		repository, digest, _ := strings.Cut(image, "@")
		options.Images = append(options.Images, domainmanifest.KustomizeImage{Name: name, NewName: repository, Digest: digest})
	}
	rendered, err := s.renderer.Render(ctx, domainmanifest.Package{Renderer: domainmanifest.RendererKustomize}, domainmanifest.EnvironmentBinding{Namespace: binding.Namespace, Kustomize: options}, files, 1)
	if err != nil {
		return nil, err
	}
	if err := validateGitOpsDocuments(application, rendered.Documents); err != nil {
		return nil, err
	}
	return rendered.Documents, nil
}

func validateGitOpsDocuments(application *unstructured.Unstructured, documents []domainmanifest.RenderedDocument) error {
	resources := make([]*unstructured.Unstructured, 0, len(documents))
	for _, document := range documents {
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal([]byte(document.Content), &object.Object); err != nil {
			return err
		}
		if object.GetAPIVersion() != document.APIVersion || object.GetKind() != document.Kind || object.GetNamespace() != document.Namespace || object.GetName() != document.Name {
			return fmt.Errorf("%w: GitOps child identity does not match the frozen document", apperrors.ErrInvalidArgument)
		}
		resources = append(resources, object)
	}
	if err := resourceruntime.ValidateArgoResources(application, resources); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	return nil
}

func (s *DeclarativeService) validateGitOpsSnapshot(ctx context.Context, app domainapp.App, snapshot domainmanifest.DeliverySnapshot) error {
	application, err := gitOpsApplication(snapshot.Documents)
	if err != nil {
		return err
	}
	if application == nil {
		if len(snapshot.GitOpsDocuments) != 0 {
			return fmt.Errorf("%w: GitOps children require an Application root", apperrors.ErrInvalidArgument)
		}
		return nil
	}
	if _, err := s.gitOpsRepository(ctx, app, application); err != nil {
		return err
	}
	return validateGitOpsDocuments(application, snapshot.GitOpsDocuments)
}

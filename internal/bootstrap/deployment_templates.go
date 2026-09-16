package bootstrap

import (
	"context"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	catalogrepo "github.com/opensoha/soha/internal/repository/catalog"
	deliveryrepo "github.com/opensoha/soha/internal/repository/delivery"
	"gorm.io/gorm"
)

func seedDeploymentTemplates(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('soha-default-deployment-templates'))`).Error; err != nil {
			return err
		}
		repo := catalogrepo.New(tx)
		existing, err := repo.ListDeploymentTemplates(ctx)
		if err != nil {
			return err
		}
		keys := make(map[string]bool, len(existing))
		for _, item := range existing {
			keys[item.Key] = true
		}
		for _, spec := range domaincatalog.BuiltinDeploymentTemplates() {
			if keys[spec.Key] {
				continue
			}
			draft, err := repo.SaveDeploymentTemplate(ctx, "", domaincatalog.DeploymentTemplateInput{DeploymentTemplateSpec: spec})
			if err != nil {
				return err
			}
			if _, err := repo.PublishDeploymentTemplate(ctx, draft.ID, draft.Revision); err != nil {
				return err
			}
		}
		return seedServiceCreationPresets(ctx, tx)
	})
}

func seedServiceCreationPresets(ctx context.Context, db *gorm.DB) error {
	repo := deliveryrepo.New(db)
	existing, err := repo.ListDeliveryBlueprints(ctx)
	if err != nil {
		return err
	}
	keys := make(map[string]bool, len(existing))
	for _, item := range existing {
		keys[item.Key] = true
	}
	templates, err := catalogrepo.New(db).ListDeploymentTemplates(ctx)
	if err != nil {
		return err
	}
	for _, template := range templates {
		key := template.Key + "-dockerfile"
		if !map[string]bool{"soha-http": true, "soha-worker": true, "soha-static": true, "soha-job": true, "soha-http-periodic": true}[template.Key] || keys[key] || !template.Enabled || template.PublicationState != "published" {
			continue
		}
		kind := domainapp.ServiceKindKubernetesWorkload
		if template.Health.Mode == "job_complete" {
			kind = domainapp.ServiceKindJob
		}
		_, err = repo.CreateDeliveryBlueprint(ctx, domaindelivery.DeliveryBlueprintInput{
			Key: key, Name: template.Name + " · Dockerfile", Enabled: true,
			Description:      "仓库须包含可构建的 Dockerfile，并推送到已配置的镜像仓库。保存后可调整构建方式与部署参数。",
			ApplicationDraft: domaindelivery.BlueprintApplicationDraft{Key: "sample-app", Name: "Sample App", Enabled: true},
			Services: []domaindelivery.DeliveryDraftService{{Key: "service", Name: template.Name, ServiceKind: kind, BuildSourceID: "source-1", Enabled: true,
				DeploymentTemplate: &domaincatalog.DeploymentTemplateBinding{TemplateID: template.ID, Version: template.PublishedVersion, Parameters: template.Defaults},
				Containers:         []domainapp.ServiceContainerInput{{Name: "main", DockerfilePath: "Dockerfile", BuildContextDir: "."}},
			}},
			BuildSources: []domainapp.BuildSourceInput{{ID: "source-1", Name: "Dockerfile", Type: "repo_dockerfile", Enabled: true, Config: map[string]any{"dockerfilePath": "Dockerfile", "contextDir": ".", "builderKind": "docker"}}},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func seedDeliveryRecipes(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('soha-default-delivery-recipes'))`).Error; err != nil {
			return err
		}
		repo := catalogrepo.New(tx)
		existing, err := repo.ListWorkflowTemplates(ctx)
		if err != nil {
			return err
		}
		keys := map[string]bool{}
		for _, item := range existing {
			keys[item.Key] = true
		}
		for _, input := range domaincatalog.BuiltinDeliveryRecipes() {
			if keys[input.Key] {
				continue
			}
			if _, err := repo.CreateWorkflowTemplate(ctx, input); err != nil {
				return err
			}
		}
		return nil
	})
}

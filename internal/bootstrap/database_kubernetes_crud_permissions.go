package bootstrap

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

var kubernetesCRUDPermissionKeys = []string{
	"platform.network.http-routes.create", "platform.network.http-routes.update", "platform.network.http-routes.delete",
	"platform.network.grpc-routes.create", "platform.network.grpc-routes.update", "platform.network.grpc-routes.delete",
	"platform.network.backend-tls-policies.create", "platform.network.backend-tls-policies.update", "platform.network.backend-tls-policies.delete",
	"platform.network.reference-grants.create", "platform.network.reference-grants.update", "platform.network.reference-grants.delete",
	"platform.pods.create", "platform.workloads.replication-controllers.delete",
}

func upgradeKubernetesCRUDPermissions(ctx context.Context, db *gorm.DB) error {
	const key = "bootstrap.kubernetes_crud_permissions_version"
	version, err := readSeedVersion(ctx, db, key)
	if err != nil || version != "" {
		return err
	}
	permissions, err := json.Marshal(kubernetesCRUDPermissionKeys)
	if err != nil {
		return err
	}
	// New write grants belong to the built-in administrator; custom roles stay explicit.
	if err := db.WithContext(ctx).Exec(`
		UPDATE roles SET permission_keys = (
			SELECT jsonb_agg(DISTINCT permission ORDER BY permission)
			FROM jsonb_array_elements_text(COALESCE(roles.permission_keys::jsonb, '[]'::jsonb) || ?::jsonb) AS added(permission)
		), updated_at=? WHERE id='admin' AND scope='system'
	`, string(permissions), time.Now().UTC()).Error; err != nil {
		return err
	}
	return writeSeedVersion(ctx, db, key, "2026-10-19-task113")
}

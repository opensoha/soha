package bootstrap

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apphosting "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/infrastructure/hostedappdata"
	"github.com/opensoha/soha/internal/infrastructure/hostedbuild"
	"github.com/opensoha/soha/internal/infrastructure/hostedruntime"
	storage "github.com/opensoha/soha/internal/infrastructure/softwareobjectstore"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type hostedDynamic struct {
	builder   *hostedbuild.Builder
	runtime   *hostedruntime.Runtime
	data      *hostedappdata.Provisioner
	kube      kubernetes.Interface
	objects   *storage.Provider
	namespace string
	dataKey   []byte
}

func configureHostedDynamic(ctx context.Context, coreDB cfgpkg.DatabaseConfig, core *gorm.DB, objects *storage.Provider) (*hostedDynamic, error) {
	namespace := strings.TrimSpace(os.Getenv("SOHA_APP_HOSTING_RUNTIME_NAMESPACE"))
	if namespace == "" {
		return nil, nil
	}
	buildConfig := hostedBuildConfiguration()
	if buildConfig.Namespace == namespace {
		return nil, errors.New("hosted build and runtime namespaces must be distinct")
	}
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.New("dynamic hosting requires a dedicated in-cluster Kubernetes identity")
	}
	return assembleHostedDynamic(ctx, kubeConfig, coreDB, core, namespace, buildConfig, objects)
}

func assembleHostedDynamic(ctx context.Context, config *rest.Config, coreDB cfgpkg.DatabaseConfig, core *gorm.DB, namespace string, buildCfg hostedbuild.Config, objects *storage.Provider) (*hostedDynamic, error) {
	kube, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	builder, err := hostedbuild.New(kube, buildCfg)
	if err != nil {
		return nil, err
	}
	runtime, err := hostedruntime.New(dyn, namespace)
	if err != nil {
		return nil, err
	}
	data, key, err := configureHostedAppData(ctx, coreDB, core)
	if err != nil {
		return nil, err
	}
	return &hostedDynamic{builder: builder, runtime: runtime, data: data, kube: kube, objects: objects, namespace: namespace, dataKey: key}, nil
}

func hostedBuildConfiguration() hostedbuild.Config {
	return hostedbuild.Config{
		Namespace:          os.Getenv("SOHA_APP_HOSTING_BUILD_NAMESPACE"),
		BuildClientImage:   os.Getenv("SOHA_APP_HOSTING_BUILD_CLIENT_IMAGE"),
		NodeBase:           os.Getenv("SOHA_APP_HOSTING_NODE_BASE_IMAGE"),
		PythonBase:         os.Getenv("SOHA_APP_HOSTING_PYTHON_BASE_IMAGE"),
		RegistryRepository: os.Getenv("SOHA_APP_HOSTING_REGISTRY_REPOSITORY"),
		DockerConfigSecret: os.Getenv("SOHA_APP_HOSTING_BUILD_REGISTRY_SECRET"),
		BuildKitAddress:    os.Getenv("SOHA_APP_HOSTING_BUILDKIT_ADDRESS"),
		BuildKitTLSSecret:  os.Getenv("SOHA_APP_HOSTING_BUILDKIT_TLS_SECRET"),
		SourceOrigin:       os.Getenv("SOHA_APP_HOSTING_BUILD_SOURCE_ORIGIN"),
		RuntimeClassName:   os.Getenv("SOHA_APP_HOSTING_BUILD_RUNTIME_CLASS"),
	}
}

func configureHostedAppData(ctx context.Context, coreDB cfgpkg.DatabaseConfig, core *gorm.DB) (*hostedappdata.Provisioner, []byte, error) {
	dsn := strings.TrimSpace(os.Getenv("SOHA_APP_HOSTING_APP_DATA_ADMIN_DSN"))
	rawKey := strings.TrimSpace(os.Getenv("SOHA_APP_HOSTING_APP_DATA_KEY"))
	limit, err := strconv.ParseInt(os.Getenv("SOHA_APP_HOSTING_APP_DATA_MAX_POOL_BYTES"), 10, 64)
	if err != nil || len(rawKey) < 32 {
		return nil, nil, errors.New("dynamic hosting requires a data key and positive app-data capacity")
	}
	provisioner, err := hostedappdata.New(dsn, limit)
	if err != nil {
		return nil, nil, err
	}
	if err := verifyHostedDataPool(ctx, dsn, coreDB, core); err != nil {
		return nil, nil, err
	}
	return provisioner, []byte(rawKey), nil
}

func verifyHostedDataPool(ctx context.Context, dsn string, coreDB cfgpkg.DatabaseConfig, core *gorm.DB) error {
	config, err := pgx.ParseConfig(dsn)
	if err != nil || (strings.EqualFold(config.Host, coreDB.Host) && int(config.Port) == coreDB.Port) {
		return errors.New("app-data must use an independent PostgreSQL endpoint")
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, err := pgx.ConnectConfig(probe, config)
	if err != nil {
		return errors.New("app-data identity check could not connect")
	}
	defer func() { _ = db.Close(probe) }()
	const identityQuery = "SELECT system_identifier::text FROM pg_catalog.pg_control_system()"
	var appIdentity, coreIdentity string
	if err := db.QueryRow(probe, identityQuery).Scan(&appIdentity); err != nil {
		return errors.New("app-data cluster identity could not be verified")
	}
	if err := core.WithContext(probe).Raw(identityQuery).Row().Scan(&coreIdentity); err != nil || coreIdentity == "" || appIdentity == "" || coreIdentity == appIdentity {
		return errors.New("app-data must have an independently verified PostgreSQL cluster identity")
	}
	return nil
}

func (h *hostedDynamic) StartBuild(ctx context.Context, rev domain.Revision) error {
	url, err := h.objects.PresignHostedSource(ctx, rev.StorageIntegrationID, rev.SourceKey)
	if err != nil {
		return err
	}
	return h.builder.Prepare(ctx, hostedbuild.Spec{RevisionID: rev.ID, Template: rev.BuildTemplate, SourceURL: url})
}

func (h *hostedDynamic) BuildResult(ctx context.Context, rev domain.Revision) (string, bool, error) {
	image, ready, err := h.builder.Result(ctx, rev.ID)
	if errors.Is(err, hostedbuild.ErrBuildFailed) {
		return "", false, errors.Join(apphosting.ErrDynamicRevisionFailed, errors.New("managed build did not produce a verified image"))
	}
	return image, ready, err
}

func (h *hostedDynamic) PrepareRuntime(ctx context.Context, rev domain.Revision) error {
	spec := hostedruntime.Spec{RevisionID: rev.ID, Image: rev.ImageRef}
	data, err := apphosting.OpenRuntimeEnvironment(h.dataKey, rev)
	if err != nil {
		return errors.Join(apphosting.ErrDynamicRevisionFailed, err)
	}
	if rev.Database {
		dsn, err := h.prepareHostedData(ctx, rev)
		if err != nil {
			return err
		}
		data["DATABASE_URL"] = []byte(dsn)
	}
	if len(data) > 0 {
		secretName, err := h.ensureRuntimeSecret(ctx, rev, data)
		if err != nil {
			return err
		}
		spec.EnvironmentSecret = secretName
	}
	return h.runtime.Prepare(ctx, spec)
}

func (h *hostedDynamic) RuntimeReady(ctx context.Context, rev domain.Revision) (bool, error) {
	ready, err := h.runtime.Ready(ctx, rev.ID)
	if errors.Is(err, hostedruntime.ErrRevisionFailed) {
		return false, errors.Join(apphosting.ErrDynamicRevisionFailed, err)
	}
	return ready, err
}

func (h *hostedDynamic) ActivateRuntime(ctx context.Context, rev domain.Revision) error {
	if rev.Database {
		return h.data.SetRevisionWritable(ctx, rev.AppID, rev.ID, true)
	}
	return nil
}

func (h *hostedDynamic) RestrictRuntime(ctx context.Context, rev domain.Revision) error {
	if rev.Database {
		return h.data.SetRevisionWritable(ctx, rev.AppID, rev.ID, false)
	}
	return nil
}

func (h *hostedDynamic) CheckRollback(ctx context.Context, rev domain.Revision) error {
	if rev.Status != sohaapi.HostedAppRevisionStatusReady || rev.ImageRef == "" {
		return apperrors.ErrConflict
	}
	ready, err := h.RuntimeReady(ctx, rev)
	if err != nil || !ready {
		return apperrors.NewBusiness(apperrors.ErrConflict, "runtime_not_ready", "rollback revision is not ready", "回滚目标运行环境未就绪")
	}
	if rev.Database || len(rev.RuntimeEnvironment) != 0 {
		if err := h.checkRuntimeSecret(ctx, rev); err != nil {
			return err
		}
	}
	if !rev.Database {
		return nil
	}
	version, err := h.data.SchemaVersion(ctx, rev.AppID)
	if err != nil || rev.DataSchema < 1 || version < rev.DataSchema || version > 2 {
		return apperrors.NewBusiness(apperrors.ErrConflict, "app_data_schema_incompatible", "rollback revision requires a compatible app data schema", "回滚目标需要兼容的应用数据库结构")
	}
	return nil
}

func (h *hostedDynamic) DeleteBuild(ctx context.Context, rev domain.Revision) error {
	return h.builder.Delete(ctx, rev.ID)
}

func (h *hostedDynamic) DeleteRuntime(ctx context.Context, rev domain.Revision) error {
	err := h.runtime.Delete(ctx, rev.ID)
	if rev.Database {
		err = errors.Join(err, h.data.DeleteRevisionRole(ctx, rev.AppID, rev.ID))
	}
	return errors.Join(err, h.deleteRuntimeSecret(ctx, rev))
}

func (h *hostedDynamic) prepareHostedData(ctx context.Context, rev domain.Revision) (string, error) {
	if _, err := h.data.Provision(ctx, rev.AppID, h.password(rev.AppID)); err != nil {
		if errors.Is(err, hostedappdata.ErrCapacity) || errors.Is(err, hostedappdata.ErrIsolation) {
			return "", errors.Join(apphosting.ErrDynamicRevisionFailed, err)
		}
		return "", err
	}
	version, err := h.data.SchemaVersion(ctx, rev.AppID)
	if err != nil {
		return "", err
	}
	if rev.DataSchema < 1 || rev.DataSchema > 2 || version < 1 || version > 2 {
		return "", errors.Join(apphosting.ErrDynamicRevisionFailed, errors.New("app data schema is not compatible with this hosted runtime"))
	}
	if version < rev.DataSchema {
		if err := h.data.UpgradeSchema(ctx, rev.AppID, rev.DataSchema); err != nil {
			return "", err
		}
	}
	return h.data.ProvisionRevision(ctx, rev.AppID, rev.ID, h.password(rev.AppID+"/"+rev.ID))
}

func (h *hostedDynamic) password(scope string) string {
	mac := hmac.New(sha256.New, h.dataKey)
	_, _ = mac.Write([]byte("soha-hosted-db/v1:" + scope))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *hostedDynamic) ensureRuntimeSecret(ctx context.Context, rev domain.Revision, data map[string][]byte) (string, error) {
	name, err := h.runtime.ServiceName(rev.ID)
	if err != nil {
		return "", err
	}
	immutable := true
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: hostedRuntimeSecretLabels(rev)},
		Immutable:  &immutable, Data: data,
	}
	secrets := h.kube.CoreV1().Secrets(h.namespace)
	_, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
	if !kubeerrors.IsAlreadyExists(err) {
		return name, err
	}
	current, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if !hostedRuntimeSecretMatches(current, rev, data) {
		return "", errors.New("hosted runtime secret does not match the revision")
	}
	return name, nil
}

func (h *hostedDynamic) checkRuntimeSecret(ctx context.Context, rev domain.Revision) error {
	unavailable := apperrors.NewBusiness(
		apperrors.ErrConflict,
		"runtime_credentials_unavailable",
		"rollback revision credentials are unavailable",
		"回滚版本的运行凭据不可用",
	)
	expected, err := h.expectedRuntimeEnvironment(rev)
	if err != nil {
		return unavailable
	}
	name, err := h.runtime.ServiceName(rev.ID)
	if err != nil {
		return unavailable
	}
	item, err := h.kube.CoreV1().Secrets(h.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || !hostedRuntimeSecretMatches(item, rev, expected) {
		return unavailable
	}
	if rev.Database {
		password := h.password(rev.AppID + "/" + rev.ID)
		if err := h.data.CheckRevisionCredential(ctx, rev.AppID, rev.ID, password); err != nil {
			return unavailable
		}
	}
	return nil
}

func (h *hostedDynamic) expectedRuntimeEnvironment(rev domain.Revision) (map[string][]byte, error) {
	data, err := apphosting.OpenRuntimeEnvironment(h.dataKey, rev)
	if err != nil {
		return nil, err
	}
	if !rev.Database {
		return data, nil
	}
	if h.data == nil {
		return nil, errors.New("hosted app data is not configured")
	}
	dsn, err := h.data.RevisionRuntimeURL(rev.AppID, rev.ID, h.password(rev.AppID+"/"+rev.ID))
	if err != nil {
		return nil, err
	}
	data["DATABASE_URL"] = []byte(dsn)
	return data, nil
}

func (h *hostedDynamic) deleteRuntimeSecret(ctx context.Context, rev domain.Revision) error {
	name, err := h.runtime.ServiceName(rev.ID)
	if err != nil {
		return err
	}
	secrets := h.kube.CoreV1().Secrets(h.namespace)
	item, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if kubeerrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ownedHostedRuntimeSecret(item, rev) {
		return errors.New("hosted database secret belongs to another revision")
	}
	err = secrets.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &item.UID}})
	if kubeerrors.IsNotFound(err) {
		return nil
	}
	return err
}

func hostedRuntimeSecretLabels(rev domain.Revision) map[string]string {
	return map[string]string{"app.kubernetes.io/managed-by": "soha-app-hosting", "soha.io/app-id": rev.AppID, "soha.io/revision-id": rev.ID}
}

func ownedHostedRuntimeSecret(secret *corev1.Secret, rev domain.Revision) bool {
	for k, v := range hostedRuntimeSecretLabels(rev) {
		if secret.Labels[k] != v {
			return false
		}
	}
	return true
}

func hostedRuntimeSecretMatches(secret *corev1.Secret, rev domain.Revision, data map[string][]byte) bool {
	return secret != nil && ownedHostedRuntimeSecret(secret, rev) &&
		secret.Immutable != nil && *secret.Immutable &&
		maps.EqualFunc(secret.Data, data, bytes.Equal)
}

func startHostedDynamicWorker(ctx context.Context, service *apphosting.Service, logger *zap.Logger) <-chan struct{} {
	if !service.DynamicEnabled() {
		return nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.RunDynamic(ctx, func(_ error) {
			logger.Named("app-hosting").Warn("dynamic revision reconciliation will retry")
		})
	}()
	return done
}

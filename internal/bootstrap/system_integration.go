package bootstrap

import (
	"strconv"
	"strings"
	"time"

	appsystemintegration "github.com/opensoha/soha/internal/application/systemintegration"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	gitlabinfra "github.com/opensoha/soha/internal/infrastructure/gitlab"
)

type gitLabSourceAdapterFactory struct{}

func (gitLabSourceAdapterFactory) Build(item domain.Integration, credentials map[string]string) (appsystemintegration.SourceAdapter, error) {
	config := integrationConfiguration(item)
	perPage, _ := strconv.Atoi(config["per_page"])
	timeout, _ := time.ParseDuration(config["timeout"])
	return gitlabinfra.NewWithOptions(gitlabinfra.Options{
		// The service applies the enabled gate for normal source operations. Keep
		// the adapter active so administrators can test a disabled connection.
		Enabled: true, BaseURL: config["base_url"], Token: credentials["token"],
		GroupID: config["group_id"], PerPage: perPage, Timeout: timeout,
	}), nil
}

func integrationConfiguration(item domain.Integration) map[string]string {
	result := make(map[string]string, len(item.Configuration))
	for _, field := range item.Configuration {
		result[strings.TrimSpace(field.Key)] = strings.TrimSpace(field.Value)
	}
	return result
}

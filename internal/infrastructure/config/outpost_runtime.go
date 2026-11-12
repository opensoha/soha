package config

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// OutpostRuntimeSlotConfig is installer policy, never a workload supplied by an API caller.
type OutpostRuntimeSlotConfig struct {
	ID                   string `mapstructure:"id"`
	OutpostID            string `mapstructure:"outpost_id"`
	ExecutorID           string `mapstructure:"executor_id"`
	Namespace            string `mapstructure:"namespace"`
	Name                 string `mapstructure:"name"`
	UID                  string `mapstructure:"uid"`
	TemplateDigest       string `mapstructure:"template_digest"`
	MaxReplicas          int32  `mapstructure:"max_replicas"`
	AllowAdoption        bool   `mapstructure:"allow_adoption"`
	PreviousOwnerStopped bool   `mapstructure:"previous_owner_stopped"`
}

type OutpostRuntimeExecutorConfig struct {
	ExecutorID string `mapstructure:"executor_id"`
	TokenFile  string `mapstructure:"token_file"`
}

func validateOutpostRuntimeConfig(c RuntimeConfig) []string {
	var problems []string
	executors := map[string]bool{}
	identifier := regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	for _, executor := range c.OutpostRuntimeExecutors {
		if !identifier.MatchString(executor.ExecutorID) || executors[executor.ExecutorID] || strings.TrimSpace(executor.TokenFile) == "" {
			problems = append(problems, "runtime.outpost_runtime_executors requires unique executor IDs and private token files")
		}
		executors[executor.ExecutorID] = true
	}
	slots, workloads := map[string]bool{}, map[string]bool{}
	for _, slot := range c.OutpostSlots {
		identity := slot.Namespace + "/" + slot.Name
		if !validOutpostRuntimeSlot(slot, identifier) || !executors[slot.ExecutorID] || slots[slot.ID] || workloads[identity] {
			problems = append(problems, "runtime.outpost_slots requires unique installer-approved workloads, identities, executor and positive replica limit")
		}
		if slot.PreviousOwnerStopped && !slot.AllowAdoption {
			problems = append(problems, "runtime.outpost_slots previous_owner_stopped requires explicit adoption policy")
		}
		slots[slot.ID], workloads[identity] = true, true
	}
	return problems
}

func validOutpostRuntimeSlot(slot OutpostRuntimeSlotConfig, identifier *regexp.Regexp) bool {
	return identifier.MatchString(slot.ID) && slot.OutpostID != "" && slot.Namespace != "" &&
		slot.Name != "" && slot.UID != "" && slot.TemplateDigest != "" && slot.MaxReplicas > 0
}

// ReadToken reads a bounded installer-provisioned credential without exposing its contents.
func (c OutpostRuntimeExecutorConfig) ReadToken() (string, error) {
	file, err := os.Open(c.TokenFile)
	if err != nil {
		return "", fmt.Errorf("open Outpost executor token file: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	// A dedicated Pod fsGroup may read a projected Secret; writable groups or
	// any world access are not acceptable for executor credentials.
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0037 != 0 {
		return "", fmt.Errorf("outpost executor token file must be a private regular file")
	}
	const maxTokenBytes = 8192
	data, err := io.ReadAll(io.LimitReader(file, maxTokenBytes+1))
	if err != nil || len(data) > maxTokenBytes {
		return "", fmt.Errorf("read bounded Outpost executor token file")
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 || strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("outpost executor token must contain at least 32 non-whitespace bytes")
	}
	return token, nil
}

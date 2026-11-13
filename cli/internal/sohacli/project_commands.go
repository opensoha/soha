package sohacli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"gopkg.in/yaml.v3"
)

const (
	defaultProjectManifestPath = ".soha/project.yaml"
	maxProjectManifestBytes    = 2 << 20
)

var (
	projectNamePattern   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	projectStepIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	projectToolPattern   = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	projectSensitiveKey  = regexp.MustCompile(`(?i)(password|passwd|secret|token|kubeconfig|private[_-]?key|api[_-]?key)`)
)

type projectManifest struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
	Metadata   struct {
		Name string `json:"name" yaml:"name"`
	} `json:"metadata" yaml:"metadata"`
	Spec struct {
		Steps []projectStep `json:"steps" yaml:"steps"`
	} `json:"spec" yaml:"spec"`
}

type projectStep struct {
	ID         string            `json:"id" yaml:"id"`
	Tool       string            `json:"tool" yaml:"tool"`
	Input      map[string]any    `json:"input" yaml:"input"`
	SecretRefs map[string]string `json:"secretRefs,omitempty" yaml:"secretRefs,omitempty"`
	DependsOn  []string          `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Wait       *bool             `json:"wait,omitempty" yaml:"wait,omitempty"`
}

type projectStepResult struct {
	ID               string           `json:"id"`
	Tool             string           `json:"tool"`
	RequestID        string           `json:"requestId"`
	Status           string           `json:"status"`
	RequiresApproval bool             `json:"requiresApproval,omitempty"`
	Output           any              `json:"output,omitempty"`
	RelatedIDs       map[string]any   `json:"relatedIds,omitempty"`
	Operation        *ComputeTaskView `json:"operation,omitempty"`
}

type projectRunResult struct {
	Project string              `json:"project"`
	Action  string              `json:"action"`
	Ready   bool                `json:"ready"`
	Status  string              `json:"status"`
	Steps   []projectStepResult `json:"steps"`
}

type projectCommandOptions struct {
	output                string
	yes                   bool
	interval, waitTimeout time.Duration
}

func runProject(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("project requires plan or apply")
	}
	switch args[0] {
	case "plan", "apply":
		return runProjectAction(ctx, args[0], args[1:], rt)
	default:
		return fmt.Errorf("unknown project command %q", args[0])
	}
}

func runProjectAction(ctx context.Context, action string, args []string, rt Runtime) error {
	leading, args := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("project "+action, args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "output format: json or yaml")
	aiClientID := fs.String("ai-client-id", "", "override AI client id")
	aiClientName := fs.String("ai-client", "", "override AI client display name")
	skillID := fs.String("skill-id", "", "override skill id")
	source := fs.String("source", "soha-project", "override source label")
	yes := fs.Bool("yes", false, "skip apply confirmation")
	interval := fs.Duration("interval", defaultPollInterval, "operation polling interval")
	waitTimeout := fs.Duration("wait-timeout", 5*time.Minute, "maximum wait per operation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) > 1 {
		return fmt.Errorf("project %s accepts at most one manifest path", action)
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml")
	if err != nil {
		return err
	}
	if *interval <= 0 || *waitTimeout <= 0 {
		return fmt.Errorf("--interval and --wait-timeout must be greater than 0")
	}
	path := defaultProjectManifestPath
	if len(positionals) == 1 {
		path = strings.TrimSpace(positionals[0])
	}
	manifest, steps, err := readAndValidateProjectManifest(rt, path)
	if err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, profile)
	headers := gatewayHeaders(profile, *aiClientID, *aiClientName, *skillID, *source)
	capabilities, err := client.Capabilities(ctx, headers)
	if err != nil {
		return err
	}
	options := projectCommandOptions{
		output: format, yes: *yes, interval: *interval, waitTimeout: *waitTimeout,
	}
	plan, triggerTools, err := planProject(ctx, client, headers, capabilities, manifest, steps)
	if err != nil {
		return err
	}
	if action == "plan" {
		if err := writeStructuredOutput(rt.Out, options.output, sanitizeCLIValue(plan)); err != nil {
			return err
		}
		if !plan.Ready {
			return fmt.Errorf("project plan is blocked")
		}
		return nil
	}
	if !plan.Ready {
		if err := writeStructuredOutput(rt.Out, options.output, sanitizeCLIValue(plan)); err != nil {
			return err
		}
		return fmt.Errorf("project apply blocked by plan")
	}
	if !options.yes && projectHasProtectedTools(triggerTools) {
		confirmed, err := confirmAction(rt, fmt.Sprintf("Apply project %s (%d steps)?", manifest.Metadata.Name, len(steps)))
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("project apply declined; pass --yes for non-interactive use")
		}
	}
	result, applyErr := applyProject(ctx, client, headers, capabilities, manifest, steps, options)
	if err := writeStructuredOutput(rt.Out, options.output, sanitizeCLIValue(result)); err != nil {
		return err
	}
	return applyErr
}

func readAndValidateProjectManifest(rt Runtime, path string) (projectManifest, []projectStep, error) {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(io.LimitReader(rt.In, maxProjectManifestBytes+1))
	} else {
		// #nosec G304 -- the manifest path is explicitly supplied by the CLI caller.
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return projectManifest{}, nil, err
	}
	if len(raw) > maxProjectManifestBytes {
		return projectManifest{}, nil, fmt.Errorf("project manifest exceeds %d bytes", maxProjectManifestBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var manifest projectManifest
	if err := decoder.Decode(&manifest); err != nil {
		return projectManifest{}, nil, fmt.Errorf("decode project manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return projectManifest{}, nil, fmt.Errorf("project manifest must contain one YAML document")
		}
		return projectManifest{}, nil, fmt.Errorf("decode project manifest: %w", err)
	}
	steps, err := validateAndOrderProjectManifest(manifest)
	if err != nil {
		return projectManifest{}, nil, err
	}
	return manifest, steps, nil
}

func validateAndOrderProjectManifest(manifest projectManifest) ([]projectStep, error) {
	if manifest.APIVersion != "opensoha.io/v1alpha1" || manifest.Kind != "Project" {
		return nil, fmt.Errorf("project manifest requires apiVersion opensoha.io/v1alpha1 and kind Project")
	}
	name := manifest.Metadata.Name
	if len(name) == 0 || len(name) > 63 || !projectNamePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid project metadata.name %q", name)
	}
	if len(manifest.Spec.Steps) == 0 || len(manifest.Spec.Steps) > 50 {
		return nil, fmt.Errorf("project spec.steps must contain 1 to 50 steps")
	}
	byID := make(map[string]projectStep, len(manifest.Spec.Steps))
	for _, step := range manifest.Spec.Steps {
		if len(step.ID) == 0 || len(step.ID) > 64 || !projectStepIDPattern.MatchString(step.ID) {
			return nil, fmt.Errorf("invalid project step id %q", step.ID)
		}
		if _, exists := byID[step.ID]; exists {
			return nil, fmt.Errorf("duplicate project step id %q", step.ID)
		}
		if len(step.Tool) == 0 || len(step.Tool) > 128 || !projectToolPattern.MatchString(step.Tool) || !strings.HasSuffix(step.Tool, ".trigger") {
			return nil, fmt.Errorf("project step %q tool must be an executable *.trigger capability", step.ID)
		}
		if step.Input == nil {
			return nil, fmt.Errorf("project step %q requires an input object", step.ID)
		}
		if err := validateProjectInputKeys(step.Input, "input"); err != nil {
			return nil, fmt.Errorf("project step %q: %w", step.ID, err)
		}
		if len(step.DependsOn) > 49 {
			return nil, fmt.Errorf("project step %q has too many dependencies", step.ID)
		}
		seenDependencies := map[string]struct{}{}
		for _, dependency := range step.DependsOn {
			if dependency == step.ID {
				return nil, fmt.Errorf("project step %q cannot depend on itself", step.ID)
			}
			if _, duplicate := seenDependencies[dependency]; duplicate {
				return nil, fmt.Errorf("project step %q repeats dependency %q", step.ID, dependency)
			}
			seenDependencies[dependency] = struct{}{}
		}
		if err := validateSecretRefs(step.SecretRefs); err != nil {
			return nil, fmt.Errorf("project step %q: %w", step.ID, err)
		}
		byID[step.ID] = step
	}
	for _, step := range manifest.Spec.Steps {
		for _, dependency := range step.DependsOn {
			if _, exists := byID[dependency]; !exists {
				return nil, fmt.Errorf("project step %q depends on unknown step %q", step.ID, dependency)
			}
		}
	}
	ordered := make([]projectStep, 0, len(manifest.Spec.Steps))
	completed := map[string]struct{}{}
	for len(ordered) < len(manifest.Spec.Steps) {
		progress := false
		for _, step := range manifest.Spec.Steps {
			if _, done := completed[step.ID]; done || !projectDependenciesComplete(step, completed) {
				continue
			}
			ordered = append(ordered, step)
			completed[step.ID] = struct{}{}
			progress = true
		}
		if !progress {
			return nil, fmt.Errorf("project step dependencies contain a cycle")
		}
	}
	return ordered, nil
}

func validateProjectInputKeys(value any, path string) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if projectSensitiveKey.MatchString(key) {
				return fmt.Errorf("%s.%s embeds a forbidden credential field; use secretRefs", path, key)
			}
			if err := validateProjectInputKeys(item, path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		for index, item := range typed {
			if err := validateProjectInputKeys(item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func projectDependenciesComplete(step projectStep, completed map[string]struct{}) bool {
	for _, dependency := range step.DependsOn {
		if _, ok := completed[dependency]; !ok {
			return false
		}
	}
	return true
}

func planProject(ctx context.Context, client APIClient, headers map[string]string, capabilities Manifest, manifest projectManifest, steps []projectStep) (projectRunResult, []ToolCapability, error) {
	result := projectRunResult{Project: manifest.Metadata.Name, Action: "plan", Ready: true, Status: "ready"}
	triggerTools := make([]ToolCapability, 0, len(steps))
	for _, step := range steps {
		triggerTool, ok := findToolCapability(capabilities, step.Tool)
		if !ok {
			return result, nil, fmt.Errorf("project step %q tool %q is not available", step.ID, step.Tool)
		}
		planToolName, ok := findProjectPlanTool(capabilities, step.Tool)
		if !ok {
			return result, nil, fmt.Errorf("project step %q has no live plan or preflight capability", step.ID)
		}
		requestID := projectStepRequestID(manifest.Metadata.Name, step)
		planTool, _ := findToolCapability(capabilities, planToolName)
		invocation, err := client.InvokeCapability(ctx, planTool, projectStepInput(step, requestID, false), requestID+"-plan", step.SecretRefs, headers)
		if err != nil {
			return result, nil, fmt.Errorf("plan project step %q: %w", step.ID, err)
		}
		ready := projectInvocationReady(invocation)
		status := "blocked"
		if ready {
			status = "ready"
		}
		result.Steps = append(result.Steps, projectStepResult{
			ID: step.ID, Tool: planToolName, RequestID: requestID + "-plan",
			Status:           status,
			RequiresApproval: invocation.RequiresApproval, Output: invocation.Output,
			RelatedIDs: invocation.RelatedIDs,
		})
		result.Ready = result.Ready && ready
		triggerTools = append(triggerTools, triggerTool)
	}
	if !result.Ready {
		result.Status = "blocked"
	}
	return result, triggerTools, nil
}

func findProjectPlanTool(capabilities Manifest, triggerTool string) (string, bool) {
	base := strings.TrimSuffix(triggerTool, ".trigger")
	for _, candidate := range []string{base + ".plan", base + ".preflight"} {
		if _, ok := findToolCapability(capabilities, candidate); ok {
			return candidate, true
		}
	}
	return "", false
}

func projectInvocationReady(result ToolInvocationResult) bool {
	switch strings.ToLower(strings.TrimSpace(result.Result)) {
	case "success", "succeeded":
	default:
		return false
	}
	if output, ok := result.Output.(map[string]any); ok {
		if ready, exists := output["ready"].(bool); exists {
			return ready
		}
	}
	return true
}

func projectHasProtectedTools(tools []ToolCapability) bool {
	for _, tool := range tools {
		if protectedTool(tool) {
			return true
		}
	}
	return false
}

func applyProject(ctx context.Context, client APIClient, headers map[string]string, capabilities Manifest, manifest projectManifest, steps []projectStep, options projectCommandOptions) (projectRunResult, error) {
	result := projectRunResult{Project: manifest.Metadata.Name, Action: "apply", Ready: true, Status: "succeeded"}
	for _, step := range steps {
		requestID := projectStepRequestID(manifest.Metadata.Name, step)
		tool, ok := findToolCapability(capabilities, step.Tool)
		if !ok {
			return result, fmt.Errorf("project step %q capability is unavailable", step.ID)
		}
		invocation, err := client.InvokeCapability(ctx, tool, projectStepInput(step, requestID, true), requestID, step.SecretRefs, headers)
		if err != nil {
			result.Status = "failed"
			return result, fmt.Errorf("apply project step %q: %w", step.ID, err)
		}
		stepResult := projectStepResult{
			ID: step.ID, Tool: step.Tool, RequestID: requestID,
			Status: invocation.Result, RequiresApproval: invocation.RequiresApproval,
			Output: invocation.Output, RelatedIDs: invocation.RelatedIDs,
		}
		result.Steps = append(result.Steps, stepResult)
		status := strings.ToLower(strings.TrimSpace(invocation.Result))
		if status == "pending" || status == "pending_approval" || status == "approval_required" {
			result.Status = "pending_approval"
			result.Ready = false
			return result, fmt.Errorf("project step %q requires approval; downstream steps were not started", step.ID)
		}
		if status != "success" && status != "succeeded" {
			result.Status = "failed"
			result.Ready = false
			return result, fmt.Errorf("project step %q finished with result %q", step.ID, invocation.Result)
		}
		if !projectStepWait(step) {
			continue
		}
		domain, operationID, ok := projectOperation(step.Tool, invocation)
		if !ok {
			continue
		}
		waitCtx, cancel := context.WithTimeout(ctx, options.waitTimeout)
		operation, waitErr := waitForOperation(waitCtx, client, domain, operationID, options.interval)
		cancel()
		result.Steps[len(result.Steps)-1].Operation = &operation
		if waitErr != nil {
			result.Status = "failed"
			result.Ready = false
			return result, fmt.Errorf("wait for project step %q: %w", step.ID, waitErr)
		}
		if operation.NormalizedStatus != sohaapi.ComputeTaskStatusSucceeded {
			result.Status = "failed"
			result.Ready = false
			return result, fmt.Errorf("project step %q operation finished with status %s", step.ID, operation.NormalizedStatus)
		}
	}
	return result, nil
}

func projectStepInput(step projectStep, requestID string, trigger bool) map[string]any {
	raw, _ := json.Marshal(step.Input)
	input := map[string]any{}
	_ = json.Unmarshal(raw, &input)
	if trigger {
		input["idempotencyKey"] = requestID
	} else {
		delete(input, "idempotencyKey")
	}
	return input
}

func projectStepRequestID(projectName string, step projectStep) string {
	raw, _ := json.Marshal(struct {
		Project string      `json:"project"`
		Step    projectStep `json:"step"`
	}{Project: projectName, Step: step})
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("project-%x", sum[:16])
}

func projectStepWait(step projectStep) bool {
	return step.Wait == nil || *step.Wait
}

func projectOperation(toolName string, invocation ToolInvocationResult) (ComputeTaskDomain, string, bool) {
	var domain ComputeTaskDomain
	switch {
	case strings.HasPrefix(toolName, "virtualization."):
		domain = sohaapi.ComputeTaskDomainVirtualization
	case strings.HasPrefix(toolName, "docker."):
		domain = sohaapi.ComputeTaskDomainContainerRuntime
	default:
		return "", "", false
	}
	for _, values := range []map[string]any{invocation.RelatedIDs, projectOutputMap(invocation.Output)} {
		for _, key := range []string{"operationId", "taskId", "id"} {
			if value := strings.TrimSpace(fmt.Sprint(values[key])); value != "" && value != "<nil>" {
				return domain, value, true
			}
		}
	}
	return "", "", false
}

func projectOutputMap(value any) map[string]any {
	if output, ok := value.(map[string]any); ok {
		return output
	}
	return nil
}

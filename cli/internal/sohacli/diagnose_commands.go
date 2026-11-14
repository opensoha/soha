package sohacli

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
)

func runDiagnose(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("diagnose", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	toolName := fs.String("tool", "", "tool name to inspect")
	resourceName := fs.String("resource", "", "resource URI to inspect")
	promptName := fs.String("prompt", "", "prompt name to inspect")
	clusterCapabilityKey := fs.String("cluster-capability", "", "cluster capability key to inspect")
	aiClientID := fs.String("ai-client-id", "", "override AI client id for this diagnostic request")
	aiClientName := fs.String("ai-client", "", "override AI client display name for this diagnostic request")
	skillID := fs.String("skill-id", "", "override skill id for this diagnostic request")
	source := fs.String("source", "", "override source label for this diagnostic request")
	clientTarget := fs.String("client", "", "AI client setup to verify, for example codex")
	output := fs.String("output", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return err
	}
	_, name, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	clientName := firstNonEmptyString(*aiClientName, *clientTarget)
	manifest, err := gatewayClient(rt, profile).Capabilities(ctx, gatewayHeaders(profile, *aiClientID, clientName, *skillID, *source))
	if err != nil {
		return err
	}
	var clusterCapabilities []ClusterCapabilityMatrixEntry
	if strings.TrimSpace(*clusterCapabilityKey) != "" {
		clusterCapabilities, err = gatewayClient(rt, profile).ClusterCapabilities(ctx, nil)
		if err != nil {
			return err
		}
	}
	clientCheck, clientCheckErr := diagnoseClientSetup(ctx, rt, name, strings.TrimSpace(*clientTarget))
	if format == "json" {
		report := map[string]any{
			"profile": map[string]any{"name": name, "server": profile.ServerURL, "user": profile.UserName},
			"context": map[string]any{
				"aiClientId": firstNonEmptyString(*aiClientID, profile.AIClientID),
				"aiClient":   firstNonEmptyString(clientName, profile.AIClientName),
				"skillId":    firstNonEmptyString(*skillID, profile.SkillID),
				"source":     firstNonEmptyString(*source, profile.Source, "soha"),
			},
			"manifest": sanitizeCLIValue(manifest),
		}
		if clientCheck != nil {
			report["clientCheck"] = clientCheck
		}
		if len(clusterCapabilities) > 0 {
			report["clusterCapabilities"] = clusterCapabilities
		}
		if err := writePrettyJSON(rt.Out, report); err != nil {
			return err
		}
		return clientCheckErr
	}
	out := newCheckedWriter(rt.Out)
	if clientCheck != nil {
		out.Printf("clientCheck: %s\nclient: %s\n", clientCheck["status"], clientCheck["client"])
		details, _ := clientCheck["details"].([]string)
		for _, detail := range details {
			out.Printf("clientCheckDetail: %s\n", detail)
		}
	}
	out.Printf("profile: %s\nserver: %s\nuser: %s\n", name, profile.ServerURL, profile.UserName)
	out.Printf("tools: %d\nresources: %d\nprompts: %d\nskills: %d\n", len(manifest.Tools), len(manifest.Resources), len(manifest.Prompts), len(manifest.Skills))
	out.Printf("permissionKeys: %d\n", len(manifest.PermissionKeys))
	out.Printf("aiClientId: %s\naiClient: %s\nskillId: %s\nsource: %s\n", firstNonEmptyString(*aiClientID, profile.AIClientID), firstNonEmptyString(clientName, profile.AIClientName), firstNonEmptyString(*skillID, profile.SkillID), firstNonEmptyString(*source, profile.Source, "soha"))
	if strings.TrimSpace(*toolName) != "" {
		diagnoseTool(out, manifest, strings.TrimSpace(*toolName))
	}
	if strings.TrimSpace(*resourceName) != "" {
		diagnoseResource(out, manifest, strings.TrimSpace(*resourceName))
	}
	if strings.TrimSpace(*promptName) != "" {
		diagnosePrompt(out, manifest, strings.TrimSpace(*promptName))
	}
	if strings.TrimSpace(*clusterCapabilityKey) != "" {
		diagnoseClusterCapability(out, clusterCapabilities, strings.TrimSpace(*clusterCapabilityKey))
	}
	if len(manifest.Tools) == 0 {
		out.Println("hint: no tools visible; check ai.gateway.invoke, MCP tool grants, access policies, and skill bindings.")
	}
	if err := out.Err(); err != nil {
		return err
	}
	return clientCheckErr
}

func diagnoseClientSetup(ctx context.Context, rt Runtime, profileName, client string) (map[string]any, error) {
	if client == "" {
		return nil, nil
	}
	var stdout, stderr bytes.Buffer
	checkRuntime := rt
	checkRuntime.Out = &stdout
	checkRuntime.Err = &stderr
	err := runSetup(ctx, []string{"--client", client, "--mode", "mcp", "--check", "--profile", profileName}, checkRuntime)
	details := nonEmptyLines(stdout.String())
	status := "ok"
	if err != nil {
		status = "error"
		details = append(details, redactSensitiveText(err.Error()))
	}
	return map[string]any{"client": client, "status": status, "details": details}, err
}

func nonEmptyLines(value string) []string {
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, redactSensitiveText(line))
		}
	}
	return out
}

func diagnoseTool(out *checkedWriter, manifest Manifest, toolName string) {
	for _, tool := range manifest.Tools {
		if tool.Name == toolName {
			out.Printf("tool: %s\nriskLevel: %s\nrequiresApproval: %t\n", tool.Name, tool.RiskLevel, tool.RequiresApproval)
			out.Printf("domain: %s\naction: %s\nmcpAdapterId: %s\nmcpToolName: %s\n", tool.Domain, tool.Action, tool.MCPAdapterID, tool.MCPToolName)
			out.Printf("requiredPermissionKeys: %s\n", strings.Join(tool.PermissionKeys, ","))
			out.Printf("requiredScopes: %s\n", strings.Join(tool.RequiredScopes, ","))
			required, fields := toolSchemaSummary(tool.InputSchema)
			out.Printf("inputRequired: %s\n", strings.Join(required, ","))
			out.Printf("inputFields: %s\n", strings.Join(fields, ","))
			outputRequired, outputFields := toolSchemaSummary(tool.OutputSchema)
			out.Printf("outputRequired: %s\n", strings.Join(outputRequired, ","))
			out.Printf("outputFields: %s\n", strings.Join(outputFields, ","))
			out.Println("hint: if invocation is denied, inspect MCP tool grants, AI access policies, skill bindings, resource scopes, and domain permission keys.")
			return
		}
	}
	out.Printf("tool: %s not visible\n", toolName)
	out.Println("hint: check ai.gateway.invoke, domain permission keys, MCP tool grants, AI access policies, skill bindings, AI client context, and resource scopes.")
}

func toolSchemaSummary(schema map[string]any) ([]string, []string) {
	if len(schema) == 0 {
		return nil, nil
	}
	required := stringSliceFromAny(schema["required"])
	properties, _ := schema["properties"].(map[string]any)
	fields := make([]string, 0, len(properties))
	for key := range properties {
		key = strings.TrimSpace(key)
		if key != "" {
			fields = append(fields, key)
		}
	}
	sort.Strings(required)
	sort.Strings(fields)
	return required, fields
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := append([]string(nil), typed...)
		sort.Strings(out)
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

func diagnoseResource(out *checkedWriter, manifest Manifest, resourceName string) {
	for _, resource := range manifest.Resources {
		if resource.Name == resourceName {
			out.Printf("resource: %s\n", resource.Name)
			out.Printf("description: %s\n", resource.Description)
			out.Printf("requiredPermissionKeys: %s\n", strings.Join(resource.PermissionKeys, ","))
			out.Printf("requiredScopes: %s\n", strings.Join(resource.RequiredScopes, ","))
			contextRequired, contextFields := toolSchemaSummary(resource.ContextSchema)
			out.Printf("contextRequired: %s\n", strings.Join(contextRequired, ","))
			out.Printf("contextFields: %s\n", strings.Join(contextFields, ","))
			out.Println("hint: resource reads proxy to Gateway resources/read; check ai.gateway.invoke, resource permission keys, skill bindings, AI client context, and context scope fields.")
			return
		}
	}
	out.Printf("resource: %s not visible\n", resourceName)
	out.Println("hint: check ai.gateway.invoke, resource permission keys, skill bindings, AI client context, and the manifest resource URI.")
}

func diagnosePrompt(out *checkedWriter, manifest Manifest, promptName string) {
	for _, prompt := range manifest.Prompts {
		if prompt.Name == promptName {
			out.Printf("prompt: %s\n", prompt.Name)
			out.Printf("description: %s\n", prompt.Description)
			out.Printf("requiredPermissionKeys: %s\n", strings.Join(prompt.PermissionKeys, ","))
			out.Printf("requiredScopes: %s\n", strings.Join(prompt.RequiredScopes, ","))
			argumentRequired, argumentFields := toolSchemaSummary(prompt.ArgumentSchema)
			contextRequired, contextFields := toolSchemaSummary(prompt.ContextSchema)
			out.Printf("argumentRequired: %s\n", strings.Join(argumentRequired, ","))
			out.Printf("argumentFields: %s\n", strings.Join(argumentFields, ","))
			out.Printf("contextRequired: %s\n", strings.Join(contextRequired, ","))
			out.Printf("contextFields: %s\n", strings.Join(contextFields, ","))
			out.Println("hint: prompt reads proxy to Gateway prompts/get; check ai.gateway.invoke, prompt permission keys, skill bindings, AI client context, and prompt arguments/context.")
			return
		}
	}
	out.Printf("prompt: %s not visible\n", promptName)
	out.Println("hint: check ai.gateway.invoke, prompt permission keys, skill bindings, AI client context, and the manifest prompt name.")
}

func diagnoseClusterCapability(out *checkedWriter, items []ClusterCapabilityMatrixEntry, key string) {
	sortClusterCapabilities(items)
	for _, item := range items {
		if item.Key != key {
			continue
		}
		out.Printf("clusterCapability: %s\n", item.Key)
		out.Printf("label: %s\ncategory: %s\nriskLevel: %s\nrequiresApproval: %t\n", item.Label, item.Category, item.RiskLevel, item.RequiresApproval)
		out.Printf("requiredScopes: %s\ndocsUrl: %s\n", strings.Join(item.RequiredScopes, ","), item.DocsURL)
		out.Printf("directStatus: %s\ndirectReason: %s\n", item.Direct.Status, redactSensitiveText(item.Direct.Reason))
		out.Printf("agentStatus: %s\nagentReason: %s\n", item.Agent.Status, redactSensitiveText(item.Agent.Reason))
		if item.Agent.Status == "unsupported" || item.Agent.Status == "partial" {
			out.Println("hint: agent mode is not fully available; use the reason above to decide whether to switch to direct mode, request scope, or wait for Agent parity.")
		}
		return
	}
	out.Printf("clusterCapability: %s not found\n", key)
	out.Println("hint: run soha capabilities --domain platform --output names to list known cluster capability keys.")
}

func sortClusterCapabilities(items []ClusterCapabilityMatrixEntry) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Key < items[j].Key
	})
}

func clusterCapabilitySupportText(support ClusterCapabilityModeSupport) string {
	status := strings.TrimSpace(string(support.Status))
	if status == "" {
		status = "unknown"
	}
	if reason := strings.TrimSpace(support.Reason); reason != "" {
		return status + ":" + redactSensitiveText(reason)
	}
	return status
}

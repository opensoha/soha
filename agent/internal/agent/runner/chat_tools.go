package runner

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

const maxChatToolCalls = 8

type chatToolGrant struct {
	mu    sync.Mutex
	ctx   context.Context
	run   AgentRun
	calls int
}

func (r *Runner) beginChatToolGrant(ctx context.Context, run AgentRun) (string, func()) {
	if len(run.ToolBindings) == 0 {
		return "", func() {}
	}
	key := "soha-tools:" + rand.Text()
	grantCtx, cancel := context.WithCancel(ctx)
	r.chatToolGrants.Store(key, &chatToolGrant{ctx: grantCtx, run: run})
	return key, func() {
		cancel()
		r.chatToolGrants.Delete(key)
	}
}

// CallChatTool accepts only a trusted runtime grant. Model arguments cannot
// select a run, callback credential, principal, binding, or provider.
func (r *Runner) CallChatTool(ctx context.Context, key string, input sohaapi.AgentRunnerToolCallRequest) (AgentToolCallResult, error) {
	denied := errors.New("agent tool grant is invalid or exhausted")
	value, ok := r.chatToolGrants.Load(key)
	if !ok {
		return AgentToolCallResult{}, denied
	}
	grant, ok := value.(*chatToolGrant)
	if !ok || grant == nil {
		return AgentToolCallResult{}, denied
	}
	grant.mu.Lock()
	defer grant.mu.Unlock()
	if grant.ctx.Err() != nil || grant.calls >= maxChatToolCalls || ctx.Err() != nil {
		return AgentToolCallResult{}, denied
	}
	grant.calls++
	data, err := json.Marshal(input)
	if err != nil || len(data) > 16384 || strings.TrimSpace(input.ToolName) == "" {
		return AgentToolCallResult{}, denied
	}
	for _, binding := range grant.run.ToolBindings {
		if fmt.Sprint(binding["toolName"]) != input.ToolName {
			continue
		}
		toolCtx, cancel := context.WithTimeout(grant.ctx, 20*time.Second)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()
		arguments := map[string]any{
			"query": input.Input.Query, "clusterId": input.Input.ClusterID, "namespace": input.Input.Namespace, "nodeName": input.Input.NodeName, "limit": input.Input.Limit,
			"title": input.Input.Title, "artifactKind": input.Input.ArtifactKind, "format": input.Input.Format, "content": input.Input.Content, "baselineCitationId": input.Input.BaselineCitationID,
			"serviceName": input.Input.ServiceName,
		}
		if change := input.Input.Arguments; change != nil {
			arguments["toolName"] = string(input.Input.ToolName)
			if input.Input.ToolName == "delivery.applications.create" {
				arguments["arguments"] = map[string]any{"name": change.Name, "key": change.Key, "description": change.Description, "enabled": change.Enabled}
			} else {
				arguments["arguments"] = map[string]any{"applicationId": change.ApplicationID, "applicationEnvironmentId": change.ApplicationEnvironmentID, "action": string(change.Action)}
			}
		}
		result, ok := r.agentRunToolCall(toolCtx, grant.run, binding, arguments)
		if !ok {
			return AgentToolCallResult{}, errors.New("soha tool call was denied or failed")
		}
		return result, nil
	}
	return AgentToolCallResult{}, denied
}

package workflow

import (
	"context"
	"maps"
	"strconv"

	"github.com/google/uuid"
)

// NodeExecution is a server-only capability. HTTP metadata cannot supply it.
// The repository verifies it against the locked, current Run before creating work.
type NodeExecution struct {
	Scope        string
	RunID        string
	NodeID       string
	TargetID     string
	Stage        string
	Version      int64
	FencingToken int64
	LeaseOwner   string
	Attempt      int
}

type nodeExecutionKey struct{}

func WithNodeExecution(ctx context.Context, run Run, node NodeRun) context.Context {
	return context.WithValue(ctx, nodeExecutionKey{}, NodeExecution{Scope: run.Scope, RunID: run.ID, NodeID: node.NodeID, TargetID: node.TargetID, Stage: node.Stage, Version: run.Version, FencingToken: run.FencingToken, LeaseOwner: run.LeaseOwner, Attempt: 1})
}

func NodeExecutionFrom(ctx context.Context) (NodeExecution, bool) {
	node, ok := ctx.Value(nodeExecutionKey{}).(NodeExecution)
	return node, ok
}

func (n NodeExecution) ResourceID(kind string) string {
	return kind + ":" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(n.RunID+"\x00"+n.NodeID+"\x00"+strconv.Itoa(n.Attempt)+"\x00"+kind)).String()
}

func (n NodeExecution) Metadata(existing map[string]any) map[string]any {
	result := maps.Clone(existing)
	if result == nil {
		result = map[string]any{}
	}
	scope := n.Scope
	if scope == "" {
		scope = ScopeDeliveryBatch
	}
	result["workflowScope"], result["workflowRunId"], result["workflowNodeId"] = scope, n.RunID, n.NodeID
	result["workflowAttempt"], result["workflowFencingToken"] = n.Attempt, n.FencingToken
	return result
}

package multiagent

import "testing"

func testBudget() Budget {
	return Budget{MaxSteps: 10, MaxTokens: 10_000, MaxCost: 1, DeadlineSecs: 60}
}

func TestServiceEnforcesPermissionIntersectionAndDependencies(t *testing.T) {
	service, _ := NewService(NewMemoryStore())
	_, err := service.Create(t.Context(), Plan{ID: "plan-1", CoordinatorRef: "coordinator-v1", PrincipalPermissions: []string{"forged.permission"}, SharedBudget: testBudget(), Subtasks: []Subtask{{ID: "research", AgentProfileRef: "researcher", Input: "research", PermissionKeys: []string{"knowledge.search"}, Budget: testBudget()}, {ID: "write", AgentProfileRef: "writer", Input: "summarize", DependsOn: []string{"research"}, PermissionKeys: []string{"knowledge.search"}, Budget: testBudget()}}}, []string{"knowledge.search"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompleteSubtask(t.Context(), "plan-1", "write", "artifact:write"); err == nil {
		t.Fatal("expected dependency error")
	}
	if _, err := service.CompleteSubtask(t.Context(), "plan-1", "research", "artifact:research"); err != nil {
		t.Fatal(err)
	}
	completed, err := service.CompleteSubtask(t.Context(), "plan-1", "write", "artifact:write")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || len(completed.MergedOutputRefs) != 2 {
		t.Fatalf("completed = %#v", completed)
	}
}

func TestServiceRejectsPermissionEscalationAndPropagatesCancel(t *testing.T) {
	service, _ := NewService(NewMemoryStore())
	_, err := service.Create(t.Context(), Plan{ID: "bad", CoordinatorRef: "coordinator-v1", PrincipalPermissions: []string{"cluster.write"}, SharedBudget: testBudget(), Subtasks: []Subtask{{ID: "admin", AgentProfileRef: "admin", Input: "write", PermissionKeys: []string{"cluster.write"}, Budget: testBudget()}}}, []string{"knowledge.search"})
	if err == nil {
		t.Fatal("expected permission escalation error")
	}
	created, err := service.Create(t.Context(), Plan{ID: "plan-1", CoordinatorRef: "coordinator-v1", PrincipalPermissions: []string{"forged.permission"}, SharedBudget: testBudget(), Subtasks: []Subtask{{ID: "research", AgentProfileRef: "researcher", Input: "research", PermissionKeys: []string{"knowledge.search"}, Budget: testBudget()}}}, []string{"knowledge.search"})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.PrincipalPermissions) != 1 || created.PrincipalPermissions[0] != "knowledge.search" {
		t.Fatalf("principal permissions = %#v", created.PrincipalPermissions)
	}
	cancelled, err := service.Cancel(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "cancelled" || cancelled.Subtasks[0].Status != "cancelled" {
		t.Fatalf("cancelled = %#v", cancelled)
	}
}

package abac

import legacy "github.com/kubecrux/kubecrux/internal/policy"

type Evaluator = legacy.Engine

func New() *Evaluator {
	return legacy.NewEngine()
}

package abac

import legacy "github.com/soha/soha/internal/policy"

type Evaluator = legacy.Engine

func New() *Evaluator {
	return legacy.NewEngine()
}

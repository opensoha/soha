package abac

import legacy "github.com/opensoha/soha/internal/policy"

type Evaluator = legacy.Engine

func New() *Evaluator {
	return legacy.NewEngine()
}

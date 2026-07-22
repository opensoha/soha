package runtimeconfig

import (
	"errors"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

var (
	ErrNotFound        = errors.New("runtime configuration not found")
	ErrVersionConflict = errors.New("runtime configuration version conflict")
)

type State struct {
	Version          int64
	ActiveRevisionID string
	Overrides        map[string]any
	UpdatedBy        string
	UpdatedAt        time.Time
}

type Revision struct {
	ID                   string
	Version              int64
	Status               sohaapi.RuntimeConfigApplicationStatus
	Changes              []sohaapi.RuntimeConfigChange
	Snapshot             map[string]any
	Actor                string
	Reason               string
	RollbackOfRevisionID string
	CreatedAt            time.Time
}

type Application struct {
	ID         string
	RevisionID string
	Version    int64
	Status     sohaapi.RuntimeConfigApplicationStatus
	Items      []sohaapi.RuntimeConfigAppliedItem
	Error      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Commit struct {
	ExpectedVersion int64
	Revision        Revision
	Application     Application
}

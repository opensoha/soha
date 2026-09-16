package workflow

// TargetValidationError locates a safe business validation error in a definition.
// The underlying error retains its category and user-facing business code.
type TargetValidationError struct {
	Index int
	Err   error
}

func (e *TargetValidationError) Error() string { return e.Err.Error() }
func (e *TargetValidationError) Unwrap() error { return e.Err }

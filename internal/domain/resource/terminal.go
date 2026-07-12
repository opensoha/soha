package resource

type TerminalSize struct {
	Width  uint16
	Height uint16
}

type TerminalSizeQueue interface {
	Next() *TerminalSize
}

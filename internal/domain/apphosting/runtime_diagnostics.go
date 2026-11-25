package apphosting

import "time"

// RuntimeSignal is one allowlisted runtime condition observed for a hosted revision.
type RuntimeSignal struct {
	Code string
	At   time.Time
}

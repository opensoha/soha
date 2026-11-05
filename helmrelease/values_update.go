package helmrelease

import (
	"encoding/json"
	"errors"
	"math"
)

type ValuesUpdateRequest struct {
	Content          string                 `json:"content"`
	ExpectedRevision ValuesExpectedRevision `json:"expectedRevision,omitempty"`
}

// ValuesExpectedRevision uses zero internally for legacy requests that omit the field.
type ValuesExpectedRevision int

func (r ValuesExpectedRevision) Valid() bool {
	return r > 0 && r < math.MaxInt32
}

func (r *ValuesExpectedRevision) UnmarshalJSON(data []byte) error {
	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if !ValuesExpectedRevision(value).Valid() {
		return errors.New("expectedRevision must be between 1 and 2147483646")
	}
	*r = ValuesExpectedRevision(value)
	return nil
}

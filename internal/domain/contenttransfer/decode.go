package contenttransfer

import (
	"bytes"
	"encoding/json"
	"errors"
)

func (c *Config) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if v, ok := fields["enabled"]; !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		return errors.New("explicit destination enabled decision is required")
	}
	type plain Config
	var p plain
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return err
	}
	*c = Config(p)
	return nil
}

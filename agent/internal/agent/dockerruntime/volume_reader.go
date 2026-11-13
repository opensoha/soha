package dockerruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
	"unicode/utf8"

	protocol "github.com/opensoha/soha-contracts/docker/runtime"
)

// VolumeReaderMain is the isolated image process entry. It never bootstraps the
// Agent, reads its configuration, or loads control-plane credentials. The caller
// must exit after this function returns, including when a filesystem blocks.
func VolumeReaderMain(in io.Reader, out io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runVolumeReader(ctx, in, out) }()
	select {
	case err := <-done:
		if err != nil {
			return 1
		}
		return 0
	case <-ctx.Done():
		return 124
	}
}

func runVolumeReader(ctx context.Context, in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(in, (16<<10)+1))
	if err != nil || len(raw) > 16<<10 || !utf8.Valid(raw) {
		return errors.New("volume reader request exceeds its limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request protocol.ReadRequest
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid volume reader request")
	}
	if request.Action != "files.list" && request.Action != "files.read" {
		return errors.New("unsupported volume reader action")
	}
	req, err := request.Normalized(request.Action)
	if err != nil {
		return err
	}
	result, err := readContainerVolume(ctx, req)
	if err != nil || result.ValidateFor(req) != nil {
		return errors.New("volume read could not be confirmed")
	}
	return json.NewEncoder(out).Encode(result)
}

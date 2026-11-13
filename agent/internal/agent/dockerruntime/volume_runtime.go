package dockerruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	protocol "github.com/opensoha/soha/contracts/docker/runtime"
)

var ErrVolumeReaderUnsupported = errors.New("safe Docker volume reader is unavailable")
var fullEngineID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Docker 29 omits absent image config keys in its template map. index returns
// nil for an absent key; field access would reject a valid scratch image.
const readerImageInspectionFormat = `{"id":{{json .Id}},"os":{{json .Os}},"volumes":{{json (index .Config "Volumes")}},"env":{{json (index .Config "Env")}},"labels":{{json (index .Config "Labels")}}}`

const (
	readerOwnerLabel     = "io.opensoha.volume-reader-owner"
	readerNonceLabel     = "io.opensoha.volume-reader-nonce"
	readerDeadlineLabel  = "io.opensoha.volume-reader-deadline"
	readerIssuedLabel    = "io.opensoha.volume-reader-issued"
	readerContainerLabel = "io.opensoha.volume-reader-container"
	readerProtocolLabel  = "io.opensoha.volume-reader-protocol"
)

// VolumeReader owns only disposable, identity-checked helper containers. Image
// and owner are explicit installation configuration; both are empty by default.
type VolumeReader struct {
	image         string
	owner         string
	slots         chan struct{}
	observe       func(context.Context, int, ...string) ([]byte, error)
	cleanupImages map[string]bool
}

func NewVolumeReader(image, owner string, cleanupImages ...string) *VolumeReader {
	r := &VolumeReader{slots: make(chan struct{}, 4), observe: observe, cleanupImages: map[string]bool{}}
	id, err := uuid.Parse(owner)
	if err == nil && id != uuid.Nil && id.String() == owner && strings.HasPrefix(image, "sha256:") && fullEngineID.MatchString(strings.TrimPrefix(image, "sha256:")) {
		r.image, r.owner = image, owner
		r.cleanupImages[image] = true
	}
	if len(cleanupImages) > 4 {
		r.image, r.owner = "", ""
		return r
	}
	for _, old := range cleanupImages {
		if !strings.HasPrefix(old, "sha256:") || !fullEngineID.MatchString(strings.TrimPrefix(old, "sha256:")) || r.cleanupImages[old] {
			r.image, r.owner = "", ""
			return r
		}
		r.cleanupImages[old] = true
	}
	return r
}

func (r *VolumeReader) Enabled() bool { return r != nil && r.image != "" && r.owner != "" }

func (r *VolumeReader) ReadVolumes(ctx context.Context, request protocol.ReadRequest) (protocol.ReadResult, error) {
	req, err := request.Normalized("volumes")
	if err != nil {
		return protocol.ReadResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	item, err := InspectContainer(ctx, req.Container)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	reason := "safe volume reader is not configured"
	if r.Enabled() {
		reason = "container configuration is unsupported by the safe reader"
		if safeReaderTarget(item) {
			reason = "pinned safe reader image is unavailable"
			if r.verifyImage(ctx) == nil {
				reason = ""
			}
		}
	}
	result := newReadResult(req)
	for _, mount := range item.Mounts {
		why := reason
		if mount.Type != "volume" && mount.Type != "bind" {
			why = "mount type is unsupported by the safe reader"
		}
		result.Volumes = append(result.Volumes, protocol.Volume{Target: mount.Destination, Type: mount.Type, ReadOnly: !mount.RW, BrowseSupported: why == "", UnsupportedReason: why})
	}
	if result.ValidateFor(req) != nil {
		return protocol.ReadResult{}, errors.New("docker mount metadata could not be confirmed")
	}
	return result, nil
}

func safeReaderTarget(item Container) bool {
	if item.Platform != "linux" || !item.Running || item.Privileged || item.PIDMode != "" || item.UserNSMode != "" {
		return false
	}
	for _, capability := range item.CapAdd {
		value := strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(capability)), "CAP_")
		if value == "ALL" || value == "SYS_ADMIN" || value == "SYS_PTRACE" {
			return false
		}
	}
	return true
}

func (r *VolumeReader) verifyImage(ctx context.Context) error {
	raw, err := r.observe(ctx, 1<<20, "image", "inspect", "--format", readerImageInspectionFormat, r.image)
	if err != nil {
		return ErrVolumeReaderUnsupported
	}
	var item struct {
		ID      string            `json:"id"`
		OS      string            `json:"os"`
		Volumes map[string]any    `json:"volumes"`
		Env     []string          `json:"env"`
		Labels  map[string]string `json:"labels"`
	}
	if json.Unmarshal(raw, &item) != nil || item.ID != r.image || item.OS != "linux" || len(item.Volumes) != 0 || len(item.Env) != 0 || item.Labels[readerProtocolLabel] != protocol.ReadProtocol {
		return ErrVolumeReaderUnsupported
	}
	return nil
}

func (r *VolumeReader) ReadFiles(ctx context.Context, request protocol.ReadRequest) (result protocol.ReadResult, err error) {
	if !r.Enabled() {
		return result, ErrVolumeReaderUnsupported
	}
	if request.Action != "files.list" && request.Action != "files.read" {
		return result, errors.New("unsupported volume read action")
	}
	req, err := request.Normalized(request.Action)
	if err != nil {
		return result, err
	}
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-ctx.Done():
		return result, ctx.Err()
	default:
		return result, errors.New("safe volume reader concurrency limit reached")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := r.verifyImage(ctx); err != nil {
		return result, err
	}
	if err := confirmReaderMount(ctx, req); err != nil {
		return result, err
	}
	return r.readWithHelper(ctx, req)
}

func (r *VolumeReader) readWithHelper(ctx context.Context, req protocol.ReadRequest) (result protocol.ReadResult, err error) {
	nonce := uuid.NewString()
	name := r.helperName(nonce)
	// Install cleanup before the one and only create attempt. A lost create
	// response is resolved by a read-only exact-name/nonce lookup, never retry.
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if failure := r.removeHelper(cleanup, name, nonce); failure != nil {
			result = protocol.ReadResult{}
			err = errors.New("safe volume reader cleanup could not be confirmed")
		}
	}()
	raw, err := r.observe(ctx, 128, r.createArguments(name, nonce, req.Container.ContainerID)...)
	if err != nil || !fullEngineID.MatchString(strings.TrimSpace(string(raw))) {
		return result, errors.New("safe volume reader could not be created")
	}
	helper, err := r.findHelper(ctx, name, nonce)
	if err != nil || helper == nil || helper.ID != strings.TrimSpace(string(raw)) {
		return result, errors.New("safe volume reader identity could not be confirmed")
	}
	if helper.Config.Labels[readerContainerLabel] != req.Container.ContainerID || helper.HostConfig.PidMode != "container:"+req.Container.ContainerID {
		return result, errors.New("safe volume reader PID target differs from the original request")
	}
	if err := confirmReaderMount(ctx, req); err != nil {
		return result, err
	}
	return r.startHelper(ctx, helper.ID, req)
}

func confirmReaderMount(ctx context.Context, req protocol.ReadRequest) error {
	item, err := InspectContainer(ctx, req.Container)
	if err != nil {
		return err
	}
	if !safeReaderTarget(item) {
		return ErrVolumeReaderUnsupported
	}
	count := 0
	for _, mount := range item.Mounts {
		if mount.Destination == req.Target && (mount.Type == "volume" || mount.Type == "bind") {
			count++
		}
	}
	if count != 1 {
		return ErrVolumeReaderUnsupported
	}
	return nil
}

func (r *VolumeReader) helperName(nonce string) string {
	return "soha-volume-reader-" + r.owner + "-" + strings.ReplaceAll(nonce, "-", "")
}

func (r *VolumeReader) createArguments(name, nonce, cid string) []string {
	issued := time.Now().Unix()
	return []string{"container", "create", "--pull=never", "--interactive", "--name", name,
		"--label", readerOwnerLabel + "=" + r.owner, "--label", readerNonceLabel + "=" + nonce,
		"--label", readerDeadlineLabel + "=" + strconv.FormatInt(issued+60, 10),
		"--label", readerIssuedLabel + "=" + strconv.FormatInt(issued, 10),
		"--label", readerContainerLabel + "=" + cid, "--label", readerProtocolLabel + "=" + protocol.ReadProtocol,
		"--rm", "--read-only", "--network", "none", "--ipc", "none", "--pid", "container:" + cid,
		"--cap-drop", "ALL", "--cap-add", "SYS_PTRACE", "--cap-add", "DAC_READ_SEARCH",
		"--security-opt", "no-new-privileges:true", "--pids-limit", "16", "--memory", "64m", "--cpus", "0.25",
		"--user", "0:0", "--workdir", "/", "--no-healthcheck", "--entrypoint", "/soha-agent", r.image, "volume-reader"}
}

func (r *VolumeReader) startHelper(ctx context.Context, id string, req protocol.ReadRequest) (protocol.ReadResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	// #nosec G204 -- fixed command and flags; id is the fully inspected, owner/image/nonce-bound helper container ID, without a shell.
	command := exec.CommandContext(ctx, "docker", "container", "start", "--attach", "--interactive", id)
	command.WaitDelay = 2 * time.Second
	command.Stdin = bytes.NewReader(body)
	output := &boundedOutput{limit: 8 << 20}
	command.Stdout, command.Stderr = output, io.Discard
	if command.Run() != nil {
		return protocol.ReadResult{}, errors.New("safe volume reader did not complete")
	}
	if !utf8.Valid(output.Bytes()) {
		return protocol.ReadResult{}, errors.New("safe volume reader returned invalid JSON text")
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	var result protocol.ReadResult
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.ValidateFor(req) != nil {
		return protocol.ReadResult{}, errors.New("safe volume reader response could not be confirmed")
	}
	return result, nil
}

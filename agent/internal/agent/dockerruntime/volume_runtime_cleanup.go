package dockerruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	protocol "github.com/opensoha/soha/contracts/docker/runtime"
)

type readerHelper struct {
	ID      string `json:"Id"`
	Image   string
	Name    string
	Created string
	Mounts  []Mount
	Config  struct {
		Labels     map[string]string
		Env        []string
		Volumes    map[string]any
		Entrypoint []string
		Cmd        []string
		User       string
		WorkingDir string
	}
	HostConfig struct {
		Privileged      bool
		ReadonlyRootfs  bool
		AutoRemove      bool
		Binds           []string
		VolumesFrom     []string
		Devices         []any
		DeviceRequests  []any
		PortBindings    map[string]any
		PublishAllPorts bool
		NetworkMode     string
		IpcMode         string
		PidMode         string
		UsernsMode      string
		CapAdd          []string
		CapDrop         []string
		SecurityOpt     []string
		PidsLimit       int64
		Memory          int64
		NanoCpus        int64
	}
}

func (r *VolumeReader) inspectHelper(ctx context.Context, id string) (*readerHelper, error) {
	if !fullEngineID.MatchString(id) {
		return nil, errors.New("invalid helper container ID")
	}
	raw, err := r.observe(ctx, 1<<20, "container", "inspect", "--format", "{{json .}}", id)
	if err != nil {
		return nil, err
	}
	var item readerHelper
	if json.Unmarshal(raw, &item) != nil || item.ID != id {
		return nil, errors.New("invalid helper container observation")
	}
	return &item, nil
}

func (r *VolumeReader) findHelper(ctx context.Context, name, nonce string) (*readerHelper, error) {
	if name != r.helperName(nonce) {
		return nil, errors.New("invalid helper lookup")
	}
	raw, err := r.observe(ctx, 16<<10, "container", "ls", "--all", "--no-trunc", "--filter", "name=^/"+name+"$", "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(raw))
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) != 1 {
		return nil, errors.New("ambiguous helper lookup")
	}
	item, err := r.inspectHelper(ctx, ids[0])
	if err != nil {
		return nil, err
	}
	if err := r.ownsHelper(item, nonce); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *VolumeReader) ownsHelper(item *readerHelper, nonce string) error {
	if err := r.validateHelperIdentity(item, nonce); err != nil {
		return err
	}
	if err := validateHelperLease(item); err != nil {
		return err
	}
	if err := validateHelperProcess(item); err != nil {
		return err
	}
	return validateHelperIsolation(item)
}

func (r *VolumeReader) validateHelperIdentity(item *readerHelper, nonce string) error {
	id, err := uuid.Parse(nonce)
	labels := item.Config.Labels
	if err != nil || id == uuid.Nil || id.String() != nonce || !fullEngineID.MatchString(item.ID) || item.Image != r.image || item.Name != "/"+r.helperName(nonce) || labels[readerOwnerLabel] != r.owner || labels[readerNonceLabel] != nonce || labels[readerProtocolLabel] != protocol.ReadProtocol || !fullEngineID.MatchString(labels[readerContainerLabel]) {
		return errors.New("helper container ownership differs from this reader")
	}
	return nil
}

func validateHelperLease(item *readerHelper) error {
	created, err := time.Parse(time.RFC3339Nano, item.Created)
	value := item.Config.Labels[readerDeadlineLabel]
	deadline, numberErr := strconv.ParseInt(value, 10, 64)
	issuedText := item.Config.Labels[readerIssuedLabel]
	issued, issuedErr := strconv.ParseInt(issuedText, 10, 64)
	// A daemon can create after the caller has timed out. Validate the fixed
	// issuer lease, not a minimum remaining duration relative to daemon time.
	if err != nil || created.Unix() <= 0 || numberErr != nil || issuedErr != nil || issued <= 0 || deadline <= issued || strconv.FormatInt(issued, 10) != issuedText || strconv.FormatInt(deadline, 10) != value || deadline-issued != 60 {
		return errors.New("helper lease is invalid")
	}
	return nil
}

func validateHelperProcess(item *readerHelper) error {
	if len(item.Mounts) != 0 || len(item.Config.Env) != 0 || len(item.Config.Volumes) != 0 || item.Config.User != "0:0" || item.Config.WorkingDir != "/" || len(item.Config.Entrypoint) != 1 || item.Config.Entrypoint[0] != "/soha-agent" || len(item.Config.Cmd) != 1 || item.Config.Cmd[0] != "volume-reader" {
		return errors.New("helper process or mount configuration changed")
	}
	return nil
}

func validateHelperIsolation(item *readerHelper) error {
	if !helperHostIsolated(item) {
		return errors.New("helper host isolation changed")
	}
	if !helperNamespacesMatch(item) {
		return errors.New("helper namespace or resource limits changed")
	}
	h := item.HostConfig
	if !sameCapabilities(h.CapDrop, []string{"ALL"}) || !sameCapabilities(h.CapAdd, []string{"SYS_PTRACE", "DAC_READ_SEARCH"}) || len(h.SecurityOpt) != 1 || (h.SecurityOpt[0] != "no-new-privileges" && h.SecurityOpt[0] != "no-new-privileges:true") {
		return errors.New("helper capabilities changed")
	}
	return nil
}

func helperHostIsolated(item *readerHelper) bool {
	h := item.HostConfig
	return !h.Privileged && h.ReadonlyRootfs && h.AutoRemove && len(h.Binds) == 0 && len(h.VolumesFrom) == 0 && len(h.Devices) == 0 && len(h.DeviceRequests) == 0 && len(h.PortBindings) == 0 && !h.PublishAllPorts
}

func helperNamespacesMatch(item *readerHelper) bool {
	h := item.HostConfig
	return h.NetworkMode == "none" && h.IpcMode == "none" && h.PidMode == "container:"+item.Config.Labels[readerContainerLabel] && h.UsernsMode == "" && h.PidsLimit == 16 && h.Memory == 64<<20 && h.NanoCpus == 250000000
}

func sameCapabilities(actual, wanted []string) bool {
	if len(actual) != len(wanted) {
		return false
	}
	seen := map[string]bool{}
	for _, value := range actual {
		key := strings.TrimPrefix(strings.ToUpper(value), "CAP_")
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	for _, value := range wanted {
		if !seen[value] {
			return false
		}
	}
	return true
}

func (r *VolumeReader) removeHelper(ctx context.Context, name, nonce string) error {
	item, err := r.findHelper(ctx, name, nonce)
	if err != nil || item == nil {
		return err
	}
	// The full immutable ID and every ownership/isolation field were checked.
	// A concurrent auto-remove is successful only after confirming absence.
	_, _ = r.observe(ctx, 1024, "container", "rm", "--force", item.ID)
	remaining, err := r.findHelper(ctx, name, nonce)
	if err != nil || remaining != nil {
		return errors.New("helper removal could not be confirmed")
	}
	return nil
}

// Collect reconciles only expired helpers of this configured owner and image.
// It also covers a process crash after create but before start. A stopped
// created helper has no running process; it is reclaimed after its short lease.
func (r *VolumeReader) Collect(ctx context.Context, now time.Time) error {
	if !r.Enabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	raw, err := r.observe(ctx, 16<<10, "container", "ls", "--all", "--no-trunc", "--filter", "label="+readerOwnerLabel+"="+r.owner, "--filter", "label="+readerProtocolLabel+"="+protocol.ReadProtocol, "--format", "{{.ID}}")
	if err != nil {
		return err
	}
	ids := strings.Fields(string(raw))
	if len(ids) > 64 {
		return errors.New("helper inventory exceeds its limit")
	}
	unconfirmed := false
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		item, err := r.inspectHelper(ctx, id)
		if err != nil {
			unconfirmed = true
			continue
		}
		if !r.cleanupImages[item.Image] {
			unconfirmed = true
			continue
		}
		// Older image IDs require an explicit cleanup-only grant. They are
		// never selected for new helper execution.
		collector := *r
		collector.image = item.Image
		nonce := item.Config.Labels[readerNonceLabel]
		if err := collector.ownsHelper(item, nonce); err != nil {
			unconfirmed = true
			continue
		}
		deadline, _ := strconv.ParseInt(item.Config.Labels[readerDeadlineLabel], 10, 64)
		if deadline > now.Unix() {
			continue
		}
		if err := collector.removeHelper(ctx, strings.TrimPrefix(item.Name, "/"), nonce); err != nil {
			unconfirmed = true
		}
	}
	if unconfirmed {
		return errors.New("one or more owned volume reader releases could not be confirmed")
	}
	return nil
}

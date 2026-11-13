package personalguest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
)

type DataPlan = guest.DataPlan

const dataPlanVersion = guest.DataPlanVersion

var dataDevice = regexp.MustCompile(`^/dev/[a-zA-Z0-9_-]+$`)

type dataBlock struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Size        int64       `json:"size"`
	Serial      string      `json:"serial"`
	FSType      string      `json:"fstype"`
	UUID        string      `json:"uuid"`
	ReadOnly    bool        `json:"ro"`
	DeviceID    string      `json:"maj:min"`
	Mountpoints []string    `json:"mountpoints"`
	Children    []dataBlock `json:"children"`
}

type dataMount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	FSType   string `json:"fstype"`
	Options  string `json:"options"`
	DeviceID string `json:"maj:min"`
	FSRoot   string `json:"fsroot"`
}

type dataReceipt struct {
	Plan         DataPlan `json:"plan"`
	Stage        string   `json:"stage"`
	FilesystemID string   `json:"filesystemId"`
	DeviceID     string   `json:"deviceId"`
}

type DataResult = guest.DataResult

type dataInitializer struct {
	run       func(context.Context, string, ...string) ([]byte, error)
	authorize func(context.Context, DataPlan) error
	load      func() (dataReceipt, bool, error)
	save      func(dataReceipt) error
	mkdir     func(string) error
	base      string
	now       func() time.Time
}

func (i dataInitializer) initialize(ctx context.Context, plan DataPlan) (DataResult, error) {
	if err := plan.Validate(i.now()); err != nil {
		return DataResult{}, err
	}
	if i.authorize == nil || i.authorize(ctx, plan) != nil {
		return DataResult{}, errors.New("current personal data binding authority is unavailable")
	}
	receipt, found, err := i.load()
	if err != nil {
		return DataResult{}, err
	}
	if found && !receipt.matches(plan) {
		return DataResult{}, errors.New("personal data receipt identity changed")
	}
	// Keep the original format operation; use the freshly authorized lease for reconciliation.
	if found {
		receipt.Plan.Binding = plan.Binding
	}
	disk, mounts, err := i.observe(ctx, plan)
	if err != nil {
		return DataResult{}, err
	}
	if !found {
		if err := i.requireBlank(ctx, disk, mounts); err != nil {
			return DataResult{}, err
		}
		receipt = dataReceipt{Plan: plan, Stage: "reserved", FilesystemID: plan.Binding.DataRef, DeviceID: disk.DeviceID}
		if err := i.save(receipt); err != nil {
			return DataResult{}, err
		}
	}
	if receipt.DeviceID != disk.DeviceID {
		// A kernel device number can change after reboot, but only an already
		// confirmed filesystem UUID permits this receipt to follow the disk.
		if receipt.Stage != "complete" || disk.FSType != "xfs" || disk.UUID != receipt.FilesystemID {
			return DataResult{}, errors.New("personal data device changed before initialization completed")
		}
	}
	if receipt.Stage == "reserved" {
		if err := i.format(ctx, &receipt); err != nil {
			return DataResult{}, err
		}
	}
	// An uncertain mkfs is never retried, even when the disk still looks blank.
	// A matching UUID permits inspection/recovery of the original operation.
	current := receipt
	current.Plan = plan
	result, err := i.mountAndLimit(ctx, current)
	if err != nil {
		return DataResult{}, err
	}
	result.Reconciled = found
	receipt.Stage = "complete"
	if err := i.save(receipt); err != nil {
		return DataResult{}, err
	}
	return result, nil
}

func validDataStage(stage string) bool {
	return stage == "reserved" || stage == "format_dispatched" || stage == "complete"
}

func (r dataReceipt) matches(plan DataPlan) bool {
	return r.Plan.Binding.SameWorkspace(plan.Binding) && r.Plan.DiskQuotaBytes == plan.DiskQuotaBytes && (r.Stage == "complete" || r.Plan.OperationID == plan.OperationID) && r.FilesystemID == plan.Binding.DataRef && validDataStage(r.Stage)
}

func (i dataInitializer) observe(ctx context.Context, plan DataPlan) (dataBlock, []dataMount, error) {
	// Read the device directly; udev can still cache the pre-mkfs filesystem identity.
	output, err := i.run(ctx, "/usr/bin/lsblk", "--json", "--bytes", "--paths", "--output", "NAME,TYPE,SIZE,SERIAL,FSTYPE,UUID,MOUNTPOINTS,RO,MAJ:MIN")
	if err != nil {
		return dataBlock{}, nil, err
	}
	var blocks struct {
		Blocks []dataBlock `json:"blockdevices"`
	}
	if json.Unmarshal(output, &blocks) != nil {
		return dataBlock{}, nil, errors.New("invalid personal data block-device observation")
	}
	disk, err := selectDataBlock(blocks.Blocks, plan.Binding.DataDiskSerial, plan.Binding.DataDiskBytes)
	if err != nil {
		return dataBlock{}, nil, err
	}
	// Keep the hardware serial from udev; libblkid probes only the selected disk's filesystem.
	// shortcut: requires util-linux --properties-by; qualify older Linux images before supporting them.
	output, err = i.run(ctx, "/usr/bin/lsblk", "--json", "--paths", "--properties-by", "blkid", "--output", "NAME,FSTYPE,UUID", disk.Name)
	if err != nil {
		return dataBlock{}, nil, err
	}
	var fresh struct {
		Blocks []dataBlock `json:"blockdevices"`
	}
	if json.Unmarshal(output, &fresh) != nil || len(fresh.Blocks) != 1 || fresh.Blocks[0].Name != disk.Name || len(fresh.Blocks[0].Children) != 0 {
		return dataBlock{}, nil, errors.New("invalid direct personal data filesystem observation")
	}
	disk.FSType, disk.UUID = fresh.Blocks[0].FSType, fresh.Blocks[0].UUID
	output, err = i.run(ctx, "/usr/bin/findmnt", "--json", "--list", "--output", "SOURCE,TARGET,FSTYPE,OPTIONS,MAJ:MIN,FSROOT")
	if err != nil {
		return dataBlock{}, nil, err
	}
	var view struct {
		Mounts []dataMount `json:"filesystems"`
	}
	if json.Unmarshal(output, &view) != nil || len(view.Mounts) == 0 {
		return dataBlock{}, nil, errors.New("invalid personal data mount observation")
	}
	if err := checkDataRootMount(disk, view.Mounts); err != nil {
		return dataBlock{}, nil, err
	}
	return disk, view.Mounts, nil
}

func checkDataRootMount(disk dataBlock, mounts []dataMount) error {
	roots := 0
	for _, mount := range mounts {
		if mount.Target != "/" {
			continue
		}
		roots++
		if mount.DeviceID == "" || blockContainsDevice(disk, mount.DeviceID) {
			return errors.New("personal data disk contains the root filesystem or root identity is unknown")
		}
	}
	if roots != 1 {
		return errors.New("root mount identity is missing or ambiguous")
	}
	return nil
}

func selectDataBlock(blocks []dataBlock, serial string, size int64) (dataBlock, error) {
	var matches []dataBlock
	var walk func([]dataBlock)
	walk = func(items []dataBlock) {
		for _, block := range items {
			if block.Serial == serial {
				matches = append(matches, block)
			}
			walk(block.Children)
		}
	}
	walk(blocks)
	if len(matches) != 1 {
		return dataBlock{}, errors.New("personal data disk identity is missing or ambiguous")
	}
	disk := matches[0]
	if disk.Type != "disk" || disk.ReadOnly || disk.Size != size || !dataDevice.MatchString(disk.Name) || disk.DeviceID == "" || len(disk.Children) != 0 {
		return dataBlock{}, errors.New("personal data disk geometry or layout changed")
	}
	return disk, nil
}

func blockContainsDevice(block dataBlock, id string) bool {
	if block.DeviceID == id {
		return true
	}
	for _, child := range block.Children {
		if blockContainsDevice(child, id) {
			return true
		}
	}
	return false
}

func (i dataInitializer) requireBlank(ctx context.Context, disk dataBlock, mounts []dataMount) error {
	if disk.FSType != "" || disk.UUID != "" || len(disk.Children) != 0 {
		return errors.New("personal data disk has existing filesystem or partition metadata")
	}
	for _, point := range disk.Mountpoints {
		if point != "" {
			return errors.New("personal data disk is already mounted")
		}
	}
	for _, mount := range mounts {
		if blockContainsDevice(disk, mount.DeviceID) {
			return errors.New("personal data disk is already in use")
		}
	}
	output, err := i.run(ctx, "/usr/sbin/wipefs", "--no-act", "--json", disk.Name)
	if err != nil {
		return err
	}
	var probe struct {
		Signatures []json.RawMessage `json:"signatures"`
	}
	if json.Unmarshal(output, &probe) != nil || probe.Signatures == nil || len(probe.Signatures) != 0 {
		return errors.New("personal data disk has a signature or unknown signature state")
	}
	return nil
}

func (i dataInitializer) format(ctx context.Context, receipt *dataReceipt) error {
	// Recheck the actual device immediately before the once-only dispatch marker.
	disk, mounts, err := i.observe(ctx, receipt.Plan)
	if err != nil {
		return err
	}
	if disk.DeviceID != receipt.DeviceID || receipt.Plan.Validate(i.now()) != nil || i.authorize(ctx, receipt.Plan) != nil {
		return errors.New("personal data authority or device changed before formatting")
	}
	if err := i.requireBlank(ctx, disk, mounts); err != nil {
		return err
	}
	receipt.Stage = "format_dispatched"
	if err := i.save(*receipt); err != nil {
		return err
	}
	_, err = i.run(ctx, "/usr/sbin/mkfs.xfs", "-K", "-m", "uuid="+receipt.FilesystemID, "-L", "soha-data", disk.Name)
	return err
}

func (i dataInitializer) mountAndLimit(ctx context.Context, receipt dataReceipt) (DataResult, error) {
	disk, mounts, err := i.observe(ctx, receipt.Plan)
	if err != nil {
		return DataResult{}, err
	}
	if disk.FSType != "xfs" || disk.UUID != receipt.FilesystemID || receipt.Plan.Validate(i.now()) != nil || i.authorize(ctx, receipt.Plan) != nil {
		return DataResult{}, errors.New("personal data filesystem identity is unconfirmed")
	}
	path := filepath.Join(i.base, receipt.FilesystemID, "mount")
	mounted, err := matchingDataMount(disk, mounts, path)
	if err != nil {
		return DataResult{}, err
	}
	if !mounted {
		if err := i.mkdir(path); err != nil {
			return DataResult{}, err
		}
		if _, err := i.run(ctx, "/usr/bin/mount", "-t", "xfs", "-o", "nodev,nosuid,noexec,usrquota,prjquota", disk.Name, path); err != nil {
			return DataResult{}, err
		}
	}
	observed, mounts, err := i.observe(ctx, receipt.Plan)
	if err != nil {
		return DataResult{}, err
	}
	if observed.DeviceID != disk.DeviceID || observed.UUID != receipt.FilesystemID || observed.FSType != "xfs" {
		return DataResult{}, errors.New("personal data device changed while mounting")
	}
	if ok, err := matchingDataMount(observed, mounts, path); err != nil || !ok {
		return DataResult{}, errors.New("personal data mount identity is unconfirmed")
	}
	return i.applyDataQuota(ctx, receipt, path)
}

func (i dataInitializer) applyDataQuota(ctx context.Context, receipt dataReceipt, path string) (DataResult, error) {
	if receipt.Plan.Validate(i.now()) != nil || i.authorize(ctx, receipt.Plan) != nil {
		return DataResult{}, errors.New("personal data authority changed before quota")
	}
	if _, err := i.run(ctx, "/usr/sbin/xfs_quota", "-x", "-c", "project -s -p "+path+" 49", path); err != nil {
		return DataResult{}, err
	}
	quota := strconv.FormatInt(receipt.Plan.DiskQuotaBytes/1024, 10) + "k"
	var used int64
	// File owners can change project IDs in the initial user namespace; UID quota closes that escape.
	for _, limit := range []struct{ kind, id string }{{"p", "49"}, {"u", "1000"}} {
		if _, err := i.run(ctx, "/usr/sbin/xfs_quota", "-x", "-c", "limit -"+limit.kind+" bhard="+quota+" "+limit.id, path); err != nil {
			return DataResult{}, err
		}
		state, err := i.run(ctx, "/usr/sbin/xfs_quota", "-x", "-c", "state -"+limit.kind, path)
		if err != nil || !strings.Contains(string(state), "Accounting: ON") || !strings.Contains(string(state), "Enforcement: ON") {
			return DataResult{}, errors.New("personal data quota is not enforced")
		}
		output, err := i.run(ctx, "/usr/sbin/xfs_quota", "-x", "-c", "report -"+limit.kind+" -b -n", path)
		if err != nil {
			return DataResult{}, err
		}
		usage, err := dataQuotaUsage(string(output), receipt.Plan.DiskQuotaBytes, "#"+limit.id)
		if err != nil {
			return DataResult{}, err
		}
		used = max(used, usage)
	}
	return DataResult{DataRef: receipt.Plan.Binding.DataRef, FilesystemID: receipt.FilesystemID, DiskQuota: receipt.Plan.DiskQuotaBytes, DiskUsed: used}, nil
}

func (c *Client) authorizeData(ctx context.Context, plan DataPlan) error {
	status, raw, err := c.call(ctx, http.MethodGet, "/data-operations/"+plan.OperationID, nil, "")
	if err != nil || status != http.StatusOK {
		return errors.New("current personal data binding authority was denied")
	}
	var current guest.DataPlan
	if json.Unmarshal(raw, &current) != nil || current != plan {
		return errors.New("personal data binding differs from current Core authority")
	}
	return nil
}

func matchingDataMount(disk dataBlock, mounts []dataMount, path string) (bool, error) {
	found := false
	for _, mount := range mounts {
		if mount.DeviceID != disk.DeviceID && mount.Target != path {
			continue
		}
		if found || mount.DeviceID != disk.DeviceID || mount.Target != path || mount.FSType != "xfs" || mount.FSRoot != "/" || !dataMountOptions(mount.Options) {
			return false, errors.New("personal data disk is mounted elsewhere or target is occupied")
		}
		found = true
	}
	return found, nil
}

func dataMountOptions(options string) bool {
	flags := map[string]bool{}
	for _, option := range strings.Split(options, ",") {
		flags[option] = true
	}
	return flags["rw"] && flags["nodev"] && flags["nosuid"] && flags["noexec"] && (flags["prjquota"] || flags["pquota"]) && (flags["usrquota"] || flags["uquota"])
}

func dataQuotaUsage(output string, quota int64, id string) (int64, error) {
	var used int64
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(strings.ToLower(line), "warning") {
			return 0, errors.New("personal data quota warning")
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != id {
			continue
		}
		if len(fields) < 4 {
			return 0, errors.New("invalid project quota observation")
		}
		usage, ue := strconv.ParseInt(fields[1], 10, 64)
		limit, le := strconv.ParseInt(fields[3], 10, 64)
		if ue != nil || le != nil || usage < 0 || usage > 1<<40 || limit != quota/1024 {
			return 0, errors.New("personal data hard quota differs from frozen policy")
		}
		used, count = usage*1024, count+1
	}
	if count != 1 {
		return 0, errors.New("personal data quota identity is ambiguous")
	}
	return used, nil
}

func decodeDataJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid root-owned personal data record")
	}
	return nil
}

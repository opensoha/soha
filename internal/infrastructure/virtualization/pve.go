package virtualization

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"sigs.k8s.io/yaml"
)

type PVEAdapter struct {
	client        *http.Client
	snippetWriter pveSnippetWriter
}

func (a *PVEAdapter) VMCapabilities() []string {
	return []string{
		CapabilityResizeCPU, CapabilityResizeMemory, CapabilityAddDisk, CapabilityResizeDisk, CapabilityAddNetwork,
	}
}

func (a *PVEAdapter) ListVMDevices(ctx context.Context, connection Connection, vm VM) ([]VMDevice, error) {
	config, err := a.pveVMConfig(ctx, connection, vm.Node, vm.ID)
	if err != nil {
		return nil, err
	}
	devices := make([]VMDevice, 0)
	for id, raw := range config {
		value := stringFromAny(raw)
		switch {
		case isPVEDiskID(id) && pveConfigOption(value, "media") != "cdrom":
			storage, _ := pveStorageFromVolume(value)
			devices = append(devices, VMDevice{ID: id, Kind: "disk", Name: id, SizeGiB: pveDiskSizeGiB(value), Storage: storage})
		case strings.HasPrefix(id, "net"):
			devices = append(devices, VMDevice{ID: id, Kind: "network", Name: id, Network: pveConfigOption(value, "bridge"), Model: strings.TrimSpace(strings.Split(value, ",")[0])})
		}
	}
	slices.SortFunc(devices, func(left, right VMDevice) int { return strings.Compare(left.ID, right.ID) })
	return devices, nil
}

func (a *PVEAdapter) pveVMConfig(ctx context.Context, connection Connection, node, vmid string) (map[string]any, error) {
	var payload struct {
		Data map[string]any `json:"data"`
	}
	endpoint := fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(vmid))
	if err := a.do(ctx, connection, http.MethodGet, endpoint, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

func isPVEDiskID(id string) bool {
	return strings.HasPrefix(id, "scsi") || strings.HasPrefix(id, "sata") || strings.HasPrefix(id, "virtio") || strings.HasPrefix(id, "ide")
}
func pveConfigOption(value, key string) string {
	for _, part := range strings.Split(value, ",") {
		name, option, ok := strings.Cut(part, "=")
		if ok && strings.TrimSpace(name) == key {
			return strings.TrimSpace(option)
		}
	}
	return ""
}
func nextPVEDeviceID(config map[string]any, prefix string) string {
	for index := 0; index < 32; index++ {
		id := fmt.Sprintf("%s%d", prefix, index)
		if _, exists := config[id]; !exists {
			return id
		}
	}
	return ""
}

type pveSnippetWriter interface {
	WriteSnippet(ctx context.Context, connection Connection, node string, storage string, filename string, content string, input CreateVMInput) error
}

type pveSSHSnippetWriter struct{}

func NewPVEAdapter(client *http.Client) *PVEAdapter {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &PVEAdapter{client: client, snippetWriter: pveSSHSnippetWriter{}}
}

func (a *PVEAdapter) TestConnection(ctx context.Context, connection Connection) (ConnectionTestResult, error) {
	var payload pveDataEnvelope
	if err := a.do(ctx, connection, http.MethodGet, "/nodes", nil, &payload); err != nil {
		result := ConnectionTestResult{Healthy: false, Status: "degraded", Message: err.Error()}
		if details, ok := AdapterErrorDetails(err); ok {
			result.Reason = details.Reason
			result.HTTPStatus = details.HTTPStatus
			result.NextAction = details.NextAction
		}
		return result, nil
	}
	return ConnectionTestResult{Healthy: true, Status: "healthy"}, nil
}

func (a *PVEAdapter) SyncAssets(ctx context.Context, connection Connection) (AssetSyncResult, error) {
	var nodes pveDataEnvelope
	if err := a.do(ctx, connection, http.MethodGet, "/nodes", nil, &nodes); err != nil {
		return AssetSyncResult{Health: pveAssetHealthFromError(err)}, nil
	}
	syncTypes := pveSyncResourceTypes(connection)
	assets := make([]Asset, 0)
	for _, node := range nodes.Data {
		nodeName := stringFromMap(node, "node")
		if nodeName == "" {
			continue
		}
		if syncTypes["node"] {
			assets = append(assets, Asset{Type: "node", Name: nodeName, Status: stringFromMap(node, "status")})
		}
		assets = append(assets, a.pveNetworkAssets(ctx, connection, nodeName, syncTypes)...)
		qemuAssets, err := a.pveQEMUAssets(ctx, connection, nodeName, syncTypes)
		if err != nil {
			return AssetSyncResult{Health: pveAssetHealthFromError(err), Assets: assets}, nil
		}
		assets = append(assets, qemuAssets...)
		assets = append(assets, a.pveStorageAssets(ctx, connection, nodeName, syncTypes)...)
	}
	return AssetSyncResult{Health: AssetHealth{Status: "healthy"}, Assets: assets}, nil
}

func (a *PVEAdapter) pveNetworkAssets(
	ctx context.Context,
	connection Connection,
	nodeName string,
	syncTypes map[string]bool,
) []Asset {
	if !syncTypes["network"] {
		return nil
	}
	var networks pveDataEnvelope
	endpoint := fmt.Sprintf("/nodes/%s/network", url.PathEscape(nodeName))
	if err := a.do(ctx, connection, http.MethodGet, endpoint, nil, &networks); err != nil {
		return nil
	}
	assets := make([]Asset, 0, len(networks.Data))
	for _, item := range networks.Data {
		if asset, ok := pveNetworkAsset(nodeName, item); ok {
			assets = append(assets, asset)
		}
	}
	return assets
}

func pveNetworkAsset(nodeName string, item map[string]any) (Asset, bool) {
	iface := firstNonEmpty(stringFromAny(item["iface"]), stringFromAny(item["name"]))
	if iface == "" {
		return Asset{}, false
	}
	networkType := firstNonEmpty(stringFromAny(item["type"]), stringFromAny(item["method"]))
	active := boolFromAny(firstNonNil(item["active"], item["autostart"]))
	metadata := map[string]string{
		"iface":     iface,
		"network":   iface,
		"type":      networkType,
		"node":      nodeName,
		"active":    strconv.FormatBool(active),
		"bridge":    strconv.FormatBool(strings.EqualFold(networkType, "bridge")),
		"sourceRef": iface,
	}
	if cidr := stringFromAny(item["cidr"]); cidr != "" {
		metadata["cidr"] = cidr
	}
	if address := stringFromAny(item["address"]); address != "" {
		metadata["address"] = address
	}
	return Asset{
		Type: "network", Name: iface, Node: nodeName,
		Status: firstNonEmpty(networkType, "network"), Metadata: metadata,
	}, true
}

func (a *PVEAdapter) pveQEMUAssets(
	ctx context.Context,
	connection Connection,
	nodeName string,
	syncTypes map[string]bool,
) ([]Asset, error) {
	if !syncTypes["qemu"] && !syncTypes["vm"] && !syncTypes["template"] {
		return nil, nil
	}
	var qemu pveDataEnvelope
	endpoint := fmt.Sprintf("/nodes/%s/qemu", url.PathEscape(nodeName))
	if err := a.do(ctx, connection, http.MethodGet, endpoint, nil, &qemu); err != nil {
		return nil, err
	}
	assets := make([]Asset, 0, len(qemu.Data))
	for _, vm := range qemu.Data {
		if asset, ok := pveQEMUAsset(nodeName, vm, syncTypes); ok {
			assets = append(assets, asset)
		}
	}
	return assets, nil
}

func pveQEMUAsset(nodeName string, vm map[string]any, syncTypes map[string]bool) (Asset, bool) {
	vmid := stringFromAny(vm["vmid"])
	assetType := "qemu"
	status := stringFromMap(vm, "status")
	if boolFromAny(vm["template"]) {
		assetType = "template"
		status = firstNonEmpty(status, "template")
	}
	if !syncTypes[assetType] && (assetType != "qemu" || !syncTypes["vm"]) {
		return Asset{}, false
	}
	metadata := map[string]string{"vmid": vmid, "sourceRef": vmid, "node": nodeName}
	if cpus := stringFromAny(firstNonNil(vm["cpus"], vm["cores"])); cpus != "" {
		metadata["cpu"] = cpus
	}
	if memory := stringFromAny(firstNonNil(vm["maxmem"], vm["mem"])); memory != "" {
		metadata["memory"] = memory
	}
	return Asset{
		Type: assetType, Name: firstNonEmpty(stringFromAny(vm["name"]), "vm-"+vmid),
		Node: nodeName, Status: status, Metadata: metadata,
	}, true
}

func (a *PVEAdapter) pveStorageAssets(
	ctx context.Context,
	connection Connection,
	nodeName string,
	syncTypes map[string]bool,
) []Asset {
	if !pveStorageSyncEnabled(syncTypes) {
		return nil
	}
	var storages pveDataEnvelope
	endpoint := fmt.Sprintf("/nodes/%s/storage", url.PathEscape(nodeName))
	if err := a.do(ctx, connection, http.MethodGet, endpoint, nil, &storages); err != nil {
		return nil
	}
	assets := make([]Asset, 0)
	for _, item := range storages.Data {
		storageName := stringFromAny(item["storage"])
		if syncTypes["storage"] {
			assets = append(assets, pveStorageAsset(nodeName, storageName, item))
		}
		assets = append(assets, a.pveStorageContentAssets(
			ctx, connection, nodeName, storageName, item, syncTypes,
		)...)
	}
	return assets
}

func pveStorageSyncEnabled(syncTypes map[string]bool) bool {
	return syncTypes["storage"] || syncTypes["storage_content"] || syncTypes["iso"] ||
		syncTypes["lxc_template"] || syncTypes["image"]
}

func pveStorageAsset(nodeName, storageName string, item map[string]any) Asset {
	contentTypes := pveStorageContentSet(item)
	return Asset{
		Type: "storage", Name: storageName, Node: nodeName, Status: stringFromMap(item, "type"),
		Metadata: map[string]string{
			"storage": storageName, "type": stringFromMap(item, "type"),
			"content":          stringFromMap(item, "content"),
			"supportsISO":      strconv.FormatBool(contentTypes["iso"]),
			"supportsImages":   strconv.FormatBool(contentTypes["images"]),
			"supportsSnippets": strconv.FormatBool(contentTypes["snippets"]),
			"supportsBackup":   strconv.FormatBool(contentTypes["backup"]),
			"supportsRootdir":  strconv.FormatBool(contentTypes["rootdir"]),
		},
	}
}

func (a *PVEAdapter) pveStorageContentAssets(
	ctx context.Context,
	connection Connection,
	nodeName, storageName string,
	storage map[string]any,
	syncTypes map[string]bool,
) []Asset {
	if storageName == "" || !pveStorageSupportsContent(storage) {
		return nil
	}
	var content pveDataEnvelope
	endpoint := fmt.Sprintf(
		"/nodes/%s/storage/%s/content", url.PathEscape(nodeName), url.PathEscape(storageName),
	)
	if err := a.do(ctx, connection, http.MethodGet, endpoint, nil, &content); err != nil {
		return nil
	}
	assets := make([]Asset, 0, len(content.Data))
	for _, item := range content.Data {
		if asset, ok := pveStorageContentAsset(nodeName, storageName, item, syncTypes); ok {
			assets = append(assets, asset)
		}
	}
	return assets
}

func pveStorageContentAsset(
	nodeName, storageName string,
	content map[string]any,
	syncTypes map[string]bool,
) (Asset, bool) {
	contentType := stringFromMap(content, "content")
	volID := firstNonEmpty(stringFromAny(content["volid"]), stringFromAny(content["name"]))
	assetType := pveStorageContentAssetType(contentType, volID)
	if !syncTypes[assetType] && !syncTypes["storage_content"] {
		return Asset{}, false
	}
	metadata := map[string]string{
		"volid": volID, "sourceRef": volID, "contentType": contentType,
		"node": nodeName, "storage": storageName,
	}
	if format := stringFromAny(content["format"]); format != "" {
		metadata["format"] = format
	}
	if size := stringFromAny(content["size"]); size != "" {
		metadata["size"] = size
	}
	return Asset{
		Type: assetType, Name: volID, Node: nodeName, Status: contentType, Metadata: metadata,
	}, true
}

func pveStorageContentAssetType(contentType, volID string) string {
	switch {
	case contentType == "iso":
		return "iso"
	case contentType == "vztmpl":
		return "lxc_template"
	case strings.Contains(strings.ToLower(volID), "template"):
		return "template"
	case contentType == "images" || contentType == "rootdir":
		return "image"
	default:
		return "storage_content"
	}
}

func (a *PVEAdapter) CreateVM(ctx context.Context, connection Connection, input CreateVMInput) (VM, error) {
	plan, err := a.preparePVECreate(ctx, connection, input)
	if err != nil {
		return VM{}, err
	}
	if input.SourceMode == "template_clone" || input.TemplateID != "" {
		return a.createPVEClone(ctx, connection, plan)
	}
	return a.createPVENative(ctx, connection, plan)
}

type pveCreatePlan struct {
	input    CreateVMInput
	node     string
	storage  string
	bridge   string
	iso      string
	vmid     string
	cicustom string
}

func (a *PVEAdapter) preparePVECreate(
	ctx context.Context,
	connection Connection,
	input CreateVMInput,
) (pveCreatePlan, error) {
	if input.Name == "" {
		return pveCreatePlan{}, invalidf("vm name is required")
	}
	plan := pveCreatePlan{input: input}
	plan.node = input.Node
	if plan.node == "" {
		plan.node = stringOptionValue(connection.Options, "defaultNode")
	}
	if plan.node == "" {
		return pveCreatePlan{}, invalidf("node is required")
	}
	plan.storage = firstNonEmpty(
		stringFromAny(input.ProviderParams["storage"]),
		stringOptionValue(connection.Options, "defaultStorage"),
	)
	plan.bridge = firstNonEmpty(
		stringFromAny(input.ProviderParams["bridge"]),
		stringOptionValue(connection.Options, "defaultBridge"),
	)
	plan.iso = stringFromAny(input.ProviderParams["iso"])
	plan.vmid = firstNonEmpty(
		stringFromAny(input.ProviderParams["vmid"]),
		stringOptionValue(connection.Options, "vmid"),
		stringOptionValue(connection.Options, "nextVmid"),
	)
	if plan.vmid == "" {
		vmid, err := a.nextVMID(ctx, connection)
		if err != nil {
			return pveCreatePlan{}, err
		}
		plan.vmid = vmid
	}
	cicustom, err := a.ensurePVECICustom(ctx, connection, plan.node, plan.vmid, input)
	if err != nil {
		return pveCreatePlan{}, err
	}
	plan.cicustom = cicustom
	return plan, nil
}

func (a *PVEAdapter) createPVEClone(
	ctx context.Context,
	connection Connection,
	plan pveCreatePlan,
) (VM, error) {
	templateID := firstNonEmpty(plan.input.TemplateID, plan.input.SourceRef, plan.input.BootImage)
	if templateID == "" {
		return VM{}, invalidf("template source is required")
	}
	payload := map[string]any{"newid": plan.vmid, "name": plan.input.Name}
	if plan.storage != "" {
		payload["storage"] = plan.storage
		payload["full"] = 1
	}
	endpoint := fmt.Sprintf(
		"/nodes/%s/qemu/%s/clone", url.PathEscape(plan.node), url.PathEscape(templateID),
	)
	createUPID, err := a.doTaskAndWait(
		ctx, connection, plan.node, http.MethodPost, endpoint, payload,
	)
	if err != nil {
		return VM{}, err
	}
	resizeUPID := ""
	if plan.input.DiskSize != "" {
		resizeUPID, err = a.resizePVEClonedDisk(
			ctx, connection, plan.node, plan.vmid, "scsi0", plan.input.DiskSize,
		)
		if err != nil {
			return VM{}, err
		}
	}
	configUPID, err := a.configurePVECloneCloudInit(ctx, connection, plan)
	if err != nil {
		return VM{}, err
	}
	startUPID, err := a.startPVEAfterCreate(ctx, connection, plan)
	if err != nil {
		return VM{}, err
	}
	return a.finishPVECreate(ctx, connection, plan, map[string]string{
		"pveCreateUpid": createUPID,
		"pveResizeUpid": resizeUPID,
		"pveConfigUpid": configUPID,
		"pveStartUpid":  startUPID,
	})
}

func (a *PVEAdapter) configurePVECloneCloudInit(
	ctx context.Context,
	connection Connection,
	plan pveCreatePlan,
) (string, error) {
	payload := pveCloudInitConfigPayload(plan.input, plan.cicustom, plan.bridge)
	if len(payload) == 0 {
		return "", nil
	}
	endpoint := fmt.Sprintf(
		"/nodes/%s/qemu/%s/config", url.PathEscape(plan.node), url.PathEscape(plan.vmid),
	)
	upid, err := a.doTaskAndWait(ctx, connection, plan.node, http.MethodPost, endpoint, payload)
	if err != nil {
		return "", err
	}
	if err := a.refreshPVECloudInit(ctx, connection, plan.node, plan.vmid); err != nil {
		return "", err
	}
	return upid, nil
}

func (a *PVEAdapter) createPVENative(
	ctx context.Context,
	connection Connection,
	plan pveCreatePlan,
) (VM, error) {
	endpoint := fmt.Sprintf("/nodes/%s/qemu", url.PathEscape(plan.node))
	createUPID, err := a.doTaskAndWait(
		ctx, connection, plan.node, http.MethodPost, endpoint, pveCreatePayload(plan),
	)
	if err != nil {
		return VM{}, err
	}
	startUPID, err := a.startPVEAfterCreate(ctx, connection, plan)
	if err != nil {
		return VM{}, err
	}
	return a.finishPVECreate(ctx, connection, plan, map[string]string{
		"pveCreateUpid": createUPID,
		"pveStartUpid":  startUPID,
	})
}

func pveCreatePayload(plan pveCreatePlan) map[string]any {
	payload := map[string]any{"name": plan.input.Name, "vmid": plan.vmid}
	if plan.input.CPU > 0 {
		payload["cores"] = plan.input.CPU
	}
	if memoryMB := normalizePVEMemoryMB(plan.input.Memory); memoryMB > 0 {
		payload["memory"] = memoryMB
	}
	if arch := pveArchitecture(plan.input.Architecture); arch != "" {
		payload["arch"] = arch
	}
	addPVEDiskAndNetwork(payload, plan)
	addPVECloudInitPayload(payload, plan)
	return payload
}

func addPVEDiskAndNetwork(payload map[string]any, plan pveCreatePlan) {
	if plan.input.DiskSize != "" {
		diskRef := normalizePVEDiskSize(plan.input.DiskSize)
		if plan.storage != "" {
			diskRef = fmt.Sprintf("%s:%s", plan.storage, diskRef)
		}
		payload["scsi0"] = diskRef
	}
	if plan.bridge != "" {
		payload["net0"] = fmt.Sprintf("virtio,bridge=%s", plan.bridge)
	} else if plan.input.Network != "" {
		payload["net0"] = plan.input.Network
	}
	for index, disk := range plan.input.Disks {
		if !disk.Add {
			continue
		}
		id := firstNonEmpty(disk.ID, disk.Name, fmt.Sprintf("scsi%d", index+1))
		storage := firstNonEmpty(disk.Storage, plan.storage)
		if id != "" && storage != "" && disk.SizeGiB > 0 {
			payload[id] = fmt.Sprintf("%s:%d", storage, disk.SizeGiB)
		}
	}
	for index, network := range plan.input.Networks {
		if !network.Add {
			continue
		}
		id := firstNonEmpty(network.ID, network.Name, fmt.Sprintf("net%d", index+1))
		if id != "" && network.Network != "" {
			payload[id] = fmt.Sprintf("%s,bridge=%s", firstNonEmpty(network.Model, "virtio"), network.Network)
		}
	}
}

func addPVECloudInitPayload(payload map[string]any, plan pveCreatePlan) {
	iso := firstNonEmpty(plan.iso, plan.input.SourceRef, plan.input.BootImage)
	if iso != "" {
		payload["ide2"] = normalizePVEISORef(iso)
	}
	if ciuser := stringFromAny(plan.input.ProviderParams["ciuser"]); ciuser != "" {
		payload["ciuser"] = ciuser
	}
	if sshKeys := stringFromAny(plan.input.ProviderParams["sshkeys"]); sshKeys != "" && plan.cicustom == "" {
		payload["sshkeys"] = normalizePVESSHKeys(sshKeys)
	}
	if plan.cicustom != "" {
		payload["cicustom"] = plan.cicustom
	}
}

func (a *PVEAdapter) startPVEAfterCreate(
	ctx context.Context,
	connection Connection,
	plan pveCreatePlan,
) (string, error) {
	if !plan.input.StartAfterCreate {
		return "", nil
	}
	endpoint := fmt.Sprintf(
		"/nodes/%s/qemu/%s/status/start", url.PathEscape(plan.node), url.PathEscape(plan.vmid),
	)
	return a.doTaskAndWait(ctx, connection, plan.node, http.MethodPost, endpoint, nil)
}

func (a *PVEAdapter) finishPVECreate(
	ctx context.Context,
	connection Connection,
	plan pveCreatePlan,
	metadata map[string]string,
) (VM, error) {
	vm, err := a.fetchVM(ctx, connection, plan.node, plan.vmid, plan.input.Name)
	if err != nil {
		return VM{}, err
	}
	return a.enrichPVEVM(
		ctx, connection, plan.node, plan.vmid, plan.input, vm, metadata,
	), nil
}

func (a *PVEAdapter) refreshPVECloudInit(ctx context.Context, connection Connection, node string, vmid string) error {
	_, err := a.doTaskAndWait(ctx, connection, node, http.MethodPut, fmt.Sprintf("/nodes/%s/qemu/%s/cloudinit", url.PathEscape(node), url.PathEscape(vmid)), nil)
	return err
}

func (a *PVEAdapter) resizePVEClonedDisk(ctx context.Context, connection Connection, node string, vmid string, disk string, targetSize string) (string, error) {
	targetGiB := pveDiskSizeGiB(targetSize)
	if targetGiB <= 0 {
		return "", nil
	}
	currentGiB := a.pveDiskConfigSizeGiB(ctx, connection, node, vmid, disk)
	if currentGiB > 0 && currentGiB >= targetGiB {
		return "", nil
	}
	payload := map[string]any{"disk": disk, "size": fmt.Sprintf("%dG", targetGiB)}
	return a.doTaskAndWait(ctx, connection, node, http.MethodPut, fmt.Sprintf("/nodes/%s/qemu/%s/resize", url.PathEscape(node), url.PathEscape(vmid)), payload)
}

func (a *PVEAdapter) pveDiskConfigSizeGiB(ctx context.Context, connection Connection, node string, vmid string, disk string) int {
	var payload struct {
		Data map[string]any `json:"data"`
	}
	endpoint := fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(vmid))
	if err := a.do(ctx, connection, http.MethodGet, endpoint, nil, &payload); err != nil {
		return 0
	}
	return pveDiskSizeGiB(stringFromAny(payload.Data[disk]))
}

func (a *PVEAdapter) PowerAction(ctx context.Context, connection Connection, vm VM, action PowerAction) (PowerActionResult, error) {
	if vm.ID == "" {
		return PowerActionResult{}, invalidf("vm id is required")
	}
	if vm.Node == "" {
		return PowerActionResult{}, invalidf("node is required")
	}
	if action == PowerActionDelete {
		return a.deletePVEVM(ctx, connection, vm)
	}
	var endpoint string
	var method = http.MethodPost
	switch action {
	case PowerActionStart:
		endpoint = fmt.Sprintf("/nodes/%s/qemu/%s/status/start", url.PathEscape(vm.Node), url.PathEscape(vm.ID))
	case PowerActionStop:
		endpoint = fmt.Sprintf("/nodes/%s/qemu/%s/status/stop", url.PathEscape(vm.Node), url.PathEscape(vm.ID))
	case PowerActionRestart:
		endpoint = fmt.Sprintf("/nodes/%s/qemu/%s/status/reboot", url.PathEscape(vm.Node), url.PathEscape(vm.ID))
	default:
		return PowerActionResult{}, invalidf("unsupported power action %q", action)
	}
	upid, err := a.doTaskAndWait(ctx, connection, vm.Node, method, endpoint, nil)
	if err != nil {
		return PowerActionResult{}, err
	}
	return PowerActionResult{Accepted: true, Action: action, UPID: upid}, nil
}

func (a *PVEAdapter) ResizeVM(ctx context.Context, connection Connection, vm VM, input AdapterResizeVMInput) (PowerActionResult, error) {
	if vm.ID == "" || vm.Node == "" {
		return PowerActionResult{}, invalidf("vm id and node are required")
	}
	payload := map[string]any{}
	if input.CPU > 0 {
		payload["cores"] = input.CPU
	}
	if input.MemoryMiB > 0 {
		payload["memory"] = input.MemoryMiB
	}
	if len(payload) > 0 {
		if _, err := a.doTaskAndWait(ctx, connection, vm.Node, http.MethodPut, fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(vm.Node), url.PathEscape(vm.ID)), payload); err != nil {
			return PowerActionResult{}, err
		}
	}
	if input.DiskGiB > 0 {
		if _, err := a.resizePVEClonedDisk(ctx, connection, vm.Node, vm.ID, "scsi0", fmt.Sprintf("%dG", input.DiskGiB)); err != nil {
			return PowerActionResult{}, err
		}
	}
	config, configErr := a.pveVMConfig(ctx, connection, vm.Node, vm.ID)
	if configErr != nil && (len(input.Disks) > 0 || len(input.Networks) > 0) {
		return PowerActionResult{}, configErr
	}
	for _, disk := range input.Disks {
		diskID := firstNonEmpty(disk.ID, disk.Name)
		if diskID == "" && disk.Add {
			diskID = nextPVEDeviceID(config, "scsi")
		}
		if diskID == "" {
			return PowerActionResult{}, invalidf("disk id is required")
		}
		if disk.Add {
			storage := firstNonEmpty(disk.Storage, stringOptionValue(connection.Options, "defaultStorage"))
			if storage == "" {
				return PowerActionResult{}, invalidf("storage is required for a new disk")
			}
			value := fmt.Sprintf("%s:%d", storage, disk.SizeGiB)
			if _, err := a.doTaskAndWait(ctx, connection, vm.Node, http.MethodPut, fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(vm.Node), url.PathEscape(vm.ID)), map[string]any{diskID: value}); err != nil {
				return PowerActionResult{}, err
			}
			config[diskID] = value
		} else if _, err := a.resizePVEClonedDisk(ctx, connection, vm.Node, vm.ID, diskID, fmt.Sprintf("%dG", disk.SizeGiB)); err != nil {
			return PowerActionResult{}, err
		}
	}
	for _, network := range input.Networks {
		if !network.Add {
			continue
		}
		networkID := firstNonEmpty(network.ID, network.Name)
		if networkID == "" {
			networkID = nextPVEDeviceID(config, "net")
		}
		if networkID == "" {
			return PowerActionResult{}, invalidf("network interface id is required")
		}
		model := firstNonEmpty(network.Model, "virtio")
		if network.Network == "" {
			return PowerActionResult{}, invalidf("network bridge is required")
		}
		value := fmt.Sprintf("%s,bridge=%s", model, network.Network)
		if _, err := a.doTaskAndWait(ctx, connection, vm.Node, http.MethodPut, fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(vm.Node), url.PathEscape(vm.ID)), map[string]any{networkID: value}); err != nil {
			return PowerActionResult{}, err
		}
	}
	return PowerActionResult{Accepted: true, Action: "resize", Message: "virtual machine resources updated"}, nil
}

func (a *PVEAdapter) deletePVEVM(ctx context.Context, connection Connection, vm VM) (PowerActionResult, error) {
	generatedSnippetRef := a.generatedPVECloudInitSnippetRef(ctx, connection, vm.Node, vm.ID)
	current, err := a.fetchVM(ctx, connection, vm.Node, vm.ID, vm.Name)
	if err != nil {
		return PowerActionResult{}, err
	}
	if strings.EqualFold(firstNonEmpty(current.Metadata["qmpstatus"], current.Status), "running") {
		endpoint := fmt.Sprintf("/nodes/%s/qemu/%s/status/stop", url.PathEscape(vm.Node), url.PathEscape(vm.ID))
		if _, err := a.doTaskAndWait(ctx, connection, vm.Node, http.MethodPost, endpoint, nil); err != nil {
			return PowerActionResult{}, err
		}
	}
	endpoint := fmt.Sprintf("/nodes/%s/qemu/%s", url.PathEscape(vm.Node), url.PathEscape(vm.ID))
	upid, err := a.doTaskAndWait(ctx, connection, vm.Node, http.MethodDelete, endpoint, nil)
	if err != nil {
		return PowerActionResult{}, err
	}
	result := PowerActionResult{Accepted: true, Action: PowerActionDelete, UPID: upid}
	if generatedSnippetRef != "" {
		if err := a.deletePVEStorageVolume(ctx, connection, vm.Node, generatedSnippetRef); err != nil {
			result.Message = "virtual machine deleted; generated cloud-init snippet cleanup failed: " + err.Error()
		} else {
			result.Message = "virtual machine deleted; generated cloud-init snippet cleaned up"
		}
	}
	return result, nil
}

func (a *PVEAdapter) generatedPVECloudInitSnippetRef(ctx context.Context, connection Connection, node string, vmid string) string {
	var payload struct {
		Data map[string]any `json:"data"`
	}
	endpoint := fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(vmid))
	if err := a.do(ctx, connection, http.MethodGet, endpoint, nil, &payload); err != nil {
		return ""
	}
	return pveGeneratedSnippetFromCICustom(stringFromAny(payload.Data["cicustom"]), vmid)
}

func pveGeneratedSnippetFromCICustom(cicustom string, vmid string) string {
	expected := fmt.Sprintf("soha-%s-cloud-init.yaml", sanitizePVESnippetName(vmid))
	for _, part := range strings.Split(cicustom, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(key) != "user" {
			continue
		}
		value = strings.TrimSpace(value)
		if path.Base(value) == expected {
			return value
		}
	}
	return ""
}

func (a *PVEAdapter) deletePVEStorageVolume(ctx context.Context, connection Connection, node string, volume string) error {
	storage, ok := pveStorageFromVolume(volume)
	if !ok {
		return invalidf("valid pve storage volume is required")
	}
	endpoint := fmt.Sprintf("/nodes/%s/storage/%s/content/%s", url.PathEscape(node), url.PathEscape(storage), pveVolumePathEscape(volume))
	return a.do(ctx, connection, http.MethodDelete, endpoint, nil, nil)
}

func pveStorageFromVolume(volume string) (string, bool) {
	storage, _, ok := strings.Cut(strings.TrimSpace(volume), ":")
	return storage, ok && storage != ""
}

func pveVolumePathEscape(volume string) string {
	parts := strings.Split(strings.TrimSpace(volume), "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func (a *PVEAdapter) ensurePVECICustom(ctx context.Context, connection Connection, node string, vmid string, input CreateVMInput) (string, error) {
	if cicustom := stringFromAny(input.ProviderParams["cicustom"]); cicustom != "" {
		return cicustom, nil
	}
	cloudInit := strings.TrimSpace(input.CloudInit)
	if cloudInit == "" {
		return "", nil
	}
	if pveCloudInitLooksLikeRef(cloudInit) {
		return cloudInit, nil
	}
	storage := firstNonEmpty(
		stringFromAny(input.ProviderParams["snippetStorage"]),
		stringOptionValue(connection.Options, "defaultSnippetStorage"),
		stringOptionValue(connection.Options, "snippetStorage"),
	)
	if storage == "" {
		return "", invalidf("pve snippet storage is required for raw cloud-init user data")
	}
	cloudInit = mergePVECloudInitIdentity(cloudInit, input.ProviderParams)
	ref, err := a.uploadPVECloudInit(ctx, connection, node, storage, vmid, cloudInit, input)
	if err != nil {
		return "", err
	}
	return "user=" + ref, nil
}

func pveCloudInitConfigPayload(input CreateVMInput, cicustom string, providerBridge string) map[string]any {
	payload := map[string]any{}
	if input.CPU > 0 {
		payload["cores"] = input.CPU
	}
	if memoryMB := normalizePVEMemoryMB(input.Memory); memoryMB > 0 {
		payload["memory"] = memoryMB
	}
	if providerBridge != "" {
		payload["net0"] = fmt.Sprintf("virtio,bridge=%s", providerBridge)
	} else if input.Network != "" {
		payload["net0"] = input.Network
	}
	for _, key := range []string{"ipconfig0", "ipconfig1", "ipconfig2", "ipconfig3", "nameserver", "searchdomain"} {
		if value := stringFromAny(input.ProviderParams[key]); value != "" {
			payload[key] = value
		}
	}
	if ciuser := stringFromAny(input.ProviderParams["ciuser"]); ciuser != "" {
		payload["ciuser"] = ciuser
	}
	if sshKeys := stringFromAny(input.ProviderParams["sshkeys"]); sshKeys != "" && cicustom == "" {
		payload["sshkeys"] = normalizePVESSHKeys(sshKeys)
	}
	if cicustom != "" {
		payload["cicustom"] = cicustom
	}
	return payload
}

func pveCloudInitLooksLikeRef(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "\n") || strings.HasPrefix(value, "#") {
		return false
	}
	return strings.Contains(value, "=") && strings.Contains(value, ":")
}

func mergePVECloudInitIdentity(content string, providerParams map[string]any) string {
	ciuser := strings.TrimSpace(stringFromAny(providerParams["ciuser"]))
	sshKeys := normalizePVECloudInitSSHKeys(stringFromAny(providerParams["sshkeys"]))
	if ciuser == "" && len(sshKeys) == 0 {
		return content
	}
	document := strings.TrimSpace(content)
	if !strings.HasPrefix(document, "#cloud-config") {
		return content
	}
	body := strings.TrimSpace(strings.TrimPrefix(document, "#cloud-config"))
	var data map[string]any
	if body != "" {
		if err := yaml.Unmarshal([]byte(body), &data); err != nil {
			return content
		}
	}
	additions := make([]string, 0, 4+len(sshKeys))
	if ciuser != "" && data["user"] == nil && data["users"] == nil {
		additions = append(additions, "user: "+ciuser)
	}
	if len(sshKeys) > 0 && data["ssh_authorized_keys"] == nil {
		additions = append(additions, "ssh_authorized_keys:")
		for _, key := range sshKeys {
			additions = append(additions, "  - "+key)
		}
	}
	if len(additions) == 0 {
		return content
	}
	lines := strings.SplitN(document, "\n", 2)
	if len(lines) == 1 {
		return "#cloud-config\n" + strings.Join(additions, "\n") + "\n"
	}
	return lines[0] + "\n" + strings.Join(additions, "\n") + "\n" + strings.TrimLeft(lines[1], "\n") + "\n"
}

func normalizePVECloudInitSSHKeys(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.Contains(value, "%") {
		if decoded, err := url.QueryUnescape(value); err == nil && decoded != "" {
			value = decoded
		}
	}
	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\\n", "\n")
	keys := make([]string, 0)
	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		keys = append(keys, line)
	}
	return keys
}

func normalizePVESSHKeys(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if decoded, err := url.QueryUnescape(value); err == nil && decoded != value && looksLikeSSHAuthorizedKey(decoded) {
		return value
	}
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func looksLikeSSHAuthorizedKey(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "ssh-") || strings.HasPrefix(value, "ecdsa-") || strings.HasPrefix(value, "sk-")
}

func (a *PVEAdapter) uploadPVECloudInit(ctx context.Context, connection Connection, node string, storage string, vmid string, content string, input CreateVMInput) (string, error) {
	filename := fmt.Sprintf("soha-%s-cloud-init.yaml", sanitizePVESnippetName(vmid))
	method, err := pveSnippetWriteMethod(connection, input)
	if err != nil {
		return "", err
	}
	if method == "ssh" {
		return a.writePVECloudInitViaSSH(ctx, connection, node, storage, filename, content, input)
	}
	endpoint := fmt.Sprintf("/nodes/%s/storage/%s/upload", url.PathEscape(node), url.PathEscape(storage))
	base, err := url.Parse(strings.TrimRight(connection.Endpoint, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", invalidf("valid pve endpoint is required")
	}
	base.Path = path.Join(base.Path, "/api2/json", endpoint)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("content", "snippets"); err != nil {
		return "", err
	}
	part, err := writer.CreateFormFile("filename", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write([]byte(content)); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := a.applyPVEAuth(ctx, req, connection); err != nil {
		return "", err
	}
	resp, err := a.clientFor(connection).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := classifyPVEHTTPError(resp.StatusCode, endpoint, raw)
		if method == "auto" && isPVEErrorReason(err, "snippet_upload_unsupported") && pveCanWriteSnippetViaSSH(connection, input) {
			return a.writePVECloudInitViaSSH(ctx, connection, node, storage, filename, content, input)
		}
		return "", err
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return "", fmt.Errorf("discard pve upload response: %w", err)
	}
	return fmt.Sprintf("%s:snippets/%s", storage, filename), nil
}

func (a *PVEAdapter) writePVECloudInitViaSSH(ctx context.Context, connection Connection, node string, storage string, filename string, content string, input CreateVMInput) (string, error) {
	if a.snippetWriter == nil {
		return "", pveSnippetWriteUnavailableError("pve snippet ssh writer is not configured")
	}
	if !pveCanWriteSnippetViaSSH(connection, input) {
		return "", pveSnippetWriteUnavailableError("pve snippet ssh write requires username/password credentials")
	}
	if err := a.snippetWriter.WriteSnippet(ctx, connection, node, storage, filename, content, input); err != nil {
		return "", &AdapterError{
			Cause:      err,
			Reason:     "snippet_write_failed",
			Message:    "pve snippet ssh write failed: " + err.Error(),
			NextAction: "verify SSH connectivity, credentials, and the configured PVE snippet directory",
		}
	}
	return fmt.Sprintf("%s:snippets/%s", storage, filename), nil
}

func pveSnippetWriteMethod(connection Connection, input CreateVMInput) (string, error) {
	method := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		stringFromAny(input.ProviderParams["snippetWriteMethod"]),
		stringOptionValue(connection.Options, "snippetWriteMethod"),
	)))
	if method == "" {
		return "auto", nil
	}
	switch method {
	case "auto", "api", "ssh":
		return method, nil
	default:
		return "", invalidf("unsupported pve snippet write method %q", method)
	}
}

func pveCanWriteSnippetViaSSH(connection Connection, input CreateVMInput) bool {
	_, username, password, _, _, ok := pveSSHConfig(connection, input)
	return ok && username != "" && password != ""
}

func pveSnippetWriteUnavailableError(message string) error {
	return &AdapterError{
		Reason:     "snippet_write_unavailable",
		Message:    message,
		NextAction: "use providerParams.cicustom with an existing PVE snippet or configure PVE SSH snippet write credentials",
	}
}

func isPVEErrorReason(err error, reason string) bool {
	details, ok := AdapterErrorDetails(err)
	return ok && details.Reason == reason
}

func (pveSSHSnippetWriter) WriteSnippet(ctx context.Context, connection Connection, node string, storage string, filename string, content string, input CreateVMInput) error {
	host, username, password, directory, port, ok := pveSSHConfig(connection, input)
	if !ok {
		return pveSnippetWriteUnavailableError("pve snippet ssh write requires host, username, password, and snippet directory")
	}
	timeout := pveSSHTimeout(connection, input)
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{ssh.Password(password)},
		//nolint:gosec // optional fallback for operator-provided PVE hosts; deployments can prefer REST snippet upload or pre-created cicustom snippets
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dial pve ssh %s: %w", address, err)
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("authenticate pve ssh %s: %w", address, err)
	}
	client := ssh.NewClient(clientConn, chans, reqs)
	defer func() { _ = client.Close() }()
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("create pve ssh session: %w", err)
	}
	defer func() { _ = session.Close() }()
	command := pveSSHWriteSnippetCommand(directory, filename)
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("open pve ssh stdin: %w", err)
	}
	if err := session.Start(command); err != nil {
		return fmt.Errorf("start pve ssh snippet write: %w", err)
	}
	if _, err := io.WriteString(stdin, content); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("write pve ssh snippet content: %w", err)
	}
	if err := stdin.Close(); err != nil {
		return fmt.Errorf("close pve ssh snippet stdin: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("finish pve ssh snippet write: %w", err)
		}
		return nil
	case <-ctx.Done():
		_ = session.Close()
		return ctx.Err()
	}
}

func pveSSHWriteSnippetCommand(directory string, filename string) string {
	target := path.Join(directory, filename)
	tempTarget := target + ".tmp"
	return fmt.Sprintf("mkdir -p %s && umask 077 && cat > %s && install -m 0644 %s %s && rm -f %s", shellQuote(directory), shellQuote(tempTarget), shellQuote(tempTarget), shellQuote(target), shellQuote(tempTarget))
}

func pveSSHConfig(connection Connection, input CreateVMInput) (string, string, string, string, int, bool) {
	host := firstNonEmpty(
		stringFromAny(input.ProviderParams["sshHost"]),
		stringOptionValue(connection.Options, "sshHost"),
		pveEndpointHost(connection.Endpoint),
	)
	username := firstNonEmpty(
		stringFromAny(input.ProviderParams["sshUsername"]),
		stringFromAny(connection.Credential["sshUsername"]),
		pveSSHUsername(stringFromAny(connection.Credential["username"])),
	)
	password := firstNonEmpty(
		stringFromAny(connection.Credential["sshPassword"]),
		stringFromAny(connection.Credential["password"]),
	)
	storage := strings.TrimSpace(storageFromInputOrOption(input, connection))
	if storage == "" {
		storage = strings.TrimSpace(stringFromAny(input.ProviderParams["snippetStorage"]))
	}
	directory := firstNonEmpty(
		stringFromAny(input.ProviderParams["snippetDirectory"]),
		stringFromAny(input.ProviderParams["sshSnippetDirectory"]),
		stringOptionValue(connection.Options, "snippetDirectory"),
		stringOptionValue(connection.Options, "sshSnippetDirectory"),
		defaultPVESnippetDirectory(storage),
	)
	port := intFromAny(input.ProviderParams["sshPort"])
	if port <= 0 {
		port = intOptionValue(connection.Options, "sshPort")
	}
	if port <= 0 {
		port = 22
	}
	ok := host != "" && username != "" && password != "" && directory != ""
	return host, username, password, directory, port, ok
}

func storageFromInputOrOption(input CreateVMInput, connection Connection) string {
	return firstNonEmpty(
		stringFromAny(input.ProviderParams["snippetStorage"]),
		stringOptionValue(connection.Options, "defaultSnippetStorage"),
		stringOptionValue(connection.Options, "snippetStorage"),
	)
}

func defaultPVESnippetDirectory(storage string) string {
	storage = strings.TrimSpace(storage)
	switch storage {
	case "":
		return ""
	case "local":
		return "/var/lib/vz/snippets"
	default:
		return "/mnt/pve/" + shellPathSegment(storage) + "/snippets"
	}
}

func pveEndpointHost(endpoint string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	if host != "" {
		return host
	}
	return strings.TrimSpace(parsed.Host)
}

func pveSSHUsername(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	if before, _, ok := strings.Cut(username, "@"); ok && before != "" {
		return before
	}
	return username
}

func pveSSHTimeout(connection Connection, input CreateVMInput) time.Duration {
	seconds := intFromAny(input.ProviderParams["sshTimeoutSeconds"])
	if seconds <= 0 {
		seconds = intOptionValue(connection.Options, "sshTimeoutSeconds")
	}
	if seconds <= 0 {
		seconds = 15
	}
	return time.Duration(seconds) * time.Second
}

func shellPathSegment(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('-')
		}
	}
	if builder.Len() == 0 {
		return "storage"
	}
	return builder.String()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func sanitizePVESnippetName(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('-')
		}
	}
	if builder.Len() == 0 {
		return "vm"
	}
	return builder.String()
}

func (a *PVEAdapter) do(ctx context.Context, connection Connection, method string, endpoint string, payload map[string]any, out any) error {
	base, err := url.Parse(strings.TrimRight(connection.Endpoint, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return invalidf("valid pve endpoint is required")
	}
	endpointPath, endpointQuery, _ := strings.Cut(endpoint, "?")
	base.Path = path.Join(base.Path, "/api2/json", endpointPath)
	if endpointQuery != "" {
		base.RawQuery = endpointQuery
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal pve payload: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, base.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := a.applyPVEAuth(ctx, req, connection); err != nil {
		return err
	}
	resp, err := a.clientFor(connection).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return classifyPVEHTTPError(resp.StatusCode, endpoint, raw)
	}
	if out == nil {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return fmt.Errorf("discard pve response: %w", err)
		}
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode pve response: %w", err)
	}
	return nil
}

func (a *PVEAdapter) doTaskAndWait(ctx context.Context, connection Connection, node string, method string, endpoint string, payload map[string]any) (string, error) {
	var result struct {
		Data any `json:"data"`
	}
	if err := a.do(ctx, connection, method, endpoint, payload, &result); err != nil {
		return "", err
	}
	upid := pveUPIDFromAny(result.Data)
	if upid == "" {
		return "", nil
	}
	if err := a.waitPVETask(ctx, connection, node, upid); err != nil {
		return upid, err
	}
	return upid, nil
}

func (a *PVEAdapter) waitPVETask(ctx context.Context, connection Connection, node string, upid string) error {
	node = strings.TrimSpace(node)
	upid = strings.TrimSpace(upid)
	if node == "" || upid == "" {
		return nil
	}
	timeout := pveTaskTimeout(connection)
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(pveTaskPollInterval(connection))
	defer ticker.Stop()
	for {
		done, err := a.inspectPVETask(deadlineCtx, connection, node, upid)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		select {
		case <-deadlineCtx.Done():
			return &AdapterError{
				Reason:     "task_timeout",
				Message:    "pve task did not finish before timeout",
				NextAction: "check the PVE task log and increase taskTimeoutSeconds if the operation is expected to be slow",
			}
		case <-ticker.C:
		}
	}
}

func (a *PVEAdapter) inspectPVETask(ctx context.Context, connection Connection, node string, upid string) (bool, error) {
	var payload struct {
		Data map[string]any `json:"data"`
	}
	endpoint := fmt.Sprintf("/nodes/%s/tasks/%s/status", url.PathEscape(node), url.PathEscape(upid))
	if err := a.do(ctx, connection, http.MethodGet, endpoint, nil, &payload); err != nil {
		return false, err
	}
	status := strings.ToLower(strings.TrimSpace(stringFromAny(payload.Data["status"])))
	if status != "stopped" {
		return false, nil
	}
	exitStatus := strings.TrimSpace(stringFromAny(payload.Data["exitstatus"]))
	if exitStatus == "" || strings.EqualFold(exitStatus, "OK") {
		return true, nil
	}
	return true, &AdapterError{
		Reason:     "task_failed",
		Message:    "pve task failed: " + exitStatus,
		NextAction: "open the PVE task log for the UPID and resolve the provider-side failure",
	}
}

func pveUPIDFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(strings.TrimSpace(typed), "UPID:") {
			return strings.TrimSpace(typed)
		}
	case map[string]any:
		for _, key := range []string{"upid", "UPID", "task"} {
			if upid := pveUPIDFromAny(typed[key]); upid != "" {
				return upid
			}
		}
	}
	return ""
}

func pveTaskTimeout(connection Connection) time.Duration {
	seconds := intOptionValue(connection.Options, "taskTimeoutSeconds")
	if seconds <= 0 {
		seconds = 600
	}
	return time.Duration(seconds) * time.Second
}

func pveTaskPollInterval(connection Connection) time.Duration {
	millis := intOptionValue(connection.Options, "taskPollIntervalMillis")
	if millis <= 0 {
		millis = 1000
	}
	return time.Duration(millis) * time.Millisecond
}

func pveSyncResourceTypes(connection Connection) map[string]bool {
	raw := firstNonEmpty(
		stringOptionValue(connection.Options, "syncResourceTypes"),
		stringOptionValue(connection.Options, "syncResources"),
	)
	if strings.TrimSpace(raw) == "" {
		return map[string]bool{
			"node": true, "network": true, "qemu": true, "template": true,
			"storage": true, "storage_content": true, "iso": true, "lxc_template": true, "image": true,
		}
	}
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		key := strings.ToLower(strings.TrimSpace(part))
		switch key {
		case "nodes":
			key = "node"
		case "networks":
			key = "network"
		case "vms":
			key = "vm"
		case "templates":
			key = "template"
		case "storages":
			key = "storage"
		case "storagecontent", "storage-contents", "storage_contents":
			key = "storage_content"
		case "isos":
			key = "iso"
		case "lxc-templates", "lxc_templates":
			key = "lxc_template"
		case "images":
			key = "image"
		}
		if key != "" {
			out[key] = true
		}
	}
	return out
}

func pveAssetHealthFromError(err error) AssetHealth {
	health := AssetHealth{Status: "degraded", Message: err.Error()}
	if details, ok := AdapterErrorDetails(err); ok {
		health.Reason = details.Reason
		health.HTTPStatus = details.HTTPStatus
		health.NextAction = details.NextAction
	}
	return health
}

func classifyPVEHTTPError(status int, endpoint string, body []byte) error {
	reason := "api_unavailable"
	nextAction := "check PVE API availability and connection settings"
	lowerEndpoint := strings.ToLower(endpoint)
	lowerBody := strings.ToLower(string(body))
	switch {
	case status == http.StatusUnauthorized:
		reason = "auth_failed"
		nextAction = "rotate or re-enter the PVE API token or ticket credential"
	case status == http.StatusForbidden && strings.Contains(lowerBody, "csrf"):
		reason = "csrf_missing"
		nextAction = "provide a valid CSRF token for ticket based PVE authentication or use an API token"
	case status == http.StatusForbidden:
		reason = "permission_denied"
		nextAction = "grant the PVE credential permission for the requested node, VM, storage, or task"
	case status == http.StatusNotFound && strings.Contains(lowerEndpoint, "/storage/"):
		reason = "storage_not_found"
		nextAction = "verify the selected PVE storage or snippet storage exists on the target node"
	case status == http.StatusNotFound && strings.Contains(lowerEndpoint, "/clone"):
		reason = "template_not_found"
		nextAction = "verify the PVE template VMID/sourceRef exists on the selected node"
	case status == http.StatusNotFound && strings.Contains(lowerEndpoint, "/nodes/"):
		reason = "node_unavailable"
		nextAction = "verify the PVE node name and cluster availability"
	case status == http.StatusServiceUnavailable || status == 596:
		reason = "node_unavailable"
		nextAction = "check PVE node availability, proxy routing, and TLS settings"
	case strings.Contains(lowerBody, "value 'snippets'") && strings.Contains(lowerBody, "iso, vztmpl, import"):
		reason = "snippet_upload_unsupported"
		nextAction = "use an existing PVE cicustom snippet reference or configure a PVE-supported snippet write path"
	case strings.Contains(lowerBody, "permission"):
		reason = "permission_denied"
		nextAction = "grant the missing PVE permission for this API path"
	case strings.Contains(lowerBody, "storage"):
		reason = "storage_not_found"
		nextAction = "verify the requested storage exists and supports the requested content type"
	case strings.Contains(lowerBody, "template"):
		reason = "template_not_found"
		nextAction = "verify the template VMID/sourceRef and selected node"
	}
	message := fmt.Sprintf("pve api request failed: %s (status %d)", reason, status)
	if providerMessage := pveProviderErrorMessage(body); providerMessage != "" {
		message += ": " + providerMessage
	}
	return &AdapterError{
		Reason:     reason,
		Message:    message,
		HTTPStatus: status,
		NextAction: nextAction,
	}
}

func pveProviderErrorMessage(body []byte) string {
	raw := strings.TrimSpace(string(body))
	if raw == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		parts := []string{}
		if message := strings.TrimSpace(stringFromAny(payload["message"])); message != "" {
			parts = append(parts, message)
		}
		if errors, ok := payload["errors"].(map[string]any); ok {
			for key, value := range errors {
				if detail := strings.TrimSpace(fmt.Sprint(value)); detail != "" {
					parts = append(parts, fmt.Sprintf("%s: %s", key, detail))
				}
			}
		}
		raw = strings.TrimSpace(strings.Join(parts, "; "))
	}
	if len(raw) > 500 {
		return raw[:500] + "..."
	}
	return raw
}

func (a *PVEAdapter) clientFor(connection Connection) *http.Client {
	if !connection.InsecureSkipTLSVerify {
		return a.client
	}
	clone := *a.client
	clone.Transport = pveInsecureTransport(a.client.Transport)
	return &clone
}

func pveInsecureTransport(base http.RoundTripper) http.RoundTripper {
	transport, ok := base.(*http.Transport)
	if !ok || transport == nil {
		defaultTransport, defaultOK := http.DefaultTransport.(*http.Transport)
		if !defaultOK {
			defaultTransport = &http.Transport{}
		}
		transport = defaultTransport
	}
	clone := transport.Clone()
	if clone.TLSClientConfig == nil {
		clone.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		clone.TLSClientConfig = clone.TLSClientConfig.Clone()
		if clone.TLSClientConfig.MinVersion < tls.VersionTLS12 {
			clone.TLSClientConfig.MinVersion = tls.VersionTLS12
		}
	}
	clone.TLSClientConfig.InsecureSkipVerify = true
	return clone
}

func applyPVEAuth(req *http.Request, credential map[string]any) {
	tokenID := stringFromAny(credential["tokenID"])
	tokenSecret := stringFromAny(credential["tokenSecret"])
	if tokenID != "" && tokenSecret != "" {
		req.Header.Set("Authorization", "PVEAPIToken="+tokenID+"="+tokenSecret)
		return
	}
	ticket := stringFromAny(credential["ticket"])
	if ticket != "" {
		req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: ticket})
		if csrf := stringFromAny(credential["csrfToken"]); csrf != "" {
			req.Header.Set("CSRFPreventionToken", csrf)
		}
	}
}

func (a *PVEAdapter) applyPVEAuth(ctx context.Context, req *http.Request, connection Connection) error {
	if pveHasReusableAuth(connection.Credential) {
		applyPVEAuth(req, connection.Credential)
		return nil
	}
	username := stringFromAny(connection.Credential["username"])
	password := stringFromAny(connection.Credential["password"])
	if username == "" || password == "" {
		return nil
	}
	ticket, csrf, err := a.loginPVE(ctx, connection, username, password)
	if err != nil {
		return err
	}
	req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: ticket})
	if csrf != "" {
		req.Header.Set("CSRFPreventionToken", csrf)
	}
	return nil
}

func pveHasReusableAuth(credential map[string]any) bool {
	return stringFromAny(credential["tokenID"]) != "" && stringFromAny(credential["tokenSecret"]) != "" ||
		stringFromAny(credential["ticket"]) != ""
}

func (a *PVEAdapter) consoleBackendHeaders(ctx context.Context, connection Connection) (http.Header, error) {
	headers := http.Header{}
	tokenID := stringFromAny(connection.Credential["tokenID"])
	tokenSecret := stringFromAny(connection.Credential["tokenSecret"])
	if tokenID != "" && tokenSecret != "" {
		headers.Set("Authorization", "PVEAPIToken="+tokenID+"="+tokenSecret)
		return headers, nil
	}

	ticket := stringFromAny(connection.Credential["ticket"])
	if ticket == "" {
		username := stringFromAny(connection.Credential["username"])
		password := stringFromAny(connection.Credential["password"])
		if username != "" && password != "" {
			var err error
			ticket, _, err = a.loginPVE(ctx, connection, username, password)
			if err != nil {
				return nil, err
			}
		}
	}
	if ticket != "" {
		headers.Set("Cookie", (&http.Cookie{Name: "PVEAuthCookie", Value: ticket}).String())
	}
	return headers, nil
}

func (a *PVEAdapter) loginPVE(ctx context.Context, connection Connection, username string, password string) (string, string, error) {
	base, err := url.Parse(strings.TrimRight(connection.Endpoint, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", "", invalidf("valid pve endpoint is required")
	}
	base.Path = path.Join(base.Path, "/api2/json/access/ticket")
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.clientFor(connection).Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", "", classifyPVEHTTPError(resp.StatusCode, "/access/ticket", raw)
	}
	var payload struct {
		Data struct {
			Ticket string `json:"ticket"`
			CSRF   string `json:"CSRFPreventionToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", fmt.Errorf("decode pve ticket response: %w", err)
	}
	if payload.Data.Ticket == "" {
		return "", "", &AdapterError{
			Reason:     "auth_failed",
			Message:    "pve ticket response did not include a ticket",
			NextAction: "verify the PVE username and password credential",
		}
	}
	return payload.Data.Ticket, payload.Data.CSRF, nil
}

type pveDataEnvelope struct {
	Data []map[string]any `json:"data"`
}

func stringFromMap(values map[string]any, key string) string {
	return stringFromAny(values[key])
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return fmt.Sprintf("%.0f", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	default:
		return ""
	}
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "1" || normalized == "true" || normalized == "yes"
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	default:
		return false
	}
}

func intOptionValue(options map[string]any, key string) int {
	return intFromAny(options[key])
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		value, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return value
		}
	}
	return 0
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func pveArchitecture(value string) string {
	normalized := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "linux/")
	switch normalized {
	case "amd64", "x86_64", "x64", "x86":
		return "x86_64"
	case "arm64", "aarch64":
		return "aarch64"
	default:
		return ""
	}
}

func pveStorageSupportsContent(item map[string]any) bool {
	contentTypes := pveStorageContentSet(item)
	if len(contentTypes) == 0 {
		return true
	}
	for _, contentType := range []string{"iso", "vztmpl", "images", "rootdir", "snippets"} {
		if contentTypes[contentType] {
			return true
		}
	}
	return false
}

func pveStorageContentSet(item map[string]any) map[string]bool {
	content := strings.ToLower(strings.TrimSpace(stringFromMap(item, "content")))
	if content == "" {
		return map[string]bool{}
	}
	out := make(map[string]bool)
	for _, part := range strings.Split(content, ",") {
		if value := strings.TrimSpace(part); value != "" {
			out[value] = true
		}
	}
	return out
}

func normalizePVEMemoryMB(value string) int {
	raw := strings.TrimSpace(strings.ToLower(value))
	if raw == "" {
		return 0
	}
	multiplier := 1.0
	for _, suffix := range []struct {
		text       string
		multiplier float64
	}{
		{"gib", 1024},
		{"gi", 1024},
		{"gb", 1024},
		{"g", 1024},
		{"mib", 1},
		{"mi", 1},
		{"mb", 1},
		{"m", 1},
	} {
		if strings.HasSuffix(raw, suffix.text) {
			raw = strings.TrimSpace(strings.TrimSuffix(raw, suffix.text))
			multiplier = suffix.multiplier
			break
		}
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil || parsed <= 0 {
		return 0
	}
	return int(parsed*multiplier + 0.5)
}

func normalizePVEDiskSize(value string) string {
	raw := strings.TrimSpace(value)
	lower := strings.ToLower(raw)
	for _, suffix := range []struct {
		text       string
		multiplier float64
	}{
		{"gib", 1},
		{"gi", 1},
		{"gb", 1},
		{"g", 1},
		{"mib", 1.0 / 1024},
		{"mi", 1.0 / 1024},
		{"mb", 1.0 / 1024},
		{"m", 1.0 / 1024},
	} {
		if strings.HasSuffix(lower, suffix.text) {
			number := strings.TrimSpace(raw[:len(raw)-len(suffix.text)])
			if number == "" {
				return raw
			}
			parsed, err := strconv.ParseFloat(number, 64)
			if err != nil || parsed <= 0 {
				return raw
			}
			sizeGB := parsed * suffix.multiplier
			if sizeGB < 1 {
				sizeGB = 1
			}
			return strconv.FormatFloat(sizeGB, 'f', -1, 64)
		}
	}
	return raw
}

func pveDiskSizeGiB(value string) int {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return 0
	}
	if strings.Contains(raw, "=") {
		for _, part := range strings.Split(raw, ",") {
			key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			if ok && strings.TrimSpace(key) == "size" {
				return pveDiskSizeGiB(value)
			}
		}
	}
	normalized := normalizePVEDiskSize(raw)
	parsed, err := strconv.ParseFloat(strings.TrimSpace(normalized), 64)
	if err != nil || parsed <= 0 {
		return 0
	}
	return int(math.Ceil(parsed))
}

func normalizePVEISORef(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" || strings.Contains(strings.ToLower(raw), "media=") {
		return raw
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, ":iso/") || strings.HasSuffix(lower, ".iso") {
		return raw + ",media=cdrom"
	}
	return raw
}

func (a *PVEAdapter) nextVMID(ctx context.Context, connection Connection) (string, error) {
	var payload struct {
		Data any `json:"data"`
	}
	if err := a.do(ctx, connection, http.MethodGet, "/cluster/nextid", nil, &payload); err != nil {
		return "", err
	}
	vmid := stringFromAny(payload.Data)
	if vmid == "" {
		return "", invalidf("pve next vmid is empty")
	}
	return vmid, nil
}

func (a *PVEAdapter) fetchVM(ctx context.Context, connection Connection, node, vmid, fallbackName string) (VM, error) {
	var payload struct {
		Data any `json:"data"`
	}
	if err := a.do(ctx, connection, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu/%s/status/current", url.PathEscape(node), url.PathEscape(vmid)), nil, &payload); err != nil {
		return VM{}, err
	}
	data, _ := payload.Data.(map[string]any)
	status := firstNonEmpty(stringFromMap(data, "status"), "created")
	name := firstNonEmpty(stringFromMap(data, "name"), fallbackName)
	metadata := map[string]string{"vmid": vmid}
	if qmpStatus := stringFromMap(data, "qmpstatus"); qmpStatus != "" {
		metadata["qmpstatus"] = qmpStatus
	}
	return VM{ID: vmid, Name: name, Node: node, Status: status, Metadata: metadata}, nil
}

func (a *PVEAdapter) enrichPVEVM(ctx context.Context, connection Connection, node string, vmid string, input CreateVMInput, vm VM, metadata map[string]string) VM {
	if vm.Metadata == nil {
		vm.Metadata = map[string]string{}
	}
	vm.Metadata["vmid"] = vmid
	for key, value := range metadata {
		if strings.TrimSpace(value) != "" {
			vm.Metadata[key] = strings.TrimSpace(value)
		}
	}
	ipAddresses := append([]string(nil), vm.IPAddresses...)
	ipAddresses = append(ipAddresses, pveProviderParamIPs(input.ProviderParams)...)
	if len(ipAddresses) == 0 && strings.EqualFold(vm.Status, "running") {
		ipAddresses = append(ipAddresses, a.fetchPVEGuestAgentIPs(ctx, connection, node, vmid)...)
	}
	ipAddresses = uniqueNonEmptyStrings(ipAddresses)
	vm.IPAddresses = ipAddresses
	if len(ipAddresses) > 0 {
		vm.Metadata["ipAddress"] = ipAddresses[0]
	}
	endpoint := firstNonEmpty(
		vm.Endpoint,
		stringFromAny(input.ProviderParams["endpoint"]),
		stringFromAny(input.ProviderParams["accessUrl"]),
		stringFromAny(input.ProviderParams["agentEndpoint"]),
	)
	if endpoint == "" && len(ipAddresses) > 0 {
		endpoint = ipAddresses[0]
	}
	vm.Endpoint = endpoint
	if endpoint != "" {
		vm.Metadata["endpoint"] = endpoint
	}
	return vm
}

func (a *PVEAdapter) fetchPVEGuestAgentIPs(ctx context.Context, connection Connection, node string, vmid string) []string {
	var payload struct {
		Data any `json:"data"`
	}
	endpoint := fmt.Sprintf("/nodes/%s/qemu/%s/agent/network-get-interfaces", url.PathEscape(node), url.PathEscape(vmid))
	if err := a.do(ctx, connection, http.MethodGet, endpoint, nil, &payload); err != nil {
		return nil
	}
	return pveIPsFromAgentData(payload.Data)
}

func pveProviderParamIPs(params map[string]any) []string {
	return uniqueNonEmptyStrings([]string{
		stringFromAny(params["ipAddress"]),
		stringFromAny(params["ip"]),
		stringFromAny(params["expectedIp"]),
		stringFromAny(params["staticIp"]),
	})
}

func pveIPsFromAgentData(data any) []string {
	items := make([]any, 0)
	switch typed := data.(type) {
	case []any:
		items = append(items, typed...)
	case []map[string]any:
		for _, item := range typed {
			items = append(items, item)
		}
	case map[string]any:
		if result, ok := typed["result"]; ok {
			return pveIPsFromAgentData(result)
		}
		if interfaces, ok := typed["interfaces"]; ok {
			return pveIPsFromAgentData(interfaces)
		}
		items = append(items, typed)
	}
	ips := make([]string, 0)
	for _, item := range items {
		mapped, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(stringFromAny(mapped["name"])))
		if name == "lo" || name == "loopback" {
			continue
		}
		rawAddresses := firstNonNil(mapped["ip-addresses"], mapped["ipAddresses"], mapped["addresses"])
		for _, address := range pveAddressItems(rawAddresses) {
			ip := firstNonEmpty(
				stringFromAny(address["ip-address"]),
				stringFromAny(address["ipAddress"]),
				stringFromAny(address["address"]),
				stringFromAny(address["ip"]),
			)
			if usableIPv4Address(ip) {
				ips = append(ips, ip)
			}
		}
	}
	return uniqueNonEmptyStrings(ips)
}

func pveAddressItems(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{typed}
	default:
		return nil
	}
}

func usableIPv4Address(value string) bool {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil || ip.To4() == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	return true
}

func uniqueNonEmptyStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (a *PVEAdapter) GetVMMetrics(ctx context.Context, connection Connection, vm VM, rangeMinutes, stepSeconds int) (VMMetricsResult, error) {
	vmid := vm.Metadata["vmid"]
	node := vm.Node
	if vmid == "" || node == "" {
		return VMMetricsResult{Message: "VM metadata incomplete", Ready: false, Source: "none"}, nil
	}

	var rrdData pveDataEnvelope
	timeframe := "hour"
	if rangeMinutes > 60 {
		timeframe = "day"
	}

	err := a.do(ctx, connection, http.MethodGet,
		fmt.Sprintf("/nodes/%s/qemu/%s/rrddata?timeframe=%s", url.PathEscape(node), url.PathEscape(vmid), timeframe),
		nil, &rrdData)

	if err != nil {
		return VMMetricsResult{Message: err.Error(), Ready: false, Source: "pve-rrd"}, nil
	}

	var series []MetricSeries
	cpuPoints := []MetricPoint{}
	memPoints := []MetricPoint{}
	netRxPoints := []MetricPoint{}
	netTxPoints := []MetricPoint{}

	for _, point := range rrdData.Data {
		ts := int64(0)
		if timeVal, ok := point["time"].(float64); ok {
			ts = int64(timeVal)
		}
		if cpu, ok := point["cpu"].(float64); ok {
			cpuPoints = append(cpuPoints, MetricPoint{Timestamp: ts, Value: cpu * 100})
		}
		if mem, ok := point["mem"].(float64); ok {
			memPoints = append(memPoints, MetricPoint{Timestamp: ts, Value: mem})
		}
		if netin, ok := point["netin"].(float64); ok {
			netRxPoints = append(netRxPoints, MetricPoint{Timestamp: ts, Value: netin})
		}
		if netout, ok := point["netout"].(float64); ok {
			netTxPoints = append(netTxPoints, MetricPoint{Timestamp: ts, Value: netout})
		}
	}

	if len(cpuPoints) > 0 {
		series = append(series, MetricSeries{Key: "cpu", Label: "CPU", Unit: "percent", Points: cpuPoints})
	}
	if len(memPoints) > 0 {
		series = append(series, MetricSeries{Key: "memory", Label: "Memory", Unit: "bytes", Points: memPoints})
	}
	if len(netRxPoints) > 0 {
		series = append(series, MetricSeries{Key: "networkRx", Label: "Network RX", Unit: "bytes/s", Points: netRxPoints})
	}
	if len(netTxPoints) > 0 {
		series = append(series, MetricSeries{Key: "networkTx", Label: "Network TX", Unit: "bytes/s", Points: netTxPoints})
	}

	return VMMetricsResult{Series: series, Ready: len(series) > 0, Source: "pve-rrd"}, nil
}

func (a *PVEAdapter) GetConsoleURL(ctx context.Context, connection Connection, vm VM) (ConsoleURLResult, error) {
	vmid := vm.Metadata["vmid"]
	node := vm.Node
	if vmid == "" || node == "" {
		return ConsoleURLResult{Message: "VM metadata incomplete", Ready: false, Provider: "pve", Type: "novnc", ProxyMode: "backend-ws-proxy"}, nil
	}

	var ticketResponse struct {
		Data struct {
			Ticket string `json:"ticket"`
			Port   any    `json:"port"`
		} `json:"data"`
	}

	err := a.do(ctx, connection, http.MethodPost,
		fmt.Sprintf("/nodes/%s/qemu/%s/vncproxy", url.PathEscape(node), url.PathEscape(vmid)),
		map[string]any{"websocket": "1"},
		&ticketResponse)

	if err != nil {
		return ConsoleURLResult{Message: err.Error(), Ready: false, Provider: "pve", Type: "novnc", ProxyMode: "backend-ws-proxy"}, err
	}

	port := stringFromAny(ticketResponse.Data.Port)
	if ticketResponse.Data.Ticket == "" || port == "" {
		return ConsoleURLResult{
			Message:   "pve vncproxy response did not include a console ticket or port",
			Ready:     false,
			Provider:  "pve",
			Type:      "novnc",
			ProxyMode: "backend-ws-proxy",
		}, nil
	}

	base, err := url.Parse(strings.TrimRight(connection.Endpoint, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return ConsoleURLResult{Message: "valid pve endpoint is required", Ready: false, Provider: "pve", Type: "novnc", ProxyMode: "backend-ws-proxy"}, invalidf("valid pve endpoint is required")
	}
	base.Path = path.Join(base.Path, fmt.Sprintf("/api2/json/nodes/%s/qemu/%s/vncwebsocket", url.PathEscape(node), url.PathEscape(vmid)))
	query := base.Query()
	query.Set("port", port)
	query.Set("vncticket", ticketResponse.Data.Ticket)
	base.RawQuery = query.Encode()
	backendHeaders, err := a.consoleBackendHeaders(ctx, connection)
	if err != nil {
		return ConsoleURLResult{Message: err.Error(), Ready: false, Provider: "pve", Type: "novnc", ProxyMode: "backend-ws-proxy"}, err
	}

	return ConsoleURLResult{
		Type:           "novnc",
		URL:            fmt.Sprintf("/api/v1/virtualization/vms/%s/console/novnc", vm.ID),
		BackendURL:     base.String(),
		Token:          ticketResponse.Data.Ticket,
		Ready:          true,
		Provider:       "pve",
		ProxyMode:      "backend-ws-proxy",
		BackendHeaders: backendHeaders,
		BackendTLS:     BackendTLS{InsecureSkipVerify: connection.InsecureSkipTLSVerify},
	}, nil
}

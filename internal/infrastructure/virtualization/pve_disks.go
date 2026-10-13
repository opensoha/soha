package virtualization

import (
	"fmt"
	"regexp"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

func pveResizePreflightError(err error) error {
	details, ok := AdapterErrorDetails(err)
	if !ok {
		details = AdapterError{Cause: err, Reason: "provider_preflight_failed", Message: err.Error()}
	}
	details.NoProviderWrite = true
	return &details
}

var pveStorageIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var pveConfigDigestPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

// Validate the complete disk plan before writing CPU, memory or any disk.
func preparePVEDiskChanges(config map[string]any, disks []domain.AdapterDiskChange, defaultStorage string) (map[string]any, []domain.AdapterDiskChange, error) {
	shadow := make(map[string]any, len(config)+len(disks))
	for key, value := range config {
		shadow[key] = value
	}
	additions := map[string]any{}
	resizes := []domain.AdapterDiskChange{}
	seen := map[string]bool{}
	for _, disk := range disks {
		id := firstNonEmpty(disk.ID, disk.Name)
		if id == "" && disk.Add {
			id = nextPVEDeviceID(shadow, "scsi")
		}
		if !isPVEDiskID(id) || seen[id] || disk.SizeGiB <= 0 || !disk.ValidSerial() {
			return nil, nil, invalidf("invalid or duplicate disk change")
		}
		seen[id], disk.ID = true, id
		if !disk.Add {
			current := stringFromAny(shadow[id])
			if current == "" || pveConfigOption(current, "media") == "cdrom" || disk.SizeGiB < pveDiskSizeGiB(current) {
				return nil, nil, invalidf("disk expansion requires an existing disk and cannot shrink it")
			}
			resizes = append(resizes, disk)
			continue
		}
		value, err := preparePVEDiskAddition(shadow, id, disk, defaultStorage)
		if err != nil {
			return nil, nil, err
		}
		if value != "" {
			additions[id], shadow[id] = value, value
		}
	}
	return additions, resizes, nil
}

func preparePVEDiskAddition(config map[string]any, id string, disk domain.AdapterDiskChange, defaultStorage string) (string, error) {
	storage := firstNonEmpty(disk.Storage, defaultStorage)
	if !pveStorageIDPattern.MatchString(storage) {
		return "", invalidf("storage is required for a new disk")
	}
	if raw, occupied := config[id]; occupied {
		current := stringFromAny(raw)
		currentStorage, _ := pveStorageFromVolume(current)
		if disk.Serial != "" && pveConfigOption(current, "serial") == disk.Serial && currentStorage == storage && pveDiskSizeGiB(current) == disk.SizeGiB && pveConfigOption(current, "media") != "cdrom" {
			return "", nil
		}
		return "", invalidf("new disk slot is occupied; reconcile the original disk instead of replacing it")
	}
	if disk.Serial != "" {
		for key, raw := range config {
			if isPVEDiskID(key) && pveConfigOption(stringFromAny(raw), "serial") == disk.Serial {
				return "", invalidf("disk serial is already attached to another slot")
			}
		}
	}
	value := fmt.Sprintf("%s:%d", storage, disk.SizeGiB)
	if disk.Serial != "" {
		value += ",serial=" + disk.Serial
	}
	return value, nil
}

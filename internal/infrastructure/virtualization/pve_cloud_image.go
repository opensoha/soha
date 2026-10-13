package virtualization

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

var pveCloudImageRefPattern = regexp.MustCompile(`^([A-Za-z0-9_-]{1,64}):import/([a-f0-9]{64})\.qcow2$`)

func pveCloudImageSource(source string) (string, string, error) {
	match := pveCloudImageRefPattern.FindStringSubmatch(source)
	if match == nil {
		return "", "", invalidf("cloud image source must be a checksum-named QCOW2 import volume")
	}
	return match[1], match[2], nil
}

func (a *PVEAdapter) inspectPVECloudImage(ctx context.Context, connection Connection, input CreateVMInput) (domain.VMIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	storage, digest, err := pveCloudImageSource(input.SourceRef)
	if err != nil {
		return domain.VMIdentity{}, err
	}
	node := firstNonEmpty(input.Node, stringOptionValue(connection.Options, "defaultNode"))
	if node == "" {
		return domain.VMIdentity{}, invalidf("cloud image node is required")
	}
	var config struct {
		Data struct {
			Type    string `json:"type"`
			Path    string `json:"path"`
			Content string `json:"content"`
			Nodes   string `json:"nodes"`
		} `json:"data"`
	}
	if err = a.do(ctx, connection, http.MethodGet, "/storage/"+url.PathEscape(storage), nil, &config); err != nil {
		return domain.VMIdentity{}, err
	}
	if config.Data.Type != "dir" || !path.IsAbs(config.Data.Path) || !slicesContainsCSV(config.Data.Content, "import") || (config.Data.Nodes != "" && !slicesContainsCSV(config.Data.Nodes, node)) {
		return domain.VMIdentity{}, invalidf("cloud image import storage is unavailable on the selected node")
	}
	client, err := pveSSHClient(ctx, connection, node, input)
	if err != nil {
		return domain.VMIdentity{}, err
	}
	defer func() { _ = client.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	session, err := client.NewSession()
	if err != nil {
		return domain.VMIdentity{}, err
	}
	defer func() { _ = session.Close() }()
	source := path.Join(config.Data.Path, "import", digest+".qcow2")
	var output bytes.Buffer
	session.Stdout = &cloudImageOutput{writer: &output, remaining: 64 << 10}
	// The immutable flag prevents another upload or rename replacing a verified source before import.
	command := "test -f " + shellQuote(source) + " && test ! -L " + shellQuote(source) + " && lsattr -d " + shellQuote(source) + " | awk '$1 ~ /i/ { found=1 } END { exit !found }' && sha256sum -- " + shellQuote(source) + " && qemu-img info --output=json " + shellQuote(source)
	if err = session.Run(command); err != nil {
		return domain.VMIdentity{}, fmt.Errorf("inspect cloud image: %w", err)
	}
	if err = validatePVECloudImageInfo(output.Bytes(), digest, input.DiskSize); err != nil {
		return domain.VMIdentity{}, err
	}
	return domain.VMIdentity{ProviderVMID: input.SourceRef, Name: digest + ".qcow2", Node: node, Generation: "sha256:" + digest}, nil
}

func slicesContainsCSV(list, value string) bool {
	for _, part := range strings.Split(list, ",") {
		if strings.TrimSpace(part) == value {
			return true
		}
	}
	return false
}

type cloudImageOutput struct {
	writer    io.Writer
	remaining int
}

func (w *cloudImageOutput) Write(data []byte) (int, error) {
	if len(data) > w.remaining {
		return 0, fmt.Errorf("cloud image inspection output exceeds limit")
	}
	n, err := w.writer.Write(data)
	w.remaining -= n
	return n, err
}

func validatePVECloudImageInfo(output []byte, digest, diskSize string) error {
	first, rest, ok := strings.Cut(string(output), "\n")
	fields := strings.Fields(first)
	if !ok || len(fields) != 2 || fields[0] != digest {
		return invalidf("cloud image digest changed or is unavailable")
	}
	var info struct {
		Format      string `json:"format"`
		Size        int64  `json:"virtual-size"`
		Backing     string `json:"backing-filename"`
		FullBacking string `json:"full-backing-filename"`
		Specific    struct {
			Data struct {
				DataFile string `json:"data-file"`
			} `json:"data"`
		} `json:"format-specific"`
	}
	if json.Unmarshal([]byte(rest), &info) != nil || info.Format != "qcow2" || info.Size <= 0 || info.Size > 1<<60 || info.Backing != "" || info.FullBacking != "" {
		return invalidf("cloud image must be a standalone QCOW2 without backing files")
	}
	if info.Specific.Data.DataFile != "" {
		return invalidf("cloud image must not reference an external data file")
	}
	if diskSize != "" {
		requested, err := strconv.ParseInt(normalizePVEDiskSize(diskSize), 10, 64)
		if err != nil || requested < 1 || (info.Size+(1<<30)-1)/(1<<30) > requested {
			return invalidf("root disk is smaller than cloud image virtual size")
		}
	}
	return nil
}

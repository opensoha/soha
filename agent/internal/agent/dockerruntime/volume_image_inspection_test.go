package dockerruntime

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"text/template"

	protocol "github.com/opensoha/soha/contracts/docker/runtime"
)

func TestReaderImageInspectionSupportsOmittedDockerConfigKeys(t *testing.T) {
	for _, populated := range []bool{false, true} {
		name := "omitted"
		if populated {
			name = "populated"
		}
		t.Run(name, func(t *testing.T) {
			config := map[string]any{"Labels": map[string]string{readerProtocolLabel: protocol.ReadProtocol}}
			if populated {
				config["Env"] = []string{"UNSAFE=value"}
				config["Volumes"] = map[string]any{"/unsafe": map[string]any{}}
			}
			item := map[string]any{"Id": "sha256:" + strings.Repeat("a", 64), "Os": "linux", "Config": config}
			format, err := template.New("inspect").Option("missingkey=error").Funcs(template.FuncMap{
				"json": func(value any) (string, error) { raw, err := json.Marshal(value); return string(raw), err },
			}).Parse(readerImageInspectionFormat)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := format.Execute(&out, item); err != nil {
				t.Fatal(err)
			}
			var projected struct {
				Env     []string          `json:"env"`
				Volumes map[string]any    `json:"volumes"`
				Labels  map[string]string `json:"labels"`
			}
			if json.Unmarshal(out.Bytes(), &projected) != nil || projected.Labels[readerProtocolLabel] != protocol.ReadProtocol || (len(projected.Env) != 0) != populated || (len(projected.Volumes) != 0) != populated {
				t.Fatal("image config projection omitted a configured security field")
			}
		})
	}
}

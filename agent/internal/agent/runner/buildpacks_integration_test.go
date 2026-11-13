package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

// The caller provisions a dedicated daemon, preloaded immutable toolchain and
// disposable authenticated registry. Source Git is served by the SSH fixture.
type buildpacksIntegrationConfig struct {
	Runtime                                                     cfgpkg.BuildpacksConfig
	WorkspaceRoot, ImagePrefix, RegistryAuth, EvidenceDirectory string
}

func readBuildpacksIntegrationConfig(t *testing.T) buildpacksIntegrationConfig {
	t.Helper()
	configuration := os.Getenv("SOHA_BUILDPACKS_INTEGRATION_CONFIG")
	if configuration == "" {
		t.Skip("requires an isolated Buildpacks daemon and private registry")
	}
	workspace, err := os.OpenRoot(filepath.Dir(configuration))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspace.Close() }()
	data, err := workspace.ReadFile(filepath.Base(configuration))
	if err != nil {
		t.Fatal(err)
	}
	var fixture buildpacksIntegrationConfig
	if err := json.Unmarshal(data, &fixture); err != nil || fixture.RegistryAuth == "" || fixture.EvidenceDirectory == "" {
		t.Fatal("invalid Buildpacks integration configuration", err)
	}
	return fixture
}

func TestBuildpacksLanguageIntegration(t *testing.T) {
	fixture := readBuildpacksIntegrationConfig(t)
	r := New(cfgpkg.ControlPlaneConfig{AgentID: "buildpacks-integration", BaseURL: "http://fixture.invalid", WorkspaceRoot: fixture.WorkspaceRoot, Buildpacks: fixture.Runtime, DefaultTimeout: 20 * time.Minute}, nil)
	if err := os.MkdirAll(fixture.EvidenceDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	library, libCommit := buildpacksGitFixture(t, map[string]string{"README.md": "explicitly bound private submodule\n"}, "")
	files := buildpacksLanguageFiles()
	files[".gitmodules"] = "[submodule \"lib\"]\npath = lib\nurl = ../lib.git\n"
	main, mainCommit := buildpacksGitFixture(t, files, libCommit)
	base, secrets, _ := buildpacksSSHFixture(t, map[string]string{"/app.git": main, "/lib.git": library})
	secrets["REGISTRY_AUTH"] = fixture.RegistryAuth
	ctx := context.WithValue(t.Context(), secretValuesContextKey{}, secrets)
	r.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var callback callbackRequest
		if err := json.NewDecoder(request.Body).Decode(&callback); err != nil {
			return nil, err
		}
		return jsonResponse(t, 200, map[string]any{"data": ExecutionTask{ID: callback.CallbackToken, Status: "running"}}), nil
	})}
	for _, language := range []string{"go", "node", "python", "java", "go"} {
		t.Run(language, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
			defer cancel()
			applicationID := "buildpacks-integration-" + language
			capability := r.BuildpacksCapability(ctx, applicationID)
			if !capability.Ready || !capability.SupportsSSH || !capability.SupportsSubmodules {
				t.Fatalf("integration runner unavailable: %s", capability.Reason)
			}
			image := fixture.ImagePrefix + "/" + language + ":" + fmt.Sprint(time.Now().UnixNano())
			spec := sohaapi.BuildpacksExecutionSpec{Configuration: *capability.Configuration, PackVersion: capability.PackVersion, Runtime: sohaapi.BuildpacksExecutionSpecRuntime(capability.Runtime), RuntimeVersion: capability.RuntimeVersion, LifecycleImage: capability.LifecycleImage, ContextDir: language, Environment: map[string]string{}}
			if language == "java" {
				spec.Environment["BP_JVM_VERSION"] = "21"
			}
			task := ExecutionTask{ID: t.Name(), TaskKind: "build", CallbackToken: t.Name(), ProviderKind: capability.ProviderKind, ApplicationID: applicationID, Payload: map[string]any{
				"image": image, "buildpacks": spec, "workspace": map[string]any{"checkouts": []buildpacksCheckout{
					{RepositoryURL: base + "/app.git", RefType: "commit", RefName: mainCommit, Submodules: true},
					{RepositoryURL: base + "/lib.git", RefType: "commit", RefName: libCommit, CheckoutPath: "lib"},
				}},
			}}
			started := time.Now()
			result, err := r.runBuildpacks(ctx, task)
			elapsed := time.Since(started)
			if err != nil {
				t.Fatalf("real %s build failed: %v; %v", language, err, redactResolvedSecretValues(ctx, result))
			}
			sbom, ok := result["sbom"].([]map[string]any)
			if !ok || len(sbom) == 0 {
				t.Fatal("real build did not produce SBOM evidence")
			}
			// Verify execution of the pushed immutable artifact, then remove that container.
			home := t.TempDir()
			if err := writeBuildpacksRegistryConfig(home, fixture.RegistryAuth); err != nil {
				t.Fatal(err)
			}
			ref := image[:strings.LastIndex(image, ":")] + "@" + fmt.Sprint(result["imageDigest"])
			output, err := runBuildpacksIntegrationImage(ctx, r, home, ref)
			if err != nil || !strings.Contains(output, "soha-r5-"+language) {
				t.Fatal("built image did not execute the language sample", err, redactResolvedSecretValues(ctx, output))
			}
			for _, directory := range []string{"source", "credentials"} {
				if _, err := os.Stat(filepath.Join(fmt.Sprint(result["workspacePath"]), directory)); !os.IsNotExist(err) {
					t.Fatal("private build workspace was retained")
				}
			}
			result["sourceCommit"], result["submoduleCommit"], result["elapsedSeconds"], result["startupOutput"] = mainCommit, libCommit, elapsed.Seconds(), output
			result["platformEmulationAllowed"] = fixture.Runtime.AllowPlatformEmulation
			var cacheBytes int64
			if fixture.Runtime.Runtime != "podman" {
				volumes, err := r.buildpacksCacheVolumes(ctx)
				if err != nil {
					t.Fatal(err)
				}
				_, cacheBytes, err = buildpacksCacheEvictions(volumes, fixture.Runtime.CacheMaxBytes)
				if err != nil {
					t.Fatal(err)
				}
			}
			result["cacheBytes"] = cacheBytes
			encoded, err := json.MarshalIndent(redactResolvedSecretValues(ctx, result), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			name := strings.ReplaceAll(t.Name(), "/", "-") + ".json"
			if err := os.WriteFile(filepath.Join(fixture.EvidenceDirectory, name), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s %s completed in %.1fs with %d SBOM files", language, fixture.Runtime.Platform, elapsed.Seconds(), len(sbom))
		})
	}
	t.Run("cache-capacity", func(t *testing.T) {
		if fixture.Runtime.Runtime == "podman" {
			t.Skip("daemonless lifecycle has no retained build cache")
		}
		volumes, err := r.buildpacksCacheVolumes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, before, err := buildpacksCacheEvictions(volumes, 1)
		if err != nil || before == 0 {
			t.Fatal("capacity proof requires populated build cache", before, err)
		}
		env := r.buildpacksEnvironment("")
		const sentinel = "soha-r5-unrelated-cache-check"
		if _, err := buildpacksCommand(ctx, "", env, "docker", "volume", "create", sentinel); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = buildpacksCommand(ctx, "", env, "docker", "volume", "rm", sentinel) }()
		r.cfg.Buildpacks.CacheMaxBytes = 1
		if err := r.trimBuildpacksCache(ctx); err != nil {
			t.Fatal(err)
		}
		volumes, err = r.buildpacksCacheVolumes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, size, err := buildpacksCacheEvictions(volumes, 1)
		if err != nil || size != 0 {
			t.Fatal("cache capacity was not enforced", size, err)
		}
		if _, err := buildpacksCommand(ctx, "", env, "docker", "volume", "inspect", sentinel); err != nil {
			t.Fatal("unrelated volume was removed", err)
		}
		t.Log("owned cache evicted at byte limit; unrelated volume preserved")
	})
}

func runBuildpacksIntegrationImage(ctx context.Context, r *Runner, home, ref string) (string, error) {
	if r.cfg.Buildpacks.Runtime != "podman" {
		return buildpacksCommand(ctx, "", r.buildpacksEnvironment(home), "docker", "run", "--rm", "--memory", "512m", "--env", "BPL_JVM_THREAD_COUNT=20", ref)
	}
	args := []string{"pull", "--authfile", filepath.Join(home, "config.json"), "--platform", r.cfg.Buildpacks.Platform}
	registry, _, _ := strings.Cut(ref, "/")
	if containsString(r.cfg.Buildpacks.InsecureRegistries, registry) {
		args = append(args, "--tls-verify=false")
	}
	if _, err := r.buildpacksPodman(ctx, home, append(args, ref)...); err != nil {
		return "", err
	}
	return r.buildpacksPodman(ctx, home, "run", "--rm", "--pull=never", "--cgroups=disabled", "--network=none", "--cap-drop=all", "--security-opt=no-new-privileges", "--env", "BPL_JVM_THREAD_COUNT=20", ref)
}

func buildpacksLanguageFiles() map[string]string {
	return map[string]string{
		"go/go.mod":                    "module example.invalid/soha-r5\n\ngo 1.26.0\n",
		"go/main.go":                   "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"soha-r5-go\")}\n",
		"node/package.json":            `{"name":"soha-r5-node","version":"1.0.0","scripts":{"start":"node index.js"}}`,
		"node/index.js":                "console.log('soha-r5-node')\n",
		"python/requirements.txt":      "",
		"python/Procfile":              "web: python main.py\n",
		"python/main.py":               "print('soha-r5-python')\n",
		"java/pom.xml":                 `<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion><groupId>io.soha</groupId><artifactId>sample</artifactId><version>1.0</version><properties><maven.compiler.source>21</maven.compiler.source><maven.compiler.target>21</maven.compiler.target></properties><build><plugins><plugin><groupId>org.apache.maven.plugins</groupId><artifactId>maven-jar-plugin</artifactId><version>3.4.2</version><configuration><archive><manifest><mainClass>Main</mainClass></manifest></archive></configuration></plugin></plugins></build></project>`,
		"java/src/main/java/Main.java": `public class Main { public static void main(String[] args) { System.out.println("soha-r5-java"); } }`,
	}
}

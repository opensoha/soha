import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
from tempfile import TemporaryDirectory
import unittest

spec = importlib.util.spec_from_file_location("ci_impact", Path(__file__).with_name("ci-impact.py"))
impact = importlib.util.module_from_spec(spec)
spec.loader.exec_module(impact)


class ImpactTests(unittest.TestCase):
    def assert_selection(self, data, selected):
        self.assertEqual(impact.select(data), {job: job in selected for job in impact.JOBS})

    def test_all_eight_required_job_identities_are_explicit(self):
        self.assertEqual(impact.JOBS, ("core", "web", "contracts", "cli", "agent", "connectors", "skills", "deletion"))

    def test_first_import_baseline_requires_exact_reviewed_tree_and_ancestry(self):
        with TemporaryDirectory(prefix="soha-web-import-base-") as directory:
            root = Path(directory)
            def git(*args, data=None):
                return subprocess.check_output(["git", "-C", directory, *args], input=data, text=True).strip()
            git("init", "--quiet", "-b", "main")
            git("config", "user.name", "Test")
            git("config", "user.email", "test@example.invalid")
            (root / "README.md").write_text("Core")
            git("add", ".")
            git("commit", "--quiet", "-m", "Core baseline")
            core = git("rev-parse", "HEAD")
            git("checkout", "--quiet", "--orphan", "raw-web")
            git("rm", "--quiet", "-rf", ".")
            (root / "package.json").write_text('{"name":"soha-web"}')
            (root / "existing-debt.ts").write_text("reviewed existing debt")
            git("add", ".")
            git("commit", "--quiet", "-m", "Reviewed Web")
            web = git("rev-parse", "HEAD")
            git("checkout", "--quiet", "main")
            git("read-tree", "--prefix=web/", "-u", web)
            imported = git("commit-tree", git("write-tree"), "-p", core, "-p", web, data="Import Web\n")
            git("update-ref", "HEAD", imported, core)
            manifest = root / "contracts/compat/consumer-revisions.json"
            manifest.parent.mkdir(parents=True)
            manifest.write_text(json.dumps({"soha-web": web}))
            git("add", ".")
            git("commit", "--quiet", "-m", "Source workspace")
            workspace = git("rev-parse", "HEAD")
            merge = git("commit-tree", git("rev-parse", "HEAD^{tree}"), "-p", core, "-p", workspace, data="PR merge\n")
            git("update-ref", "HEAD", merge, workspace)
            self.assertEqual(impact.web_baseline(core, root), imported)
            self.assertEqual(impact.web_baseline(imported, root), imported)
            with self.assertRaises(ValueError):
                impact.web_baseline("main", root)
            manifest.write_text(json.dumps({"soha-web": core}))
            with self.assertRaisesRegex(ValueError, "differs"):
                impact.web_baseline(core, root)

    def test_cli_reads_actual_git_rename_and_delete_and_falls_back_on_bad_event(self):
        with TemporaryDirectory(prefix="soha-ci-impact-") as directory:
            root = Path(directory)
            def git(*args):
                return subprocess.check_output(["git", "-C", directory, *args], text=True).strip()
            git("init", "--quiet")
            git("config", "user.name", "Test")
            git("config", "user.email", "test@example.invalid")
            (root / "contracts").mkdir()
            (root / "contracts/old.json").write_text("old")
            (root / "contracts/deleted.json").write_text("delete")
            git("add", ".")
            git("commit", "--quiet", "-m", "base")
            base = git("rev-parse", "HEAD")
            (root / "contracts/old.json").rename(root / "renamed.md")
            (root / "contracts/deleted.json").unlink()
            git("add", "-A")
            git("commit", "--quiet", "-m", "rename and delete")
            event = root / "event.json"
            event.write_text(json.dumps({"before": base}))
            output = root / "output"
            env = {**os.environ, "GITHUB_EVENT_PATH": str(event), "GITHUB_EVENT_NAME": "push", "GITHUB_SHA": git("rev-parse", "HEAD"), "GITHUB_OUTPUT": str(output)}
            for data in [json.dumps({"before": base}), "broken json"]:
                event.write_text(data)
                output.write_text("")
                result = subprocess.run([os.sys.executable, str(Path(impact.__file__).resolve()), "route"], cwd=root, env=env, capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(output.read_text().splitlines(), [f"{job}=true" for job in impact.JOBS])

    def test_component_and_documentation_paths(self):
        self.assertFalse(any(impact.select(b"M\0docs/guide.md\0").values()))
        self.assert_selection(b"M\0web/src/example.test.ts\0", {"core", "web"})
        self.assert_selection(b"M\0internal/example_test.go\0", {"core", "deletion"})

    def test_new_component_source_tests_fixtures_and_generated_paths(self):
        examples = {
            "cli": ["cli/cmd/soha/main.go", "cli/internal/fixture_test.go", "cli/testdata/prompt.md", "cli/npm/cli/bin/soha.js"],
            "agent": ["agent/cmd/agent/main.go", "agent/runtimes/dsh/service.test.mjs", "agent/testdata/prompt.md", "agent/generated/types.go"],
            "connectors": ["connectors/src/index.ts", "connectors/test/example.test.ts", "connectors/fixtures/example.json", "connectors/dist/index.js"],
        }
        for job, paths in examples.items():
            for path in paths:
                for status in ("A", "M", "D"):
                    with self.subTest(path=path, status=status):
                        self.assert_selection(f"{status}\0{path}\0".encode(), {job})

    def test_skills_markdown_assets_and_catalogs_select_real_consumers(self):
        for path in ["skills/skills/example/SKILL.md", "skills/agent-skills/soha/SKILL.md",
                     "skills/agent-skills/soha/agents/openai.yaml", "skills/mcp-presets/default.json",
                     "skills/agent-profiles/default.json", "skills/catalog/README.md",
                     "skills/schemas/skill.schema.json", "skills/tools/validate_assets.py"]:
            for status in ("A", "M", "D"):
                with self.subTest(path=path, status=status):
                    self.assert_selection(f"{status}\0{path}\0".encode(), {"skills", "cli", "agent"})

    def test_shared_mvs_hoist_manifests_and_locks_select_every_gate(self):
        for path in ["go.mod", "go.sum", "go.work", "go.work.sum", "cli/go.mod", "cli/go.sum",
                     "agent/go.mod", "agent/go.sum", "contracts/go.mod", "package.json", "package-lock.json",
                     "contracts/package.json", "web/package.json", "connectors/package.json", "cli/npm/cli/package.json",
                     "connectors/package-lock.json", "cli/npm/cli/package-lock.json", "agent/unexpected/package-lock.json",
                     "agent/npm-shrinkwrap.json", "scripts/source-workspace.py", "Makefile", ".prettierignore", ".prettierrc.json"]:
            for status in ("M", "D"):
                with self.subTest(path=path, status=status):
                    self.assertTrue(all(impact.select(f"{status}\0{path}\0".encode()).values()))

    def test_independent_dsh_runtime_lock_and_manifest_select_only_agent(self):
        for path in ("agent/runtimes/dsh/package.json", "agent/runtimes/dsh/package-lock.json"):
            for status in ("A", "M", "D"):
                with self.subTest(path=path, status=status):
                    self.assert_selection(f"{status}\0{path}\0".encode(), {"agent"})

    def test_documentation_exceptions_do_not_hide_runtime_markdown_or_unknown_paths(self):
        for path in ["README.md", "docs/design.md", "web/docs/testing.md", "cli/docs/usage.md",
                     "agent/docs/usage.md", "connectors/docs/usage.md", "skills/docs/usage.md", "skills/README.md"]:
            with self.subTest(path=path):
                self.assert_selection(f"M\0{path}\0".encode(), set())
        self.assert_selection(b"D\0cli/README.md\0", {"cli"})
        self.assert_selection(b"D\0skills/README.md\0", {"skills", "cli", "agent"})
        for path in ["new-tool.conf", "unknown/docs/guide.md", "unknown.md", "docs/runtime.py", ".prettierrc"]:
            with self.subTest(path=path):
                self.assertTrue(all(impact.select(f"M\0{path}\0".encode()).values()))

    def test_all_imported_workflow_paths_remain_shared_inputs(self):
        for component in ("cli", "agent", "connectors", "skills", "web"):
            for status in ("M", "D"):
                self.assertTrue(all(impact.select(f"{status}\0{component}/.github/workflows/ci.yml\0".encode()).values()))

    def test_rename_and_copy_union_both_component_paths(self):
        for status in ("R100", "C100"):
            self.assert_selection(f"{status}\0cli/src/old.go\0agent/src/new.go\0".encode(), {"cli", "agent"})
            self.assert_selection(f"{status}\0skills/skills/old/SKILL.md\0docs/old.md\0".encode(), {"skills", "cli", "agent"})
            self.assertTrue(all(impact.select(f"{status}\0agent/runtimes/dsh/package-lock.json\0package-lock.json\0".encode()).values()))

    def test_rename_delete_shared_generated_and_unknown(self):
        for diff in [b"R100\0contracts/gen/sdk.ts\0docs/sdk.md\0", b"D\0contracts/fixtures/a.json\0", b"M\0package-lock.json\0", b"M\0web/.github/workflows/ci.yml\0", b"M\0new-tool.conf\0", b"M\0.github/workflows/ci.yml\0"]:
            with self.subTest(diff=diff):
                self.assertTrue(all(impact.select(diff).values()))
        self.assertTrue(impact.select(b"R100\0web/src/a.ts\0docs/a.md\0")["web"])
        self.assertTrue(impact.select(b"D\0internal/old.go\0")["core"])

    def test_malformed_diff_fails_closed(self):
        for diff in [b"M\0a", b"R100\0a\0", b"WHAT\0a\0", b"M\0../a\0", b"M\0\xff\0"]:
            self.assertTrue(all(impact.select(diff).values()))

    def test_required_results_cannot_be_hidden(self):
        needs = {job: {"result": "success"} for job in impact.JOBS}
        needs["route"] = {"result": "success", "outputs": dict.fromkeys(impact.JOBS, "true")}
        impact.check_summary(needs)
        for job in impact.JOBS:
            for state in ["skipped", "cancelled", "failure", None]:
                broken = {**needs, job: {"result": state}}
                with self.subTest(job=job, state=state), self.assertRaises(ValueError):
                    impact.check_summary(broken)
            with self.subTest(missing=job), self.assertRaises(ValueError):
                impact.check_summary({key: value for key, value in needs.items() if key != job})
        for broken in [{**needs, "route": {"result": "failure"}}, {**needs, "route": {"result": "success", "outputs": {}}}, {key: value for key, value in needs.items() if key != "web"}]:
            with self.assertRaises(ValueError):
                impact.check_summary(broken)
        needs["route"]["outputs"]["web"] = "false"
        needs["web"]["result"] = "skipped"
        impact.check_summary(needs)
        needs["web"]["result"] = "cancelled"
        with self.assertRaises(ValueError):
            impact.check_summary(needs)

    def test_invalid_route_and_job_shapes_fail_closed_for_each_job(self):
        for value in (None, [], True):
            with self.assertRaises(ValueError):
                impact.check_summary(value)
        needs = {job: {"result": "success"} for job in impact.JOBS}
        for route in (None, [], {"result": "cancelled"}, {"result": "success", "outputs": []}):
            with self.subTest(route=route), self.assertRaises(ValueError):
                impact.check_summary({**needs, "route": route})
        for job in impact.JOBS:
            for invalid in (None, True, "TRUE", "", [], {}):
                outputs = dict.fromkeys(impact.JOBS, "true")
                outputs[job] = invalid
                with self.subTest(job=job, invalid=invalid), self.assertRaises(ValueError):
                    impact.check_summary({**needs, "route": {"result": "success", "outputs": outputs}})
            outputs = dict.fromkeys(impact.JOBS, "true")
            del outputs[job]
            with self.assertRaises(ValueError):
                impact.check_summary({**needs, "route": {"result": "success", "outputs": outputs}})
            for entry in (None, [], "success"):
                with self.assertRaises(ValueError):
                    impact.check_summary({**needs, job: entry, "route": {"result": "success", "outputs": dict.fromkeys(impact.JOBS, "true")}})

    def test_only_explicitly_unselected_success_or_skip_can_pass(self):
        for job in impact.JOBS:
            outputs = dict.fromkeys(impact.JOBS, "true")
            outputs[job] = "false"
            for state in ("success", "skipped", "cancelled", "failure", None):
                needs = {name: {"result": "success"} for name in impact.JOBS}
                needs.update(route={"result": "success", "outputs": outputs})
                needs[job] = {"result": state}
                if state in ("success", "skipped"):
                    impact.check_summary(needs)
                else:
                    with self.assertRaises(ValueError):
                        impact.check_summary(needs)


class WorkflowWiringTests(unittest.TestCase):
    directory = Path(__file__).resolve().parents[1] / ".github/workflows"

    def workflow(self, component):
        return (self.directory / f"{component}-ci.yml").read_text()

    def test_root_routes_and_summarizes_exactly_the_same_eight_jobs(self):
        workflow = (self.directory / "ci.yml").read_text().split("\njobs:\n", 1)[1]
        matches = list(re.finditer(r"^  ([a-z][a-z-]*):\n", workflow, re.M))
        blocks = {item[1]: workflow[item.end():matches[i + 1].start() if i + 1 < len(matches) else len(workflow)]
                  for i, item in enumerate(matches)}
        self.assertEqual(set(blocks), {"route", "summary", *impact.JOBS})
        outputs = blocks["route"].split("    outputs:\n", 1)[1].split("    steps:", 1)[0]
        self.assertEqual(re.findall(r"^      ([a-z]+):", outputs, re.M), list(impact.JOBS))
        for job in impact.JOBS:
            self.assertIn(f"needs.route.outputs.{job} == 'true'", blocks[job])
            if job != "core":
                filename = "deletion-integration" if job == "deletion" else f"{job}-ci"
                self.assertIn(f"uses: ./.github/workflows/{filename}.yml", blocks[job])
        summary = blocks["summary"]
        self.assertIn("if: always()", summary)
        names = re.search(r"needs: \[([^\]]+)\]", summary)[1]
        self.assertEqual([name.strip() for name in names.split(",")], ["route", *impact.JOBS])
        self.assertIn("CI_NEEDS: ${{ toJSON(needs) }}", summary)
        self.assertIn("python3 scripts/ci-impact.py summary", summary)
        for command in ("go test ./...", "go test -race ./...", "go vet ./...", "make complexity-check",
                        "govulncheck@v1.8.0", "run-proxy-native-tests.py --sdk-mode source", "Docker build"):
            self.assertIn(command, blocks["core"])

    def test_new_workflows_are_callable_and_keep_read_only_checkout_permissions(self):
        for component in ("cli", "agent", "connectors", "skills"):
            with self.subTest(component=component):
                workflow = self.workflow(component)
                self.assertRegex(workflow, r"(?m)^  workflow_call:$")
                self.assertIn("permissions:\n  contents: read", workflow)
                self.assertEqual(workflow.count("uses: actions/checkout@"), workflow.count("persist-credentials: false"))
                for forbidden in ("contents: write", "id-token:", "attestations:", "secrets: inherit",
                                  "pull_request_target", "--push", "npm publish", "action-gh-release", "docker login", "GH_TOKEN"):
                    self.assertNotIn(forbidden, workflow)

    def test_source_go_jobs_preserve_full_gates_and_complete_root_build_context(self):
        for component in ("cli", "agent"):
            workflow = self.workflow(component)
            self.assertIn("go-version: '1.27.4'", workflow)
            self.assertIn("GOWORK: ${{ github.workspace }}/go.work", workflow)
            self.assertNotIn("GOWORK=off", workflow)
            for path in ("go.sum", "contracts/go.sum", "cli/go.sum", "agent/go.sum"):
                self.assertIn(f"            {path}\n", workflow)
            for gate in ("source-workspace.py check-go", "source-workspace.py tidy", "go mod verify", "go test ./...",
                         "go test -race ./...", "go vet ./...", "govulncheck@v1.8.0", "golangci-lint-action@v9", "CGO_ENABLED=0 go build"):
                self.assertIn(gate, workflow)
            builds = [line.strip() for line in workflow.splitlines() if line.strip().startswith("docker build") or line.strip().startswith("run: docker build")]
            self.assertEqual(len(builds), 1 if component == "cli" else 6)
            for build in builds:
                self.assertTrue(build.endswith(" ."), build)
                self.assertIn(f"-f {component}/", build)
                self.assertIn('--build-arg COMMIT="$SOURCE_COMMIT"', build)
                self.assertIn('--build-arg DATE="$SOURCE_DATE"', build)
                self.assertNotIn("--build-arg VERSION", build)
        self.assertIn("npm test --workspace cli/npm/cli", self.workflow("cli"))
        self.assertIn("npm pack --dry-run --workspace cli/npm/cli", self.workflow("cli"))
        self.assertIn("python3 cli/scripts/test_source_layout.py", self.workflow("cli"))

    def test_agent_keeps_node24_lock_schema_hermes_outpost_and_startup_gates(self):
        workflow = self.workflow("agent")
        for gate in ("resolve_contracts.py --mode source", "test_resolve_contracts.py", "contracts/agent/dsh-runtime.schema.json",
                     "deploy/hermes-tools/test_plugin.py", "kubectl kustomize", "kubeconform@v0.8.0",
                     "node-version: '24'", "cache-dependency-path: agent/runtimes/dsh/package-lock.json",
                     "npm --prefix agent/runtimes/dsh --workspaces=false ci", "npm --prefix agent/runtimes/dsh --workspaces=false test",
                     "--target volume-reader-runtime", "--target buildpacks-runtime", "--target buildpacks-daemonless-runtime",
                     "Dockerfile.hermes-agent-runner", "Dockerfile.dsh-runtime", "running healthy", "--network none --read-only"):
            self.assertIn(gate, workflow)

    def test_connectors_and_skills_keep_real_package_and_source_validation_gates(self):
        workflow = self.workflow("connectors")
        for gate in ("node-version: '22'", "cache-dependency-path: package-lock.json", "npm ci", "npm run check:workspace",
                     "npm run check --workspace connectors", "npm run package:check --workspace connectors",
                     "npm pack --json --workspace connectors", "npm run release:artifacts --workspace connectors",
                     "npm run release:verify --workspace connectors", "source-provenance.checks.mjs"):
            self.assertIn(gate, workflow)
        workflow = self.workflow("skills")
        for gate in ("working-directory: skills", "test -f agent-profiles/README.md", "test -f mcp-presets/README.md",
                     "python3 -m unittest discover -s tools", "--package-dry-run", "--package-output-dir dist", "--verify-package"):
            self.assertIn(gate, workflow)


if __name__ == "__main__":
    unittest.main()

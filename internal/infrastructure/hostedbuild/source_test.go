package hostedbuild

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSourceArchiveAndPackageValidation(t *testing.T) {
	t.Parallel()
	const check = `
import io
import json
import pathlib
import sys
import tarfile
import tempfile

source = {"__name__": "test"}
exec(sys.stdin.read(), source)

def make_archive(files, link=False):
    data = io.BytesIO()
    with tarfile.open(fileobj=data, mode="w:gz") as archive:
        for name, content in files.items():
            member = tarfile.TarInfo(name)
            member.size = len(content)
            archive.addfile(member, io.BytesIO(content))
        if link:
            member = tarfile.TarInfo("escape")
            member.type = tarfile.SYMTYPE
            member.linkname = "../outside"
            archive.addfile(member)
    data.seek(0)
    return data

def rejects(operation):
    try:
        operation()
    except ValueError:
        return
    raise AssertionError("invalid input was accepted")

with tempfile.TemporaryDirectory() as directory:
    root = pathlib.Path(directory)
    files = {
        "server.js": b"console.log('ok')",
        "package.json": b'{"dependencies":{"test":"^1.2.3"}}',
        "package-lock.json": json.dumps({"lockfileVersion": 3, "packages": {
            "": {"dependencies": {"test": "^1.2.3"}},
            "node_modules/test": {
                "resolved": "https://registry.npmjs.org/test/-/test-1.2.3.tgz",
                "integrity": "sha512-" + "a" * 86 + "==",
            },
        }}).encode(),
        "Dockerfile": b"FROM attacker",
        ".dockerignore": b"*",
        ".npmrc": b"ignore-scripts=false",
        "soha-host.json": b"{}",
        "app.py": b"print('ok')",
        "requirements.txt": b"example==1.2.3 # lock\n",
    }
    source["unpack"](make_archive(files), root)
    for item in ("Dockerfile", ".dockerignore", ".npmrc", "soha-host.json"):
        assert not (root / item).exists()
    source["validate_node"](root)
    source["validate_python"](root)
    assert (root / "requirements.txt").read_text() == "example==1.2.3\n"
    (root / "requirements.txt").write_text("--no-binary :all:\n")
    rejects(lambda: source["validate_python"](root))
    (root / "package.json").write_text('{"dependencies":{"test":"git+https://elsewhere.test/package"}}')
    rejects(lambda: source["validate_node"](root))
    rejects(lambda: source["unpack"](make_archive({"../outside": b"data"}), root))
    rejects(lambda: source["unpack"](make_archive({}, link=True), root))
`
	cmd := exec.CommandContext(t.Context(), "python3", "-I", "-c", check)
	cmd.Stdin = strings.NewReader(sourceScript)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hosted source checks failed: %v\n%s", err, output)
	}
}

from pathlib import Path
import shutil
import subprocess
import sys
from tempfile import TemporaryDirectory
import unittest

import check_dev_guidance as guidance


class GuidanceTests(unittest.TestCase):
    def write(self, root, name, text):
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
        return path

    def test_current_standalone_checkout(self):
        guidance.check_guidance(Path(__file__).resolve().parents[1])

    def test_local_inline_reference_image_and_encoded_paths(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            self.write(root, "target (example).md", "# Target\n")
            for link in ("[target](target%20(example).md#target)",
                         '[target](<target (example).md> "title")',
                         "![image](target%20%28example%29.md)",
                         "[target][ref]\n[ref]: target%20%28example%29.md",
                         "[ref][]\n[ref]: <target (example).md>",
                         "    - [nested item](target%20%28example%29.md)"):
                with self.subTest(link=link):
                    self.write(root, "AGENTS.md", link)
                    guidance.check_guidance(root)

    def test_broken_links_report_source_and_fail_even_when_another_link_is_valid(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            for link in ("[bad](missing.md)", "[bad][ref]\n[ref]: missing.md",
                         "[bad][undefined]", "[bad](../outside.md)"):
                with self.subTest(link=link):
                    self.write(root, "AGENTS.md", "[ok](https://example.invalid)\n" + link)
                    with self.assertRaisesRegex(ValueError, r"AGENTS.md.*(missing|undefined)"):
                        guidance.check_guidance(root)

    def test_deleted_or_renamed_target_is_rejected_by_surviving_incoming_link(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            self.write(root, "AGENTS.md", "[rules](web/AGENTS.md)")
            target = self.write(root, "web/AGENTS.md", "Web rules")
            guidance.check_guidance(root)
            target.rename(target.with_name("moved.md"))
            with self.assertRaisesRegex(ValueError, "web/AGENTS.md"):
                guidance.check_guidance(root)
            self.write(root, "AGENTS.md", "No longer links to the removed guide")
            guidance.check_guidance(root)

    def test_remote_fragment_and_code_examples_do_not_require_files(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            self.write(root, "AGENTS.md", """[web](https://example.invalid/missing)
[email](mailto:docs@example.invalid) [heading](#heading)
`[example](missing.md)`
```md
[example](missing.md)
```
~~~md
[example](missing.md)
~~~
""")
            guidance.check_guidance(root)

    def test_symlink_guidance_and_escaping_link_are_rejected(self):
        with TemporaryDirectory() as directory, TemporaryDirectory() as outside:
            root = Path(directory)
            target = self.write(Path(outside), "rules.md", "Outside repository")
            (root / "AGENTS.md").symlink_to(target)
            with self.assertRaisesRegex(ValueError, "symlink"):
                guidance.check_guidance(root)
            (root / "AGENTS.md").unlink()
            (root / "link.md").symlink_to(target)
            self.write(root, "AGENTS.md", "[escape](link.md)")
            with self.assertRaisesRegex(ValueError, "outside-repository"):
                guidance.check_guidance(root)

    def test_skill_metadata_valid_strings_and_folded_description(self):
        path = Path(".agents/skills/soha-cli/SKILL.md")
        for description in ('description: Valid description', 'description: "Quoted: description"',
                            "description: 'It''s valid'", "description: >\n  First line\n  second line",
                            "description: Valid\nlicense: MIT\nmetadata:\n  owner: CLI"):
            with self.subTest(description=description):
                guidance.check_metadata("---\nname: soha-cli\n" + description + "\n---\n", path)

    def test_missing_malformed_and_wrong_type_metadata_fail(self):
        path = Path(".agents/skills/soha-cli/SKILL.md")
        for text in ("No front matter", "---\nname: soha-cli\n", "---\nname: soha-cli\n---",
                     "---\nname: other-name\ndescription: Valid\n---",
                     *[f"---\nname: soha-cli\ndescription: {value}\n---"
                       for value in ("", "false", "42", "[]", "{}", '"unterminated', "bad: mapping", ">")]):
            with self.subTest(text=text), self.assertRaises(ValueError):
                guidance.check_metadata(text, path)

    def test_cli_returns_nonzero_on_damaged_metadata(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            script = root / "scripts/check_dev_guidance.py"
            script.parent.mkdir()
            shutil.copyfile(guidance.__file__, script)
            self.write(root, ".agents/skills/soha-cli/SKILL.md", "---\nname: soha-cli\n---\n")
            result = subprocess.run([sys.executable, str(script)], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("description", result.stderr)


if __name__ == "__main__":
    unittest.main()

import argparse
import copy
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import validate_assets as validator


class SourceLayoutTest(unittest.TestCase):
    def test_actual_prefixed_defaults(self):
        paths = validator.required_source_inputs()
        validator.validate_source_inputs(argparse.Namespace(**paths))
        self.assertEqual(paths["contracts_skill_schema"], validator.ROOT.parent / "contracts/skills/skill-manifest.schema.json")
        self.assertEqual(paths["gateway_catalog_source"], validator.ROOT.parent / "internal/application/aigateway/catalog.go")

    def test_prefixed_fixture_and_each_missing_source_fail_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve() / "source"
            skills = root / "skills"
            skills.mkdir(parents=True)
            paths = {name: root / path.relative_to(validator.ROOT.parent)
                     for name, path in validator.required_source_inputs().items()}
            for path in paths.values():
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("fixture")
            args = argparse.Namespace(**paths)
            with patch.object(validator, "required_source_inputs", return_value=paths):
                validator.validate_source_inputs(args)
                for name, path in paths.items():
                    with self.subTest(missing=name):
                        path.unlink()
                        with self.assertRaisesRegex(validator.ValidationError, "required same-source input"):
                            validator.validate_source_inputs(args)
                        path.write_text("fixture")
                changed = argparse.Namespace(**{**paths, "contracts_skill_schema": root / "foreign.json"})
                with self.assertRaisesRegex(validator.ValidationError, "must use the same-source"):
                    validator.validate_source_inputs(changed)
                target = paths["contracts_skill_schema"]
                target.unlink()
                outside = Path(directory).resolve() / "foreign.json"
                outside.write_text("fixture")
                target.symlink_to(outside)
                with self.assertRaisesRegex(validator.ValidationError, "must not redirect"):
                    validator.validate_source_inputs(args)

    def test_strict_schema_locations_reject_legacy_and_redirected_locations(self):
        catalog = validator.load_capability_catalog()
        platform = validator.load_platform_capability_catalog()
        index = validator.read_json(validator.ROOT / "skills/index.json")
        matrix = validator.validate_compatibility_matrix(catalog, platform, index)
        original_read = validator.read_json
        for field in ("preferred", "fallback"):
            changed = copy.deepcopy(matrix)
            changed["contractSchemas"][field] = "../soha-contracts"
            with self.subTest(field=field), patch.object(validator, "read_json", side_effect=lambda path: changed if path == validator.COMPATIBILITY_MATRIX_PATH else original_read(path)):
                with self.assertRaisesRegex(validator.ValidationError, "contractSchemas"):
                    validator.validate_compatibility_matrix(catalog, platform, index)
        for name in matrix["contractSchemas"]["assets"]:
            changed = copy.deepcopy(matrix)
            changed["contractSchemas"]["assets"][name]["preferred"] = "../foreign/schema.json"
            with self.subTest(asset=name), patch.object(validator, "read_json", side_effect=lambda path: changed if path == validator.COMPATIBILITY_MATRIX_PATH else original_read(path)):
                with self.assertRaisesRegex(validator.ValidationError, "must be"):
                    validator.validate_compatibility_matrix(catalog, platform, index)

    def test_missing_inputs_return_failed_report_not_skipped(self):
        args = argparse.Namespace(**validator.required_source_inputs(), write_index=False, release_version="0.1.3")
        args.contracts_skill_schema = None
        code, report, _ = validator.run_validation(args)
        self.assertEqual(code, 1)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(report["checks"][0]["name"], "same-source-inputs")
        self.assertEqual(report["checks"][0]["status"], "failed")


if __name__ == "__main__":
    unittest.main()

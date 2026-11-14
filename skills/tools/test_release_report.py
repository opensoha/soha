import datetime
import json
import os
import unittest
from pathlib import Path
from unittest.mock import patch

import validate_assets as validator


class ReleaseReportTest(unittest.TestCase):
    def test_fixed_epoch_makes_reports_identical(self):
        with patch.dict(os.environ, {"SOURCE_DATE_EPOCH": "1700000000"}):
            checks = [validator.check("release-report-fixture")]
            first = validator.build_validation_report("passed", "0.1.4", checks, {})
            second = validator.build_validation_report("passed", "0.1.4", checks, {})
        self.assertEqual(first["generatedAt"], "2023-11-14T22:13:20Z")
        self.assertEqual(json.dumps(first, sort_keys=True), json.dumps(second, sort_keys=True))

    def test_release_paths_do_not_depend_on_checkout_location(self):
        with patch.dict(os.environ, {"SOURCE_DATE_EPOCH": "0"}):
            for root in (Path("/first/source/skills"), Path("/second/source/skills")):
                with self.subTest(root=root), patch.object(validator, "ROOT", root):
                    self.assertEqual(validator.report_path(root.parent / "contracts/schema.json"), "../contracts/schema.json")
                    self.assertEqual(validator.report_path(Path("dist/release/report.json")), "dist/release/report.json")
                    self.assertEqual(validator.report_path(None), "")

    def test_malformed_epoch_fails_before_validation_or_writes(self):
        for epoch in ("", "-1", "1.5", " 1", "1\n", "no", "999999999999999999999999999"):
            with self.subTest(epoch=epoch), patch.dict(os.environ, {"SOURCE_DATE_EPOCH": epoch}):
                with self.assertRaisesRegex(validator.ValidationError, "SOURCE_DATE_EPOCH"):
                    validator.run_validation(None)

    def test_absent_epoch_keeps_local_time_and_paths(self):
        with patch.dict(os.environ):
            os.environ.pop("SOURCE_DATE_EPOCH", None)
            before = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)
            actual = datetime.datetime.fromisoformat(validator.utc_now().replace("Z", "+00:00"))
            after = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)
            self.assertLessEqual(before, actual)
            self.assertLessEqual(actual, after)
            path = validator.ROOT.parent / "contracts/schema.json"
            self.assertEqual(validator.report_path(path), path.as_posix())


if __name__ == "__main__":
    unittest.main()

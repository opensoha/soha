import tarfile
import unittest
from pathlib import Path
from unittest.mock import Mock

import validate_assets as validator


class ReleasePackageTest(unittest.TestCase):
    def test_member_paths_must_be_unique(self):
        archive = Mock(spec=tarfile.TarFile)
        artifact = Path("fixture.tar.gz")
        index = tarfile.TarInfo("soha-skills/skills/index.json")
        readme = tarfile.TarInfo("soha-skills/README.md")
        archive.getmembers.return_value = [index, readme]
        self.assertEqual(validator.release_member_names(archive, artifact), {index.name, readme.name})

        archive.getmembers.return_value = [index, readme, tarfile.TarInfo(index.name)]
        with self.assertRaisesRegex(validator.ValidationError, "duplicate release member"):
            validator.release_member_names(archive, artifact)
        archive.extractfile.assert_not_called()

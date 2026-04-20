import importlib.util
import json
import sys
import tempfile
import unittest
from argparse import Namespace
from pathlib import Path
from unittest.mock import patch


SCRIPT_PATH = Path(__file__).with_name("update-svelte-skills.py")


def load_module():
    spec = importlib.util.spec_from_file_location("update_svelte_skills", SCRIPT_PATH)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


class UpdateSvelteSkillsTest(unittest.TestCase):
    def test_default_ref_is_pinned_commit_sha(self):
        self.assertRegex(self.module.DEFAULT_REF, r"^[0-9a-f]{40}$")

    def setUp(self):
        self.module = load_module()
        self.temp_dir = tempfile.TemporaryDirectory()
        self.root = Path(self.temp_dir.name)
        (self.root / "skills").mkdir()
        (self.root / ".agents/skills").mkdir(parents=True)
        (self.root / ".claude/skills").mkdir(parents=True)

    def tearDown(self):
        self.temp_dir.cleanup()

    def write_skill(self, path: Path, name: str) -> None:
        path.mkdir(parents=True, exist_ok=True)
        (path / "SKILL.md").write_text(f"# {name}\n")

    def write_manifest(self, names: list[str]) -> None:
        (self.root / "skills/.svelte-managed.json").write_text(
            json.dumps({"skills": names}, indent=2) + "\n"
        )

    def test_download_failure_leaves_existing_skills_intact(self):
        self.write_skill(self.root / "skills/managed-old", "managed-old")
        self.write_skill(self.root / "skills/unrelated-local", "unrelated-local")
        self.write_manifest(["managed-old"])
        (self.root / ".agents/skills/managed-old").symlink_to(
            Path("../../skills/managed-old"), target_is_directory=True,
        )

        args = Namespace(ref="test-ref", target="all", dry_run=False)
        upstream = [
            self.module.RemoteEntry(
                entry_type="dir",
                path="tools/skills/managed-new",
                name="managed-new",
            ),
        ]

        with patch.object(self.module, "parse_args", return_value=args), patch.object(
            self.module, "repo_root", return_value=self.root,
        ), patch.object(
            self.module, "list_remote_directory", return_value=upstream,
        ), patch.object(
            self.module, "download_directory", side_effect=RuntimeError("boom"),
        ):
            with self.assertRaisesRegex(RuntimeError, "boom"):
                self.module.main()

        self.assertTrue((self.root / "skills/managed-old").is_dir())
        self.assertTrue((self.root / "skills/unrelated-local").is_dir())
        self.assertTrue((self.root / ".agents/skills/managed-old").is_symlink())

    def test_prunes_only_manifest_managed_skills(self):
        self.write_skill(self.root / "skills/managed-old", "managed-old")
        self.write_skill(self.root / "skills/unrelated-local", "unrelated-local")
        self.write_manifest(["managed-old"])
        (self.root / ".agents/skills/managed-old").symlink_to(
            Path("../../skills/managed-old"), target_is_directory=True,
        )

        args = Namespace(ref="test-ref", target="all", dry_run=False)
        upstream = [
            self.module.RemoteEntry(
                entry_type="dir",
                path="tools/skills/managed-new",
                name="managed-new",
            ),
        ]

        def fake_download_directory(_api_path: str, destination: Path, _ref: str) -> None:
            self.write_skill(destination, "managed-new")

        with patch.object(self.module, "parse_args", return_value=args), patch.object(
            self.module, "repo_root", return_value=self.root,
        ), patch.object(
            self.module, "list_remote_directory", return_value=upstream,
        ), patch.object(
            self.module, "download_directory", side_effect=fake_download_directory,
        ):
            result = self.module.main()

        self.assertEqual(result, 0)
        self.assertFalse((self.root / "skills/managed-old").exists())
        self.assertTrue((self.root / "skills/unrelated-local").is_dir())
        self.assertTrue((self.root / "skills/managed-new").is_dir())
        self.assertFalse((self.root / ".agents/skills/managed-old").exists())
        self.assertTrue((self.root / ".agents/skills/managed-new").is_symlink())

        manifest = json.loads((self.root / "skills/.svelte-managed.json").read_text())
        self.assertEqual(manifest, {"skills": ["managed-new"]})


if __name__ == "__main__":
    unittest.main()

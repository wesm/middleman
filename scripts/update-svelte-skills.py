#!/usr/bin/env python3

from __future__ import annotations

import argparse
import json
import shutil
import sys
import tempfile
import urllib.request
import zipfile
from pathlib import Path


PINNED_SKILLS = {
    "svelte-code-writer": {
        "tag": "svelte-code-writer-v2026.03.12-173240",
        "asset_url": "https://github.com/sveltejs/ai-tools/releases/download/"
        "svelte-code-writer-v2026.03.12-173240/svelte-code-writer.zip",
    },
    "svelte-core-bestpractices": {
        "tag": "svelte-core-bestpractices-v2026.03.12-173239",
        "asset_url": "https://github.com/sveltejs/ai-tools/releases/download/"
        "svelte-core-bestpractices-v2026.03.12-173239/svelte-core-bestpractices.zip",
    },
}

TARGETS = {
    "codex": Path(".agents/skills"),
    "claude": Path(".claude/skills"),
}

RELEASES_API = "https://api.github.com/repos/sveltejs/ai-tools/releases?per_page=100"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Install repo-local Svelte AI skills for Codex and Claude."
    )
    parser.add_argument(
        "--target",
        choices=("all", "codex", "claude"),
        default="all",
        help="Which repo-local skill targets to update.",
    )
    parser.add_argument(
        "--latest",
        action="store_true",
        help="Resolve the latest upstream release for each skill instead of pinned tags.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print planned actions without writing files.",
    )
    return parser.parse_args()


def repo_root() -> Path:
    return Path(__file__).resolve().parent.parent


def selected_targets(target_arg: str) -> dict[str, Path]:
    if target_arg == "all":
        return TARGETS.copy()
    return {target_arg: TARGETS[target_arg]}


def fetch_json(url: str) -> object:
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/vnd.github+json",
            "User-Agent": "middleman-svelte-skills",
        },
    )
    with urllib.request.urlopen(request) as response:
        return json.load(response)


def resolve_latest_skills() -> dict[str, dict[str, str]]:
    releases = fetch_json(RELEASES_API)
    if not isinstance(releases, list):
        raise RuntimeError("Unexpected releases API response from GitHub")

    resolved: dict[str, dict[str, str]] = {}
    for skill_name in PINNED_SKILLS:
        prefix = f"{skill_name}-v"
        for release in releases:
            tag = release.get("tag_name")
            if not isinstance(tag, str) or not tag.startswith(prefix):
                continue

            assets = release.get("assets", [])
            asset_name = f"{skill_name}.zip"
            for asset in assets:
                if asset.get("name") == asset_name:
                    resolved[skill_name] = {
                        "tag": tag,
                        "asset_url": asset["browser_download_url"],
                    }
                    break

            if skill_name in resolved:
                break

        if skill_name not in resolved:
            raise RuntimeError(f"Could not resolve latest release for {skill_name}")

    return resolved


def download_and_extract(skill_name: str, asset_url: str, workspace: Path) -> Path:
    archive_path = workspace / f"{skill_name}.zip"
    request = urllib.request.Request(asset_url, headers={"User-Agent": "middleman-svelte-skills"})
    with urllib.request.urlopen(request) as response, archive_path.open("wb") as output:
        shutil.copyfileobj(response, output)

    extract_root = workspace / f"{skill_name}-extract"
    with zipfile.ZipFile(archive_path) as archive:
        archive.extractall(extract_root)

    children = [path for path in extract_root.iterdir() if path.is_dir()]
    if len(children) != 1:
        raise RuntimeError(
            f"{skill_name}: expected one top-level directory in archive, found {len(children)}"
        )

    skill_dir = children[0]
    if skill_dir.name != skill_name:
        raise RuntimeError(
            f"{skill_name}: archive top-level directory was {skill_dir.name}, expected {skill_name}"
        )

    if not (skill_dir / "SKILL.md").is_file():
        raise RuntimeError(f"{skill_name}: archive is missing SKILL.md")

    return skill_dir


def install_skill(skill_src: Path, target_dir: Path, dry_run: bool) -> None:
    action = "replace" if target_dir.exists() else "create"
    print(f"{action}: {target_dir}")

    if dry_run:
        return

    target_dir.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(
        dir=str(target_dir.parent), prefix=f".{target_dir.name}.tmp-"
    ) as temp_parent:
        staged_dir = Path(temp_parent) / target_dir.name
        shutil.copytree(skill_src, staged_dir)

        backup_dir = target_dir.parent / f".{target_dir.name}.bak"
        if backup_dir.exists():
            shutil.rmtree(backup_dir)

        try:
            if target_dir.exists():
                target_dir.rename(backup_dir)

            staged_dir.rename(target_dir)

            if backup_dir.exists():
                shutil.rmtree(backup_dir)
        except Exception:
            if backup_dir.exists() and not target_dir.exists():
                backup_dir.rename(target_dir)
            raise


def main() -> int:
    args = parse_args()
    root = repo_root()
    targets = selected_targets(args.target)
    skill_sources = resolve_latest_skills() if args.latest else PINNED_SKILLS

    print(f"repo: {root}")
    print("targets:")
    for name, rel_path in targets.items():
        print(f"  - {name}: {root / rel_path}")

    print("skills:")
    for skill_name, meta in skill_sources.items():
        print(f"  - {skill_name}: {meta['tag']}")

    if args.dry_run:
        print("mode: dry-run")

    with tempfile.TemporaryDirectory(prefix="svelte-skills-") as temp_dir:
        workspace = Path(temp_dir)
        extracted: dict[str, Path] = {}

        for skill_name, meta in skill_sources.items():
            print(f"fetch: {skill_name}")
            extracted[skill_name] = download_and_extract(
                skill_name, meta["asset_url"], workspace
            )

        for target_name, target_rel_path in targets.items():
            print(f"install target: {target_name}")
            target_root = root / target_rel_path
            for skill_name, skill_src in extracted.items():
                install_skill(skill_src, target_root / skill_name, args.dry_run)

    print("done")
    print("If Codex or Claude does not pick up the changes immediately, restart it.")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1)

"""Distinguish optional local tooling from the source that a contributor publishes."""

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from check_commit_message import validate
from validate_skills import expected_adapter


ROOT = Path(__file__).resolve().parents[1]
HEADER = "---\nstatus: adopted\n---\n"


class RepositoryPolicyContracts(unittest.TestCase):
    def setUp(self):
        scratch = ROOT / "tmp"
        scratch.mkdir(exist_ok=True)
        directory = tempfile.TemporaryDirectory(prefix="repository policy ", dir=scratch)
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.env = {key: value for key, value in os.environ.items()
                    if key not in ("GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_PREFIX", "GIT_COMMON_DIR")}
        self.env["PYTHONIOENCODING"] = "utf-8"
        self.env["AGENTMEMORY_INJECT_CONTEXT"] = "false"
        for relative in (
            "tools/check_repository.py", "tools/validate_skills.py", ".gitignore",
            ".node-version", ".betterleaks.toml", "relkit.toml",
            "web/package.json", "web/package-lock.json",
        ):
            target = self.root / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, target)
        self.write("AGENTS.md", "# Repository contract\n")
        self.write("README.md", "# Example project\n")
        self.skill("example", "Check repository contracts.")
        result = self.run_command("git", "init", "--quiet")
        self.assertEqual(result.returncode, 0, result.stderr)

    def write(self, relative, content):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def skill(self, name, description):
        self.write(f".agents/skills/{name}/SKILL.md", f"---\nname: {name}\ndescription: {description}\n---\n# Skill\n")
        self.write(f".claude/skills/{name}/SKILL.md", expected_adapter(name, description))

    def run_command(self, *command):
        return subprocess.run(command, cwd=self.root, env=self.env, capture_output=True, encoding="utf-8", timeout=30)

    def check(self, script, expected=0):
        result = self.run_command(sys.executable, f"tools/{script}.py")
        self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        return result

    def test_ignored_tool_state_and_personal_skills_do_not_change_repository_contracts(self):
        self.write(".git/info/exclude", "/.impeccable/\n/.playwright-mcp/\n/.agents/skills/personal/\n/.claude/skills/personal/\n")
        for relative in (
            ".impeccable/state.txt", ".playwright-mcp/report.md",
            ".agents/skills/personal/SKILL.md", ".claude/skills/personal/SKILL.md",
            "docs/plans/private/notes.md", ".codex/config.toml",
            ".claude/settings.json", ".claude/settings.local.json", ".codex/workflow.toml",
        ):
            self.write(relative, "local-only fixture, deliberately not a public document\n")
        self.check("check_repository")
        self.check("validate_skills")

    def test_ignoring_an_external_tool_does_not_make_it_a_dependency(self):
        with (self.root / ".gitignore").open("a", encoding="utf-8") as target:
            target.write("\n/.impeccable/\n/.agents/skills/frontend-ui-engineering/\n")
        self.check("validate_skills")

    def test_skill_description_can_discuss_todo_comments(self):
        self.skill("example", "Locate TODO comments that need a maintainer decision.")
        self.check("validate_skills")

    def test_public_adapter_cannot_depend_on_an_ignored_canonical_skill(self):
        self.write(".git/info/exclude", "/.agents/skills/personal/\n")
        self.skill("personal", "Local preferences.")
        result = self.check("validate_skills", 1)
        self.assertIn("adapter has no canonical skill", result.stderr)

    def test_public_adapters_still_require_the_canonical_contract(self):
        self.write(".claude/skills/example/SKILL.md", "# Independent instructions\n")
        result = self.check("validate_skills", 1)
        self.assertIn("discovery adapter contract has drifted", result.stderr)

    def test_nested_example_skill_is_not_a_root_project_skill(self):
        self.write("examples/template/.agents/skills/nested/SKILL.md", "Example, not an installed project skill\n")
        self.check("validate_skills")

    def test_public_documents_are_not_rejected_by_directory_name(self):
        paths = ["docs/plans/migration-guide.md", "docs/history/история-форматов.md", "docs/audits/security-review.md"]
        self.write("README.md", "\n".join(f"[Reference]({path})" for path in paths))
        for path in paths:
            self.write(path, HEADER + "# Contributor reference\n")
        self.check("check_repository")

    def test_withdrawn_internal_documents_still_cannot_return(self):
        path = "docs/history/ui-audit-2026-08-21.md"
        self.write("README.md", f"[Report]({path})\n")
        self.write(path, HEADER + "# Internal work report\n")
        result = self.check("check_repository", 1)
        self.assertIn("retired working-document path", result.stderr)

    def test_status_frontmatter_only_applies_to_publishable_documentation(self):
        self.write("docs/plans/private/notes.md", "Private scratch without frontmatter\n")
        self.check("check_repository")
        self.write("README.md", "[Guide](docs/guide.md)\n")
        self.write("docs/guide.md", "# Public guide without frontmatter\n")
        result = self.check("check_repository", 1)
        self.assertIn("missing valid status frontmatter", result.stderr)
        self.write("docs/guide.md", HEADER + "# Public guide\n")
        self.check("check_repository")

    def path_findings(self, relative):
        script = (
            "import json, pathlib, sys, tomllib; sys.path.insert(0, sys.argv[1]); "
            "from releasekit.exposure.rules import kinds_in_path; "
            "policy=tomllib.loads(pathlib.Path('relkit.toml').read_text(encoding='utf-8'))['exposure']; "
            "print(json.dumps(sorted(kinds_in_path(sys.argv[2], "
            "private_paths=policy['private_paths'], private_files=policy['private_files'], "
            "private_suffixes=policy['private_suffixes']))))"
        )
        result = self.run_command(sys.executable, "-c", script, str(ROOT / ".github/relkit.pyz"), relative)
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def test_shared_config_names_are_not_private_by_definition(self):
        for path in (".codex/config.toml", ".claude/settings.json"):
            with self.subTest(path=path):
                self.assertEqual(self.path_findings(path), [])

    def test_personal_settings_reports_and_tool_output_remain_private(self):
        for path in (
            ".claude/settings.local.json", ".codex/workflow.toml", "AGENTS.local.md",
            ".private/report.md", ".agents/local/state.json", "docs/plans/private/notes.md",
            ".impeccable/state.json", ".playwright-mcp/report.md",
        ):
            with self.subTest(path=path):
                self.assertIn("private-path", self.path_findings(path))

    def test_shared_config_is_still_checked_for_private_content(self):
        script = (
            "import json, pathlib, sys, tomllib; sys.path.insert(0, sys.argv[1]); "
            "from releasekit.exposure.rules import kinds_in_text; "
            "policy=tomllib.loads(pathlib.Path('relkit.toml').read_text(encoding='utf-8'))['exposure']; "
            "print(json.dumps(sorted(kinds_in_text(sys.argv[2], relative_path='.codex/config.toml', "
            "forbid_internal_planning=policy['forbid_internal_planning'], "
            "forbid_machine_observations=policy['forbid_machine_observations']))))"
        )
        for source, expected in (
            ('path = "/' + 'home' + '/personal-machine/private"', "home-directory"),
            ("card" + ": 123", "internal-planning"),
        ):
            with self.subTest(kind=expected):
                result = self.run_command(sys.executable, "-c", script, str(ROOT / ".github/relkit.pyz"), source)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(expected, json.loads(result.stdout))


class CommitSubjectContracts(unittest.TestCase):
    def test_valid_structure_does_not_impose_description_language_or_style(self):
        for subject in (
            "fix: IPv6 routing", "docs: исправить описание установки", "test: x",
            "fix(api)!: Keep migration instructions.", "docs: " + "context " * 10 + "details",
        ):
            with self.subTest(subject=subject):
                self.assertEqual(validate(subject), [])

    def test_closed_types_lowercase_scopes_and_nonempty_descriptions_remain_required(self):
        for subject in (
            "", "fix: ", "fix:   ", "fix: x\ny", "fix(API): change", "fix change",
            "feature: change", "fix!!: change", "fix: change ",
        ):
            with self.subTest(subject=subject):
                self.assertTrue(validate(subject))


if __name__ == "__main__":
    unittest.main()

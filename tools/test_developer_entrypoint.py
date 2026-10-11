"""Exercise the real developer helpers against isolated Git and tool paths."""

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PWSH = shutil.which("pwsh")
GO = shutil.which("go")
if GO is None and os.name == "nt":
    fallback = Path(r"C:\Program Files\Go\bin\go.exe")
    if fallback.is_file():
        GO = str(fallback)
DRIVER = r"""
param([string]$Source, [string]$Checkout, [string]$Operation, [string]$GoPath, [string]$Target)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$RepositoryRoot = $Checkout
$GoExecutable = $GoPath
$ParseTokens = $null
$ParseErrors = $null
$Syntax = [Management.Automation.Language.Parser]::ParseFile($Source, [ref]$ParseTokens, [ref]$ParseErrors)
if ($ParseErrors.Count -ne 0) { throw ($ParseErrors | Out-String) }
foreach ($Definition in $Syntax.FindAll({ param($Node) $Node -is [Management.Automation.Language.FunctionDefinitionAst] }, $false)) {
    . ([scriptblock]::Create($Definition.Extent.Text))
}
switch ($Operation) {
    'install' { Install-Hooks }
    'assert' { Assert-RoutevaneHooksCurrent }
    'formatter' {
        $Formatter = Get-RoutevaneGoFormatter
        & $Formatter $Target
        if ($LASTEXITCODE -ne 0) { throw 'selected formatter failed' }
    }
    'format-check' {
        function Invoke-Checked { }
        function Invoke-GoSecurity { }
        Invoke-GoCheck
    }
    default { throw 'unknown test operation' }
}
"""


@unittest.skipUnless(PWSH, "PowerShell 7 is required for developer entrypoint tests")
class DeveloperEntrypointTests(unittest.TestCase):
    def setUp(self):
        scratch = ROOT / "tmp"
        scratch.mkdir(exist_ok=True)
        directory = tempfile.TemporaryDirectory(prefix="developer contracts ", dir=scratch)
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.driver = self.root / "driver.ps1"
        self.driver.write_text(DRIVER, encoding="utf-8")
        self.env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
        self.env.update({"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
                         "POWERSHELL_TELEMETRY_OPTOUT": "1"})
        self.git("init", "--quiet")
        tracked = self.root / ".githooks"
        tracked.mkdir()
        for name in ("pre-commit", "commit-msg"):
            path = tracked / name
            path.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
            path.chmod(0o644)

    def git(self, *arguments):
        return subprocess.run(["git", *arguments], cwd=self.root, env=self.env,
                              check=True, capture_output=True, text=True, timeout=15)

    def helper(self, operation, *, go="unused", target="unused"):
        return subprocess.run(
            [PWSH, "-NoLogo", "-NoProfile", "-File", str(self.driver),
             str(ROOT / "tools/dev.ps1"), str(self.root), operation, str(go), str(target)],
            cwd=self.root, env=self.env, capture_output=True, text=True, timeout=20,
        )

    def assert_ok(self, result):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_doctor_receives_only_the_first_go_application_on_path(self):
        tools = self.root / "tools"
        tools.mkdir()
        entrypoint = tools / "dev.ps1"
        shutil.copyfile(ROOT / "tools/dev.ps1", entrypoint)
        (tools / "doctor.py").write_text(
            "import json, subprocess, sys\n"
            "result = subprocess.run([sys.argv[2], 'version'], check=True, "
            "capture_output=True, text=True)\n"
            "print(json.dumps({'arguments': sys.argv[1:], "
            "'identity': result.stdout.strip()}))\n", encoding="utf-8")
        applications = []
        for identity in ("first-go", "second-go"):
            directory = self.root / identity
            directory.mkdir()
            executable = directory / ("go.cmd" if os.name == "nt" else "go")
            executable.write_text(
                f"@echo off\n@echo {identity}\n" if os.name == "nt"
                else f"#!/bin/sh\nprintf '%s\\n' '{identity}'\n", encoding="utf-8")
            executable.chmod(0o755)
            applications.append(executable)
        self.env["PATH"] = os.pathsep.join(
            [str(application.parent) for application in applications] + [self.env["PATH"]])
        result = subprocess.run(
            [PWSH, "-NoLogo", "-NoProfile", "-File", str(entrypoint), "doctor"],
            cwd=self.root, env=self.env, capture_output=True, text=True, timeout=20)
        self.assert_ok(result)
        observed = json.loads(result.stdout)
        self.assertEqual(observed["arguments"], ["--go", str(applications[0])])
        self.assertEqual(observed["identity"], "first-go")

    def entrypoint_with_path_probe(self):
        tools = self.root / "tools"
        tools.mkdir()
        entrypoint = tools / "dev.ps1"
        shutil.copyfile(ROOT / "tools/dev.ps1", entrypoint)
        (tools / "doctor.py").write_text(
            "import json, os\nprint(json.dumps(os.environ['PATH']))\n", encoding="utf-8")
        go = self.root / "go bin"
        go.mkdir()
        executable = go / ("go.cmd" if os.name == "nt" else "go")
        executable.write_text("@echo off\n" if os.name == "nt" else "#!/bin/sh\n", encoding="utf-8")
        executable.chmod(0o755)
        return entrypoint, go

    def run_entrypoint(self, entrypoint, path):
        self.env["PATH"] = path
        return subprocess.run(
            [PWSH, "-NoLogo", "-NoProfile", "-File", str(entrypoint), "doctor"],
            # A localized warning prefix arrives in the console code page.
            cwd=self.root, env=self.env, capture_output=True, text=True, errors="replace", timeout=20)

    def test_child_path_drops_duplicate_and_missing_directories_on_windows_only(self):
        entrypoint, go = self.entrypoint_with_path_probe()
        first, second = self.root / "first tools", self.root / "second tools"
        first.mkdir()
        second.mkdir()
        inherited = os.environ["PATH"]
        crafted = [str(go), str(first), str(self.root / "removed tools"), str(first).upper() + os.sep,
                   str(self.driver), f'"{second}"', "relative tools", "relative tools",
                   str(go).replace("\\", "/")]
        result = self.run_entrypoint(entrypoint, os.pathsep.join(crafted + [inherited]))
        self.assert_ok(result)
        observed = json.loads(result.stdout).split(os.pathsep)
        if os.name != "nt":
            # Elsewhere PATH is left alone; PowerShell itself may prepend its home.
            expected = crafted + inherited.split(os.pathsep)
            self.assertEqual(observed[-len(expected):], expected)
            return
        kept = [str(go), str(first), f'"{second}"', "relative tools"]
        self.assertEqual([entry for entry in observed if entry in kept], kept)
        for dropped in (str(self.root / "removed tools"), str(first).upper() + os.sep, str(self.driver),
                        str(go).replace("\\", "/")):
            self.assertNotIn(dropped, observed)
        keys = [os.path.normcase(os.path.normpath(entry.strip().strip('"'))) for entry in observed]
        self.assertEqual(len(keys), len(set(keys)), observed)
        for entry in observed:
            directory = entry.strip().strip('"')
            if os.path.isabs(directory):
                self.assertTrue(os.path.isdir(directory), entry)

    @unittest.skipUnless(os.name == "nt", "cmd.exe limits PATH only on Windows")
    def test_overlong_distinct_path_warns_and_still_runs(self):
        entrypoint, go = self.entrypoint_with_path_probe()
        directories = [str(go)]
        while sum(len(entry) + 1 for entry in directories) <= 6000:
            directory = self.root / f"distinct tools {len(directories):03d} {'x' * 100}"
            directory.mkdir()
            directories.append(str(directory))
        result = self.run_entrypoint(entrypoint, os.pathsep.join(directories + [os.environ["PATH"]]))
        self.assert_ok(result)
        self.assertIn("8191-character limit", result.stdout + result.stderr)
        self.assertTrue(any(line.startswith('"') for line in result.stdout.splitlines()),
                        "the probe must still run")

    @unittest.skipUnless(GO, "Go is required for the real formatter check")
    def test_formatting_check_handles_long_checkout_paths_and_nested_files(self):
        for name in ("cmd", "sdk", "examples", "tools", "internal"):
            (self.root / name).mkdir()
        nested = self.root / "internal" / "nested formatting fixture"
        nested.mkdir()
        files = []
        argument_length = 0
        while argument_length <= 32767:
            path = nested / f"formatted_{len(files):03d}.go"
            path.write_text("package fixture\n\nvar value = 1\n", encoding="utf-8", newline="\n")
            files.append(path)
            argument_length += len(str(path)) + 3
        self.assertGreater(sum(len(str(path)) + 3 for path in files), 32767)
        self.assert_ok(self.helper("format-check", go=GO))
        malformed = files[-1]
        original = "package fixture\nvar value=1\n"
        malformed.write_text(original, encoding="utf-8", newline="\n")
        rejected = self.helper("format-check", go=GO)
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("gofmt is required", rejected.stdout + rejected.stderr)
        self.assertIn(malformed.name, rejected.stdout + rejected.stderr)
        self.assertEqual(malformed.read_bytes(), original.encode("utf-8"))

    def test_active_hooks_path_is_checked_and_installation_populates_it(self):
        self.assert_ok(self.helper("install"))
        self.git("config", "core.hooksPath", "custom hooks")
        rejected = self.helper("assert")
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("custom hooks", rejected.stdout + rejected.stderr)
        self.assert_ok(self.helper("install"))
        self.assert_ok(self.helper("assert"))
        for name in ("pre-commit", "commit-msg"):
            self.assertEqual((self.root / "custom hooks" / name).read_bytes(),
                             (self.root / ".githooks" / name).read_bytes())
        (self.root / "custom hooks/pre-commit").write_text("#!/bin/sh\nexit 1\n", encoding="utf-8")
        stale = self.helper("assert")
        self.assertNotEqual(stale.returncode, 0)
        self.assertIn("stale", stale.stdout + stale.stderr)
        self.assert_ok(self.helper("install"))
        self.assert_ok(self.helper("assert"))

    def test_tracked_hooks_can_be_the_configured_hook_directory(self):
        self.git("config", "core.hooksPath", ".githooks")
        self.assert_ok(self.helper("install"))
        self.assert_ok(self.helper("assert"))
        self.assert_ok(self.helper("install"))

    def test_an_absolute_hook_directory_is_respected(self):
        hooks = self.root / "absolute hooks"
        self.git("config", "core.hooksPath", str(hooks))
        self.assert_ok(self.helper("install"))
        self.assert_ok(self.helper("assert"))
        self.assertEqual((hooks / "pre-commit").read_bytes(),
                         (self.root / ".githooks/pre-commit").read_bytes())

    @unittest.skipIf(os.name == "nt", "Unix executable mode contract")
    def test_matching_bytes_do_not_hide_an_inactive_nonexecutable_hook(self):
        self.assert_ok(self.helper("install"))
        installed = self.root / ".git/hooks/pre-commit"
        self.assertTrue(os.access(installed, os.X_OK))
        installed.chmod(0o644)
        rejected = self.helper("assert")
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("not executable", rejected.stdout + rejected.stderr)
        self.assert_ok(self.helper("install"))
        self.assertTrue(os.access(installed, os.X_OK))

    @unittest.skipIf(os.name == "nt", "Unix execution access contract")
    def test_hook_access_matches_this_user_instead_of_any_execute_bit(self):
        self.assert_ok(self.helper("install"))
        installed = self.root / ".git/hooks/pre-commit"
        # The kernel runs this harmless, independently owned fixture, not a hook.
        # It gives an oracle without repeating the helper's os.access call.
        oracle = self.root / "permission oracle"
        oracle.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
        modes = (0o644, 0o645, 0o744) if os.getuid() == 0 else (0o644, 0o744)
        for mode in modes:
            with self.subTest(mode=oct(mode), uid=os.getuid()):
                installed.chmod(mode)
                oracle.chmod(mode)
                try:
                    executed = subprocess.run([str(oracle)], cwd=self.root, env=self.env,
                                              capture_output=True, text=True, timeout=5)
                except PermissionError:
                    executable = False
                else:
                    self.assertEqual(executed.returncode, 0, executed.stdout + executed.stderr)
                    executable = True
                # A root process may lack permission-override capabilities; its
                # actual result, not UID alone, decides whether 0645 can execute.
                checked = self.helper("assert")
                self.assertEqual(checked.returncode == 0, executable,
                                 checked.stdout + checked.stderr)
                if not executable:
                    self.assertIn("not executable", checked.stdout + checked.stderr)

    @unittest.skipIf(os.name == "nt" or os.getuid() == 0,
                     "0645 denial requires a normal Unix owner; root can execute it")
    def test_normal_owner_cannot_use_another_users_hook_execute_bit(self):
        self.assert_ok(self.helper("install"))
        installed = self.root / ".git/hooks/pre-commit"
        installed.chmod(0o645)
        oracle = self.root / "permission oracle"
        oracle.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
        oracle.chmod(0o645)
        with self.assertRaises(PermissionError):
            subprocess.run([str(oracle)], cwd=self.root, env=self.env,
                           capture_output=True, text=True, timeout=5)
        checked = self.helper("assert")
        self.assertNotEqual(checked.returncode, 0)
        self.assertIn("not executable", checked.stdout + checked.stderr)

    @unittest.skipIf(os.name == "nt", "Unix Python failure fixture")
    def test_hook_access_check_fails_closed_when_python_fails(self):
        self.assert_ok(self.helper("install"))
        tools = self.root / "broken Python"
        tools.mkdir()
        python = tools / "python"
        python.write_text("#!/bin/sh\nexit 17\n", encoding="utf-8")
        python.chmod(0o755)
        self.env["PATH"] = str(tools) + os.pathsep + self.env["PATH"]
        checked = self.helper("assert")
        self.assertNotEqual(checked.returncode, 0)
        self.assertIn("Unable to check Routevane hook execution access", checked.stdout + checked.stderr)
        self.assertIn("Python exited with code 17", checked.stdout + checked.stderr)

    @unittest.skipIf(os.name == "nt", "Unix executable shim fixture")
    def test_formatter_comes_from_the_selected_go_even_when_path_has_another(self):
        goroot = self.root / "selected Go"
        (goroot / "bin").mkdir(parents=True)
        selected = self.root / "go shim"
        selected.write_text(f"#!{sys.executable}\nprint({str(goroot)!r})\n", encoding="utf-8")
        selected.chmod(0o755)
        formatter = goroot / "bin/gofmt"
        formatter.write_text(
            f"#!{sys.executable}\nfrom pathlib import Path\nimport sys\n"
            "Path(sys.argv[1]).write_text('selected formatter', encoding='utf-8')\n", encoding="utf-8")
        formatter.chmod(0o755)
        ambient = self.root / "ambient tools"
        ambient.mkdir()
        wrong = ambient / "gofmt"
        wrong.write_text("#!/bin/sh\nexit 77\n", encoding="utf-8")
        wrong.chmod(0o755)
        self.env["PATH"] = str(ambient) + os.pathsep + self.env["PATH"]
        target = self.root / "formatted.go"
        self.assert_ok(self.helper("formatter", go=selected, target=target))
        self.assertEqual(target.read_text(encoding="utf-8"), "selected formatter")
        formatter.unlink()
        rejected = self.helper("formatter", go=selected, target=target)
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("formatter is missing", rejected.stdout + rejected.stderr)


HOOK_DOCTOR = """\
import json, os, pathlib
log = pathlib.Path(__file__).resolve().parents[1] / "hook-log.jsonl"
with log.open("a", encoding="utf-8") as stream:
    print(json.dumps({"tool": "doctor", "index": os.environ.get("GIT_INDEX_FILE")}), file=stream)
"""
# The audit double reads the index through Git exactly as the real audit does,
# so whichever index the hook hands it is the one it judges.
HOOK_AUDIT = """\
import json, os, pathlib, subprocess, sys
root = pathlib.Path(__file__).resolve().parents[1]
names = subprocess.run(["git", "diff", "--cached", "--name-only", "--diff-filter=ACMR"], cwd=root,
                       check=True, capture_output=True, text=True).stdout.split()
with (root / "hook-log.jsonl").open("a", encoding="utf-8") as stream:
    print(json.dumps({"tool": "audit", "arguments": sys.argv[1:],
                      "index": os.environ.get("GIT_INDEX_FILE"), "staged": names}), file=stream)
for name in names:
    blob = subprocess.run(["git", "show", ":" + name], cwd=root, check=True, capture_output=True).stdout
    if b"PLANTED-SECRET" in blob:
        print("audit rejected " + name, file=sys.stderr)
        sys.exit(1)
"""
HOOK_ENTRYPOINT = """\
param([string]$Command)
$Log = Join-Path (Split-Path -Parent $PSScriptRoot) 'hook-log.jsonl'
$Entry = [ordered]@{ tool = 'dev'; command = $Command; index = $env:GIT_INDEX_FILE } | ConvertTo-Json -Compress
Add-Content -LiteralPath $Log -Value $Entry -Encoding utf8
"""


@unittest.skipUnless(PWSH, "PowerShell 7 is required for the pre-commit hook tests")
class PreCommitHookTests(unittest.TestCase):
    """Run the tracked pre-commit hook in a throwaway repository with recording tools."""

    def setUp(self):
        scratch = ROOT / "tmp"
        scratch.mkdir(exist_ok=True)
        directory = tempfile.TemporaryDirectory(prefix="pre-commit hook ", dir=scratch)
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
        self.env.update({"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
                         "POWERSHELL_TELEMETRY_OPTOUT": "1", "PYTHONIOENCODING": "utf-8"})
        self.git("init", "--quiet")
        self.git("config", "user.name", "Hook Test")
        self.git("config", "user.email", "hook@example.invalid")
        self.git("config", "core.autocrlf", "false")
        for relative, content in (("tools/doctor.py", HOOK_DOCTOR), (".github/relkit.pyz", HOOK_AUDIT),
                                  ("tools/dev.ps1", HOOK_ENTRYPOINT)):
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")
        self.write("internal/app.go", "package app\n")
        self.write("docs/notes.md", "notes\n")
        self.git("add", "--", "internal/app.go", "docs/notes.md")
        self.git("commit", "--quiet", "-m", "base")
        hooks = self.root / "hooks"
        hooks.mkdir()
        hook = hooks / "pre-commit"
        hook.write_bytes((ROOT / ".githooks/pre-commit").read_bytes())
        hook.chmod(0o755)
        self.git("config", "core.hooksPath", "hooks")

    def git(self, *arguments, check=True):
        return subprocess.run(["git", *arguments], cwd=self.root, env=self.env,
                              check=check, capture_output=True, text=True, timeout=120)

    def write(self, relative, content):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def commit(self, *arguments):
        log = self.root / "hook-log.jsonl"
        if log.exists():
            log.unlink()
        result = self.git("commit", "--quiet", *arguments, check=False)
        entries = []
        if log.exists():
            entries = [json.loads(line) for line in log.read_text(encoding="utf-8-sig").splitlines() if line]
        return result, entries

    def checks(self, entries):
        return [entry["command"] for entry in entries if entry["tool"] == "dev"]

    def audit(self, entries):
        audits = [entry for entry in entries if entry["tool"] == "audit"]
        self.assertEqual(len(audits), 1, entries)
        self.assertEqual(audits[0]["arguments"], ["audit", "--staged"])
        return audits[0]

    def assert_children_have_a_clean_git_environment(self, entries):
        for entry in entries:
            if entry["tool"] in ("doctor", "dev"):
                self.assertIn(entry["index"], (None, ""), entry)

    def test_commit_all_is_audited_against_the_index_being_committed(self):
        self.write("internal/app.go", "package app\n\n// PLANTED-SECRET\n")
        result, entries = self.commit("-a", "-m", "include the secret")
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        audit = self.audit(entries)
        self.assertEqual(audit["staged"], ["internal/app.go"])
        self.assertTrue(Path(audit["index"]).is_absolute(), audit)
        self.assertEqual(self.git("log", "--format=%s").stdout.split("\n")[0], "base")

    def test_commit_all_routes_the_files_it_commits(self):
        self.write("internal/app.go", "package app\n\nconst Name = \"app\"\n")
        result, entries = self.commit("-a", "-m", "change go")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.audit(entries)["staged"], ["internal/app.go"])
        self.assertEqual(self.checks(entries), ["check-go"])
        self.assert_children_have_a_clean_git_environment(entries)

    def test_a_path_commit_routes_only_the_named_paths(self):
        self.write("internal/app.go", "package app\n\nconst Name = \"app\"\n")
        self.write("docs/notes.md", "changed notes\n")
        result, entries = self.commit("-m", "change go only", "--", "internal/app.go")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.audit(entries)["staged"], ["internal/app.go"])
        self.assertEqual(self.checks(entries), ["check-go"])
        self.assert_children_have_a_clean_git_environment(entries)

    def test_a_deleted_go_file_still_runs_the_go_gate(self):
        self.git("rm", "--quiet", "--", "internal/app.go")
        result, entries = self.commit("-m", "remove go")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.checks(entries), ["check-go"])
        self.assert_children_have_a_clean_git_environment(entries)

    def test_an_ordinary_documentation_commit_runs_no_subsystem_gate(self):
        self.write("docs/notes.md", "changed notes\n")
        self.git("add", "--", "docs/notes.md")
        result, entries = self.commit("-m", "docs")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        audit = self.audit(entries)
        self.assertEqual(audit["staged"], ["docs/notes.md"])
        # A plain commit hands the hook a relative index path; the audit must
        # still receive one it can open from any directory.
        self.assertTrue(Path(audit["index"]).is_absolute(), audit)
        self.assertEqual(self.checks(entries), [])
        self.assertIn("doctor", [entry["tool"] for entry in entries])
        self.assert_children_have_a_clean_git_environment(entries)


if __name__ == "__main__":
    unittest.main()

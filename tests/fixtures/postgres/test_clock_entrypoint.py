"""Fixture-only process tests. No Docker service or real PostgreSQL is started."""

import os
import secrets
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


class ClockEntrypointTests(unittest.TestCase):
    def run_fixture(self, signal=None, offset="+24h", existing=False):
        shell = shutil.which("sh")
        if shell is None:
            raise RuntimeError("the selected fixture test requires a POSIX shell")
        with tempfile.TemporaryDirectory(prefix="effectus-clock-signals-") as temp:
            root = Path(temp)
            binaries, data = root / "bin", root / "data"
            binaries.mkdir()
            data.mkdir()
            if existing:
                (data / "PG_VERSION").write_text("owned fixture marker")
            initialization = "#!/bin/sh\n"
            if signal:
                initialization += f'kill -{signal} "$PPID"\nsleep 0.05\n'
            (binaries / "initdb").write_text(initialization)
            (binaries / "postgres").write_text(
                '#!/bin/sh\nprintf started > "$PGDATA/unexpected-start"\n'
            )
            for path in binaries.iterdir():
                path.chmod(0o700)
            env = dict(os.environ)
            for key in ("LD_PRELOAD", "FAKETIME", "FAKETIME_DONT_FAKE_MONOTONIC"):
                env.pop(key, None)
            env.update(
                PATH=str(binaries) + os.pathsep + os.defpath,
                TMPDIR=str(root),
                PGDATA=str(data),
                POSTGRES_PASSWORD=secrets.token_hex(24),
                EFFECTUS_CLOCK_OFFSET=offset,
            )
            result = subprocess.run(
                [shell, str(Path(__file__).with_name("clock-skew-entrypoint.sh"))],
                env=env,
                capture_output=True,
                text=True,
                timeout=5,
                check=False,
            )
            self.assertFalse((data / "unexpected-start").exists())
            self.assertEqual([], list(root.glob("tmp.*")))
            if existing:
                self.assertEqual(
                    "owned fixture marker", (data / "PG_VERSION").read_text()
                )
                self.assertEqual(
                    ["PG_VERSION"], sorted(path.name for path in data.iterdir())
                )
            return result

    def test_startup_signals_exit_without_starting_the_server(self):
        for signal, expected in (("HUP", 129), ("INT", 130), ("TERM", 143)):
            with self.subTest(signal=signal):
                self.assertEqual(expected, self.run_fixture(signal=signal).returncode)

    def test_invalid_offset_fails_before_initialization(self):
        result = self.run_fixture(offset="+1h")
        self.assertEqual(64, result.returncode)
        self.assertIn("EFFECTUS_CLOCK_OFFSET must be +24h or -24h", result.stderr)

    def test_existing_database_marker_is_untouched(self):
        result = self.run_fixture(existing=True)
        self.assertEqual(64, result.returncode)
        self.assertIn("requires an empty, task-owned data directory", result.stderr)


if __name__ == "__main__":
    unittest.main()

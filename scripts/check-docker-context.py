#!/usr/bin/env python3
"""Check Docker's actual ignore semantics using only harmless private fixtures."""
from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SECRET_FIXTURES = (
    "fixture.pem", "backend/fixture.pem", "backend/nested/fixture.key",
    "backend/nested/fixture.p12", "backend/nested/fixture.pfx",
    "certs/private.data", "certificates/private.data",
    "backend/certs/private.key", "backend/certificates/client.pem",
    "backend/nested/certs/private.data", "backend/nested/certificates/private.data",
    ".env", "backend/.env", "backend/nested/.env",
    ".env.local", "backend/nested/.env.production",
    ".local/keys/demo.key", "backend/nested/.local/keys/demo.key",
)
REQUIRED_FIXTURES = ("backend/main.go", ".env.example", "backend/nested/.env.example")


def check_context(policy: Path) -> None:
    # Only this synthetic context is sent to Docker. Never inspect repository secrets.
    with tempfile.TemporaryDirectory(prefix="sentinel-docker-context-") as directory:
        root = Path(directory)
        context, exported = root / "fixture", root / "exported"
        context.mkdir(mode=0o700)
        shutil.copyfile(policy, context / ".dockerignore")
        for name in SECRET_FIXTURES + REQUIRED_FIXTURES:
            target = context / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text("harmless synthetic fixture\n", encoding="utf-8")
        # No syntax frontend or base image download: use the built-in frontend.
        (context / "Dockerfile").write_text(
            "FROM scratch\nCOPY . /context/\n", encoding="utf-8"
        )
        try:
            subprocess.run(
                ["docker", "buildx", "build", "--network=none", "--progress=quiet",
                 "--output", f"type=local,dest={exported}", str(context)],
                check=True, capture_output=True, timeout=60,
            )
        except (OSError, subprocess.SubprocessError) as error:
            raise RuntimeError("Docker fixture export failed or exceeded 60 seconds") from error
        leaked = [name for name in SECRET_FIXTURES if (exported / "context" / name).exists()]
        missing = [name for name in REQUIRED_FIXTURES if not (exported / "context" / name).is_file()]
        if leaked:
            raise RuntimeError("private fixture leaked into Docker context: " + ", ".join(leaked))
        if missing:
            raise RuntimeError("required fixture missing from Docker context: " + ", ".join(missing))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dockerignore", type=Path, default=ROOT / ".dockerignore")
    arguments = parser.parse_args()
    try:
        check_context(arguments.dockerignore)
    except (OSError, RuntimeError) as error:
        # Do not forward daemon output, paths, credentials, or environment data.
        if isinstance(error, RuntimeError):
            print(f"FAIL: {error}", file=sys.stderr)
        else:
            print("FAIL: cannot read fixture policy", file=sys.stderr)
        return 1
    print("PASS: Docker context excludes private fixtures and retains required source")
    return 0


if __name__ == "__main__":
    sys.exit(main())

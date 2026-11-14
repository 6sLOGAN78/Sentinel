"""Derive keyctl's PG* connection environment from the repo .env.

backend/tools/keyctl reads only PGHOST, PGPORT, PGUSER, PGPASSWORD, PGDATABASE
and PGSSLMODE. On a clean runner nothing exports those, so the Phase 4 browser
seeder maps them from the SENTINEL_DATABASE.* values in the repo .env. Values
already present in the base environment win over the file, matching godotenv's
no-override semantics. Nothing is printed, and errors never include values.
"""

from __future__ import annotations

from collections.abc import Mapping
from pathlib import Path

PREFIX = "SENTINEL_DATABASE."
REQUIRED = (
    ("HOST", "PGHOST"),
    ("PORT", "PGPORT"),
    ("USER", "PGUSER"),
    ("PASSWORD", "PGPASSWORD"),
    ("NAME", "PGDATABASE"),
)
OPTIONAL = (("SSL_MODE", "PGSSLMODE"),)
MAX_PORT = 65535


def _parse(env_file: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in env_file.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key, value = key.strip(), value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        values[key] = value
    return values


def keyctl_env(env_file: Path, base: Mapping[str, str]) -> dict[str, str]:
    """Return a copy of base plus the PG* keys keyctl needs."""
    merged = _parse(env_file)
    merged.update({key: value for key, value in base.items() if key.startswith(PREFIX)})
    result = dict(base)
    for suffix, target in REQUIRED:
        key = PREFIX + suffix
        value = merged.get(key, "")
        if not value:
            raise ValueError(f"{key} is missing or empty in {env_file.name}")
        result[target] = value
    port_key = PREFIX + "PORT"
    port = result["PGPORT"]
    if not port.isdigit() or not 1 <= int(port) <= MAX_PORT:
        raise ValueError(f"{port_key} must be an integer in 1..{MAX_PORT}")
    for suffix, target in OPTIONAL:
        value = merged.get(PREFIX + suffix, "")
        if value:
            result[target] = value
    return result

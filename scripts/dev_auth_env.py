#!/usr/bin/env python3
"""Launch a local child with a private, stable dashboard JWT signing key."""

from __future__ import annotations

import fcntl
import os
from pathlib import Path
import secrets
import stat
import sys

NORMAL = "SENTINEL_AUTH_JWT_SECRET"
DOTTED = "SENTINEL_AUTH.JWT_SECRET"
MAX_KEY_BYTES = 4096


class DevAuthError(RuntimeError):
    """Sanitized local-bootstrap failure; never includes credential material."""


def _validate_key(value: str) -> str:
    if not 32 <= len(value.encode("utf-8")) <= MAX_KEY_BYTES or not value.strip() or any(
        char in value for char in "\x00\r\n"
    ):
        raise DevAuthError("JWT key must contain 32..4096 bytes without line breaks")
    return value


def _check_private(descriptor: int, *, directory: bool) -> None:
    info = os.fstat(descriptor)
    expected = 0o700 if directory else 0o600
    valid_type = stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)
    if (not valid_type or info.st_uid != os.getuid() or
            stat.S_IMODE(info.st_mode) != expected or (not directory and info.st_nlink != 1)):
        raise DevAuthError("JWT credential path must be owned, private and not linked")


def _private_key(root: Path) -> str:
    descriptors: list[int] = []
    temporary: str | None = None
    parent = -1
    try:
        flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
        parent = os.open(root, flags)
        descriptors.append(parent)
        for name in (".local", "auth"):
            try:
                os.mkdir(name, mode=0o700, dir_fd=parent)
            except FileExistsError:
                pass
            parent = os.open(name, flags, dir_fd=parent)
            descriptors.append(parent)
            _check_private(parent, directory=True)
        # Lock the checked directory itself: no replaceable lock-file path and
        # no reader can observe another invocation's incomplete publication.
        fcntl.flock(parent, fcntl.LOCK_EX)
        try:
            descriptor = os.open("jwt.key", os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent)
        except FileNotFoundError:
            temporary = ".jwt-" + secrets.token_hex(16)
            descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                                 0o600, dir_fd=parent)
            with os.fdopen(descriptor, "wb") as stream:
                stream.write(secrets.token_urlsafe(48).encode("ascii"))
                stream.flush()
                os.fsync(stream.fileno())
            # link refuses to overwrite an existing user credential.
            os.link(temporary, "jwt.key", src_dir_fd=parent, dst_dir_fd=parent, follow_symlinks=False)
            os.unlink(temporary, dir_fd=parent)
            temporary = None
            os.fsync(parent)
            descriptor = os.open("jwt.key", os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent)
        try:
            _check_private(descriptor, directory=False)
            content = os.read(descriptor, MAX_KEY_BYTES + 1)
            return _validate_key(content.decode("utf-8"))
        finally:
            os.close(descriptor)
    except (OSError, UnicodeError) as error:
        raise DevAuthError("cannot safely provision the private local JWT key") from error
    finally:
        if temporary is not None and parent >= 0:
            try:
                os.unlink(temporary, dir_fd=parent)
            except OSError:
                pass
        for descriptor in reversed(descriptors):
            os.close(descriptor)


def child_environment(root: Path, environment: dict[str, str]) -> dict[str, str]:
    """Return a child-only environment; explicit nonlocal modes fail closed."""
    for name in ("SENTINEL_PRIMARY.ENV", "SENTINEL_PRIMARY_ENV"):
        if name in environment and environment[name] not in ("local", "test"):
            raise DevAuthError("JWT bootstrap is restricted to local/test mode")
    overrides = [environment[name] for name in (NORMAL, DOTTED) if name in environment]
    if overrides and any(value != overrides[0] for value in overrides):
        raise DevAuthError("conflicting JWT environment overrides")
    key = _validate_key(overrides[0]) if overrides else _private_key(root)
    return {**environment, NORMAL: key, DOTTED: key}


def main() -> int:
    command = sys.argv[1:]
    if command[:1] == ["--"]:
        command = command[1:]
    if not command:
        print("usage: dev_auth_env.py -- command [args...]", file=sys.stderr)
        return 2
    try:
        environment = child_environment(Path(__file__).resolve().parents[1], dict(os.environ))
        os.execvpe(command[0], command, environment)
    except (DevAuthError, OSError, UnicodeError):
        print("local JWT launch failed: check private key permissions, override and local/test mode", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

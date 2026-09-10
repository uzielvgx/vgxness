"""Scratch paths for offline tests; runtime symlink rejection stays strict."""
import pathlib
import tempfile


def temporary_directory():
    # macOS exposes its default temp root via /var -> /private/var.
    return tempfile.TemporaryDirectory(dir=pathlib.Path(tempfile.gettempdir()).resolve(strict=True))

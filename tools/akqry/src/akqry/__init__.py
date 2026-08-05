"""akqry: auditable discovery and retrieval for AkShare."""

from importlib.metadata import PackageNotFoundError, version

try:
    from ._version import version as __version__
except ImportError:
    try:
        __version__ = version("akqry")
    except PackageNotFoundError:
        __version__ = "0+unknown"

"""Aggregate of per-pillar metadata — single import entry point.

The generator imports REPOS_FULL from here. Per-pillar modules stay under the
800-line pre-commit guard; this file unions them. Both absolute and relative
imports are supported so the module works standalone or as a package.
"""
try:
    from .metadata_platform import REPOS as _platform
    from .metadata_business import REPOS as _business
    from .metadata_fabrication import REPOS as _fabrication
    from .metadata_intelligence import REPOS as _intelligence
    from .metadata_experience import REPOS as _experience
except ImportError:
    from metadata_platform import REPOS as _platform
    from metadata_business import REPOS as _business
    from metadata_fabrication import REPOS as _fabrication
    from metadata_intelligence import REPOS as _intelligence
    from metadata_experience import REPOS as _experience

REPOS_FULL = {
    **_platform,
    **_business,
    **_fabrication,
    **_intelligence,
    **_experience,
}

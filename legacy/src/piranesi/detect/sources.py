"""Re-exports source specifications from scan.specs for backward compatibility."""

from piranesi.scan.specs import BUILTIN_SOURCE_SPECS, SourceSpec, SourceType

__all__ = ["BUILTIN_SOURCE_SPECS", "SourceSpec", "SourceType"]

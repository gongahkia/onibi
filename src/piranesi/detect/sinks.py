"""Re-exports sink specifications from scan.specs for backward compatibility."""

from piranesi.scan.specs import BUILTIN_SINK_SPECS, SinkSpec, SinkType

__all__ = ["BUILTIN_SINK_SPECS", "SinkSpec", "SinkType"]

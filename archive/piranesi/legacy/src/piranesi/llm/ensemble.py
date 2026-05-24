"""Re-exports ensemble voter from triage module.

The calibrated ensemble voter implementation lives in piranesi.triage.ensemble.
This module provides a convenience re-export.
"""

from piranesi.triage.ensemble import CalibratedEnsembleVoter, EnsembleDecision

__all__ = ["CalibratedEnsembleVoter", "EnsembleDecision"]

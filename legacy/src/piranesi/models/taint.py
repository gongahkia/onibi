from __future__ import annotations

from pydantic import BaseModel, ConfigDict


class SourceLocation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    file: str
    line: int
    column: int
    end_line: int | None = None
    end_column: int | None = None
    snippet: str


class TaintSource(BaseModel):
    model_config = ConfigDict(extra="forbid")

    location: SourceLocation
    source_type: str
    data_categories: list[str]
    parameter_name: str | None = None


class TaintSink(BaseModel):
    model_config = ConfigDict(extra="forbid")

    location: SourceLocation
    sink_type: str
    api_name: str


class TaintStep(BaseModel):
    model_config = ConfigDict(extra="forbid")

    location: SourceLocation
    operation: str
    taint_state: str
    through_function: str | None = None
    sanitizer_applied: str | None = None


class PathCondition(BaseModel):
    model_config = ConfigDict(extra="forbid")

    location: SourceLocation
    condition_type: str
    expression: str
    required_value: bool
    symbolic_constraint: str | None = None

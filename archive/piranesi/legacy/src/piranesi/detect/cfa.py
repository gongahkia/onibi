from __future__ import annotations

import hashlib
from collections import defaultdict
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True, slots=True)
class CallContext:
    """Immutable k-CFA call string."""

    chain: tuple[tuple[str, int], ...]

    @staticmethod
    def empty() -> CallContext:
        return CallContext(chain=())

    def extend(self, caller: str, line: int, *, k: int) -> CallContext:
        if k <= 0:
            return CallContext.empty()
        extended = (*self.chain, (caller, line))
        return CallContext(chain=extended[-k:])

    def __str__(self) -> str:
        if not self.chain:
            return "∅"
        return "→".join(f"{caller}:{line}" for caller, line in self.chain)


@dataclass(frozen=True, slots=True)
class TaintSignature:
    """Abstract entry taint state for memoized summary reuse."""

    tainted_params: tuple[int, ...]

    @staticmethod
    def empty() -> TaintSignature:
        return TaintSignature(tainted_params=())

    @staticmethod
    def from_indexes(indexes: Iterable[int]) -> TaintSignature:
        return TaintSignature(tainted_params=tuple(sorted(set(indexes))))

    @staticmethod
    def from_origin_sets(origins_by_param: Sequence[Sequence[Any] | set[Any]]) -> TaintSignature:
        return TaintSignature.from_indexes(
            index for index, origins in enumerate(origins_by_param) if origins
        )

    def cache_key(self) -> str:
        digest = hashlib.sha256()
        digest.update(",".join(str(index) for index in self.tainted_params).encode("utf-8"))
        return digest.hexdigest()

    def __str__(self) -> str:
        if not self.tainted_params:
            return "clean"
        return ",".join(str(index) for index in self.tainted_params)


@dataclass(frozen=True, slots=True)
class ContextualSummary:
    """Opaque per-context summary wrapper stored by the CFA cache."""

    function_key: str
    context: CallContext
    taint_signature: TaintSignature
    payload: Any


@dataclass(frozen=True, slots=True)
class ContextAnalysisConfig:
    context_sensitivity: int = 1
    max_contexts: int = 1000
    hot_threshold: int = 50
    context_timeout: int = 300


@dataclass(slots=True)
class ContextSensitiveStore:
    """Tracks contextual summaries and split/degrade decisions."""

    config: ContextAnalysisConfig
    _store: dict[tuple[str, CallContext, TaintSignature], ContextualSummary] = field(
        default_factory=dict
    )
    _requests_by_function: dict[str, set[tuple[CallContext, TaintSignature]]] = field(
        default_factory=lambda: defaultdict(set)
    )
    _hot_functions: set[str] = field(default_factory=set)
    _collapsed_functions: set[str] = field(default_factory=set)
    _aliases: dict[
        tuple[str, CallContext, TaintSignature],
        tuple[str, CallContext, TaintSignature],
    ] = field(default_factory=dict)
    degraded: bool = False

    def get(
        self,
        function_key: str,
        context: CallContext,
        taint_signature: TaintSignature,
    ) -> ContextualSummary | None:
        key = self._canonical_key(function_key, context, taint_signature)
        return self._store.get(key)

    def put(
        self,
        function_key: str,
        context: CallContext,
        taint_signature: TaintSignature,
        payload: Any,
    ) -> ContextualSummary:
        self.record_request(function_key, context, taint_signature)
        summary = ContextualSummary(
            function_key=function_key,
            context=context,
            taint_signature=taint_signature,
            payload=payload,
        )
        key = (function_key, context, taint_signature)
        self._store[key] = summary
        return summary

    def alias(
        self,
        function_key: str,
        requested_context: CallContext,
        taint_signature: TaintSignature,
        *,
        canonical_context: CallContext,
    ) -> None:
        requested = (function_key, requested_context, taint_signature)
        canonical = (function_key, canonical_context, taint_signature)
        self._aliases[requested] = canonical

    def record_request(
        self,
        function_key: str,
        context: CallContext,
        taint_signature: TaintSignature,
    ) -> None:
        self._requests_by_function[function_key].add((context, taint_signature))

    def should_split(
        self,
        function_key: str,
        context: CallContext,
        taint_signature: TaintSignature,
    ) -> bool:
        if self.config.context_sensitivity <= 0 or self.degraded:
            return False
        if function_key in self._hot_functions or function_key in self._collapsed_functions:
            return False
        requests = self._requests_by_function[function_key]
        requests.add((context, taint_signature))
        return len(requests) > 1

    def effective_context(
        self,
        function_key: str,
        requested_context: CallContext,
        taint_signature: TaintSignature,
    ) -> CallContext:
        if (
            self.config.context_sensitivity <= 0
            or self.degraded
            or function_key in self._hot_functions
            or function_key in self._collapsed_functions
        ):
            return CallContext.empty()
        if not self.should_split(function_key, requested_context, taint_signature):
            return CallContext.empty()
        if self.context_count(function_key) >= self.config.max_contexts:
            self._collapsed_functions.add(function_key)
            self.degraded = True
            return CallContext.empty()
        return requested_context

    def context_count(self, function_key: str) -> int:
        return len(
            {
                context
                for (stored_function_key, context, _signature) in self._store
                if stored_function_key == function_key
            }
        )

    def all_contexts(self, function_key: str) -> tuple[ContextualSummary, ...]:
        return tuple(
            summary
            for (stored_function_key, _context, _signature), summary in self._store.items()
            if stored_function_key == function_key
        )

    def mark_hot(self, function_key: str) -> None:
        self._hot_functions.add(function_key)

    def is_hot(self, function_key: str) -> bool:
        return function_key in self._hot_functions

    def collapse(self, function_key: str) -> None:
        self._collapsed_functions.add(function_key)
        self.degraded = True

    def is_collapsed(self, function_key: str) -> bool:
        return function_key in self._collapsed_functions

    def _canonical_key(
        self,
        function_key: str,
        context: CallContext,
        taint_signature: TaintSignature,
    ) -> tuple[str, CallContext, TaintSignature]:
        key = (function_key, context, taint_signature)
        return self._aliases.get(key, key)


@dataclass(frozen=True, slots=True)
class DispatchFunction:
    function_key: str
    name: str
    module_path: str
    language: str
    class_name: str | None = None


@dataclass(frozen=True, slots=True)
class DispatchCall:
    callee: str
    receiver: str | None = None
    receiver_types: tuple[str, ...] = ()
    language: str = "javascript"


@dataclass(slots=True)
class DispatchResolver:
    """Resolves direct and dynamic dispatch with lightweight type hints."""

    functions: Sequence[DispatchFunction]
    bases_by_class: Mapping[str, Sequence[str]] = field(default_factory=dict)
    _functions_by_name: dict[str, tuple[DispatchFunction, ...]] = field(init=False)
    _methods_by_name: dict[str, tuple[DispatchFunction, ...]] = field(init=False)
    _methods_by_class: dict[tuple[str, str], tuple[DispatchFunction, ...]] = field(init=False)

    def __post_init__(self) -> None:
        direct_by_name: dict[str, list[DispatchFunction]] = defaultdict(list)
        methods_by_name: dict[str, list[DispatchFunction]] = defaultdict(list)
        methods_by_class: dict[tuple[str, str], list[DispatchFunction]] = defaultdict(list)
        for function in self.functions:
            if function.class_name is None:
                direct_by_name[function.name].append(function)
                continue
            methods_by_name[function.name].append(function)
            methods_by_class[(function.class_name, function.name)].append(function)
        self._functions_by_name = {name: tuple(entries) for name, entries in direct_by_name.items()}
        self._methods_by_name = {name: tuple(entries) for name, entries in methods_by_name.items()}
        self._methods_by_class = {key: tuple(entries) for key, entries in methods_by_class.items()}

    def resolve(self, call: DispatchCall) -> tuple[DispatchFunction, ...]:
        if call.receiver is None:
            direct = self._functions_by_name.get(call.callee, ())
            if direct:
                return direct
            return self._methods_by_name.get(call.callee, ())

        resolved: list[DispatchFunction] = []
        for receiver_type in call.receiver_types:
            resolved.extend(self._methods_for_type(receiver_type, call.callee, call.language))
        if call.receiver_types:
            return tuple(_dedupe_dispatch_functions(resolved))

        direct = self._functions_by_name.get(call.callee, ())
        if direct:
            return direct
        return self._methods_by_name.get(call.callee, ())

    def mro_for(self, class_name: str) -> tuple[str, ...]:
        order: list[str] = []
        seen: set[str] = set()

        def visit(candidate: str) -> None:
            if candidate in seen:
                return
            seen.add(candidate)
            order.append(candidate)
            for base in self.bases_by_class.get(candidate, ()):
                visit(base)

        visit(class_name)
        return tuple(order)

    def _methods_for_type(
        self,
        type_name: str,
        method_name: str,
        language: str,
    ) -> tuple[DispatchFunction, ...]:
        if language == "python":
            for candidate in self.mro_for(type_name):
                methods = self._methods_by_class.get((candidate, method_name), ())
                if methods:
                    return methods
            return ()

        resolved: list[DispatchFunction] = []
        for candidate in self.mro_for(type_name):
            resolved.extend(self._methods_by_class.get((candidate, method_name), ()))
        return tuple(_dedupe_dispatch_functions(resolved))


def _dedupe_dispatch_functions(
    functions: Iterable[DispatchFunction],
) -> list[DispatchFunction]:
    deduped: list[DispatchFunction] = []
    seen: set[str] = set()
    for function in functions:
        if function.function_key in seen:
            continue
        seen.add(function.function_key)
        deduped.append(function)
    return deduped


__all__ = [
    "CallContext",
    "ContextAnalysisConfig",
    "ContextSensitiveStore",
    "ContextualSummary",
    "DispatchCall",
    "DispatchFunction",
    "DispatchResolver",
    "TaintSignature",
]

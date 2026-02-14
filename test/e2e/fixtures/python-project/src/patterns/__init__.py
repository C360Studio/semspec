"""Design patterns and advanced Python features."""

from .decorators import timer, retry, deprecated, singleton
from .async_patterns import AsyncWorker, TaskQueue
from .protocols import Comparable, Hashable, Printable

__all__ = [
    "timer",
    "retry",
    "deprecated",
    "singleton",
    "AsyncWorker",
    "TaskQueue",
    "Comparable",
    "Hashable",
    "Printable",
]

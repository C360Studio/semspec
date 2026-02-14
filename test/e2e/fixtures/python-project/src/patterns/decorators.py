"""Decorator patterns and utilities."""

import functools
import time
import warnings
from typing import Any, Callable, Optional, TypeVar, Type
import logging


logger = logging.getLogger(__name__)


F = TypeVar('F', bound=Callable[..., Any])


def timer(func: F) -> F:
    """Decorator that logs the execution time of a function.

    Example:
        @timer
        def slow_function():
            time.sleep(1)
    """
    @functools.wraps(func)
    def wrapper(*args: Any, **kwargs: Any) -> Any:
        start = time.perf_counter()
        try:
            return func(*args, **kwargs)
        finally:
            elapsed = time.perf_counter() - start
            logger.debug(f"{func.__name__} executed in {elapsed:.4f}s")
    return wrapper  # type: ignore


def retry(
    max_attempts: int = 3,
    delay: float = 1.0,
    backoff: float = 2.0,
    exceptions: tuple = (Exception,)
) -> Callable[[F], F]:
    """Decorator that retries a function on failure.

    Args:
        max_attempts: Maximum number of retry attempts
        delay: Initial delay between retries in seconds
        backoff: Multiplier applied to delay after each retry
        exceptions: Tuple of exception types to catch

    Example:
        @retry(max_attempts=3, delay=0.5)
        def unstable_api_call():
            ...
    """
    def decorator(func: F) -> F:
        @functools.wraps(func)
        def wrapper(*args: Any, **kwargs: Any) -> Any:
            current_delay = delay
            last_exception: Optional[Exception] = None

            for attempt in range(max_attempts):
                try:
                    return func(*args, **kwargs)
                except exceptions as e:
                    last_exception = e
                    if attempt < max_attempts - 1:
                        logger.warning(
                            f"{func.__name__} failed (attempt {attempt + 1}/{max_attempts}): {e}"
                        )
                        time.sleep(current_delay)
                        current_delay *= backoff

            raise last_exception  # type: ignore

        return wrapper  # type: ignore

    return decorator


def deprecated(
    reason: str = "",
    version: Optional[str] = None,
    replacement: Optional[str] = None
) -> Callable[[F], F]:
    """Mark a function as deprecated with optional replacement info.

    Args:
        reason: Why the function is deprecated
        version: Version when function will be removed
        replacement: Name of replacement function

    Example:
        @deprecated(reason="Use new_func instead", version="2.0")
        def old_func():
            ...
    """
    def decorator(func: F) -> F:
        @functools.wraps(func)
        def wrapper(*args: Any, **kwargs: Any) -> Any:
            message = f"{func.__name__} is deprecated."
            if reason:
                message += f" {reason}"
            if replacement:
                message += f" Use {replacement} instead."
            if version:
                message += f" Will be removed in version {version}."

            warnings.warn(message, DeprecationWarning, stacklevel=2)
            return func(*args, **kwargs)

        return wrapper  # type: ignore

    return decorator


def singleton(cls: Type) -> Type:
    """Class decorator that implements the singleton pattern.

    Example:
        @singleton
        class Database:
            def __init__(self):
                self.connection = create_connection()
    """
    instances: dict = {}

    @functools.wraps(cls)
    def get_instance(*args: Any, **kwargs: Any) -> Any:
        if cls not in instances:
            instances[cls] = cls(*args, **kwargs)
        return instances[cls]

    return get_instance  # type: ignore


def memoize(func: F) -> F:
    """Cache function results based on arguments.

    Uses a dictionary to store results keyed by argument tuple.
    For more advanced caching, use functools.lru_cache.

    Example:
        @memoize
        def fibonacci(n):
            if n < 2:
                return n
            return fibonacci(n - 1) + fibonacci(n - 2)
    """
    cache: dict = {}

    @functools.wraps(func)
    def wrapper(*args: Any) -> Any:
        if args not in cache:
            cache[args] = func(*args)
        return cache[args]

    wrapper.cache = cache  # type: ignore
    wrapper.cache_clear = cache.clear  # type: ignore

    return wrapper  # type: ignore


def validate_args(**validators: Callable[[Any], bool]) -> Callable[[F], F]:
    """Decorator that validates function arguments.

    Args:
        **validators: Mapping of argument names to validation functions

    Example:
        @validate_args(x=lambda x: x > 0, y=lambda y: isinstance(y, str))
        def process(x, y):
            ...
    """
    def decorator(func: F) -> F:
        @functools.wraps(func)
        def wrapper(*args: Any, **kwargs: Any) -> Any:
            # Get argument names from function signature
            import inspect
            sig = inspect.signature(func)
            bound = sig.bind(*args, **kwargs)
            bound.apply_defaults()

            for arg_name, validator in validators.items():
                if arg_name in bound.arguments:
                    value = bound.arguments[arg_name]
                    if not validator(value):
                        raise ValueError(
                            f"Invalid value for argument '{arg_name}': {value}"
                        )

            return func(*args, **kwargs)

        return wrapper  # type: ignore

    return decorator


class classproperty:
    """Decorator for class-level properties.

    Similar to @property but works on the class itself,
    not instances.

    Example:
        class Config:
            _env = "development"

            @classproperty
            def environment(cls):
                return cls._env
    """

    def __init__(self, method: Callable):
        self.method = method
        functools.update_wrapper(self, method)

    def __get__(self, obj: Any, objtype: Type) -> Any:
        return self.method(objtype)

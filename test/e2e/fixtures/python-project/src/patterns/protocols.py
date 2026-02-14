"""Protocol definitions for structural typing."""

from typing import Protocol, TypeVar, runtime_checkable, Any, Iterator


T_co = TypeVar('T_co', covariant=True)
T_contra = TypeVar('T_contra', contravariant=True)


@runtime_checkable
class Comparable(Protocol):
    """Protocol for objects that can be compared.

    Any class implementing __lt__ is considered Comparable.
    This enables structural typing - no explicit inheritance needed.

    Example:
        class Point:
            def __init__(self, x: int, y: int):
                self.x = x
                self.y = y

            def __lt__(self, other: 'Point') -> bool:
                return (self.x, self.y) < (other.x, other.y)

        # Point is now Comparable without explicit inheritance
        assert isinstance(Point(1, 2), Comparable)
    """

    def __lt__(self, other: Any) -> bool:
        """Compare less than."""
        ...


@runtime_checkable
class Hashable(Protocol):
    """Protocol for hashable objects.

    Objects implementing __hash__ are considered Hashable.
    Required for use as dictionary keys or set members.
    """

    def __hash__(self) -> int:
        """Return hash value."""
        ...


@runtime_checkable
class Printable(Protocol):
    """Protocol for objects with string representation.

    Any object with __str__ is considered Printable.
    """

    def __str__(self) -> str:
        """Return string representation."""
        ...


@runtime_checkable
class Sizeable(Protocol):
    """Protocol for objects with a size/length."""

    def __len__(self) -> int:
        """Return the size."""
        ...


@runtime_checkable
class Iterable(Protocol[T_co]):
    """Protocol for iterable objects."""

    def __iter__(self) -> Iterator[T_co]:
        """Return an iterator."""
        ...


class Reader(Protocol[T_co]):
    """Protocol for readable streams.

    Defines the minimal interface for reading data.
    """

    def read(self, size: int = -1) -> T_co:
        """Read up to size bytes/characters.

        Args:
            size: Maximum amount to read (-1 for all)

        Returns:
            The read data
        """
        ...

    def readline(self) -> T_co:
        """Read a single line.

        Returns:
            The line read
        """
        ...


class Writer(Protocol[T_contra]):
    """Protocol for writable streams.

    Defines the minimal interface for writing data.
    """

    def write(self, data: T_contra) -> int:
        """Write data to the stream.

        Args:
            data: The data to write

        Returns:
            Number of bytes/characters written
        """
        ...

    def flush(self) -> None:
        """Flush buffered data."""
        ...


class Closeable(Protocol):
    """Protocol for closeable resources."""

    def close(self) -> None:
        """Close the resource."""
        ...


class ContextManager(Protocol[T_co]):
    """Protocol for context managers.

    Objects implementing __enter__ and __exit__ support
    the `with` statement.
    """

    def __enter__(self) -> T_co:
        """Enter the context."""
        ...

    def __exit__(
        self,
        exc_type: Any,
        exc_val: Any,
        exc_tb: Any
    ) -> bool:
        """Exit the context.

        Args:
            exc_type: Exception type if an error occurred
            exc_val: Exception value if an error occurred
            exc_tb: Exception traceback if an error occurred

        Returns:
            True to suppress the exception, False to propagate
        """
        ...


class AsyncContextManager(Protocol[T_co]):
    """Protocol for async context managers.

    Objects implementing __aenter__ and __aexit__ support
    the `async with` statement.
    """

    async def __aenter__(self) -> T_co:
        """Enter the async context."""
        ...

    async def __aexit__(
        self,
        exc_type: Any,
        exc_val: Any,
        exc_tb: Any
    ) -> bool:
        """Exit the async context."""
        ...


class Subscriber(Protocol[T_contra]):
    """Protocol for pub/sub subscribers.

    Part of the observer pattern for event handling.
    """

    def on_next(self, value: T_contra) -> None:
        """Handle a new value.

        Args:
            value: The published value
        """
        ...

    def on_error(self, error: Exception) -> None:
        """Handle an error.

        Args:
            error: The error that occurred
        """
        ...

    def on_complete(self) -> None:
        """Handle completion of the stream."""
        ...


class Publisher(Protocol[T_co]):
    """Protocol for pub/sub publishers."""

    def subscribe(self, subscriber: Subscriber[T_co]) -> None:
        """Add a subscriber.

        Args:
            subscriber: The subscriber to add
        """
        ...

    def unsubscribe(self, subscriber: Subscriber[T_co]) -> None:
        """Remove a subscriber.

        Args:
            subscriber: The subscriber to remove
        """
        ...

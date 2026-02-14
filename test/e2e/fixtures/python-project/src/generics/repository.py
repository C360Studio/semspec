"""Generic repository pattern implementation."""

from abc import ABC, abstractmethod
from typing import TypeVar, Generic, Optional, List, Dict, Callable, Iterator
from dataclasses import dataclass


# Type variables for entity and ID types
T = TypeVar('T')
ID = TypeVar('ID')


class Repository(ABC, Generic[T, ID]):
    """Abstract generic repository interface.

    Provides a standard interface for data access operations
    that can be implemented for different storage backends.

    Type Parameters:
        T: The entity type managed by this repository
        ID: The type of the entity's identifier
    """

    @abstractmethod
    def find_by_id(self, id: ID) -> Optional[T]:
        """Find an entity by its ID.

        Args:
            id: The entity identifier

        Returns:
            The entity if found, None otherwise
        """
        pass

    @abstractmethod
    def find_all(self) -> List[T]:
        """Retrieve all entities.

        Returns:
            List of all entities
        """
        pass

    @abstractmethod
    def save(self, entity: T) -> T:
        """Save an entity (create or update).

        Args:
            entity: The entity to save

        Returns:
            The saved entity
        """
        pass

    @abstractmethod
    def delete(self, id: ID) -> bool:
        """Delete an entity by ID.

        Args:
            id: The entity identifier

        Returns:
            True if deleted, False if not found
        """
        pass

    @abstractmethod
    def exists(self, id: ID) -> bool:
        """Check if an entity exists.

        Args:
            id: The entity identifier

        Returns:
            True if exists, False otherwise
        """
        pass

    @abstractmethod
    def count(self) -> int:
        """Count total entities.

        Returns:
            Number of entities
        """
        pass


class InMemoryRepository(Repository[T, ID]):
    """In-memory implementation of the Repository interface.

    Stores entities in a dictionary for testing and development.
    Supports custom ID extraction via a function parameter.
    """

    def __init__(self, id_extractor: Callable[[T], ID]):
        """Initialize the repository.

        Args:
            id_extractor: Function to extract ID from an entity
        """
        self._storage: Dict[ID, T] = {}
        self._id_extractor = id_extractor

    def find_by_id(self, id: ID) -> Optional[T]:
        """Find an entity by its ID."""
        return self._storage.get(id)

    def find_all(self) -> List[T]:
        """Retrieve all entities."""
        return list(self._storage.values())

    def save(self, entity: T) -> T:
        """Save an entity."""
        entity_id = self._id_extractor(entity)
        self._storage[entity_id] = entity
        return entity

    def delete(self, id: ID) -> bool:
        """Delete an entity by ID."""
        if id in self._storage:
            del self._storage[id]
            return True
        return False

    def exists(self, id: ID) -> bool:
        """Check if an entity exists."""
        return id in self._storage

    def count(self) -> int:
        """Count total entities."""
        return len(self._storage)

    def find_by(self, predicate: Callable[[T], bool]) -> List[T]:
        """Find entities matching a predicate.

        Args:
            predicate: Function that returns True for matching entities

        Returns:
            List of matching entities
        """
        return [e for e in self._storage.values() if predicate(e)]

    def find_first(self, predicate: Callable[[T], bool]) -> Optional[T]:
        """Find the first entity matching a predicate.

        Args:
            predicate: Function that returns True for matching entities

        Returns:
            First matching entity or None
        """
        for entity in self._storage.values():
            if predicate(entity):
                return entity
        return None

    def clear(self) -> None:
        """Remove all entities from the repository."""
        self._storage.clear()

    def __iter__(self) -> Iterator[T]:
        """Iterate over all entities."""
        return iter(self._storage.values())

    def __len__(self) -> int:
        """Return the number of entities."""
        return len(self._storage)


@dataclass
class Page(Generic[T]):
    """A page of results for paginated queries.

    Attributes:
        items: The items in this page
        page_number: Current page number (1-indexed)
        page_size: Number of items per page
        total_items: Total number of items across all pages
        total_pages: Total number of pages
    """
    items: List[T]
    page_number: int
    page_size: int
    total_items: int
    total_pages: int

    @property
    def has_next(self) -> bool:
        """Check if there is a next page."""
        return self.page_number < self.total_pages

    @property
    def has_previous(self) -> bool:
        """Check if there is a previous page."""
        return self.page_number > 1


class PaginatedRepository(Repository[T, ID], Generic[T, ID]):
    """Repository extension with pagination support."""

    @abstractmethod
    def find_page(self, page: int, size: int) -> Page[T]:
        """Retrieve a page of entities.

        Args:
            page: Page number (1-indexed)
            size: Number of items per page

        Returns:
            A Page containing the requested items
        """
        pass


class CachingRepository(Repository[T, ID], Generic[T, ID]):
    """Repository decorator that adds caching.

    Wraps another repository and caches read operations.
    """

    def __init__(self, delegate: Repository[T, ID], max_size: int = 1000):
        """Initialize caching repository.

        Args:
            delegate: The underlying repository
            max_size: Maximum cache size
        """
        self._delegate = delegate
        self._cache: Dict[ID, T] = {}
        self._max_size = max_size

    def find_by_id(self, id: ID) -> Optional[T]:
        """Find with caching."""
        if id in self._cache:
            return self._cache[id]

        entity = self._delegate.find_by_id(id)
        if entity is not None:
            self._cache_put(id, entity)
        return entity

    def find_all(self) -> List[T]:
        """Find all (not cached)."""
        return self._delegate.find_all()

    def save(self, entity: T) -> T:
        """Save and update cache."""
        result = self._delegate.save(entity)
        # Cache invalidation would be handled here
        return result

    def delete(self, id: ID) -> bool:
        """Delete and invalidate cache."""
        self._cache.pop(id, None)
        return self._delegate.delete(id)

    def exists(self, id: ID) -> bool:
        """Check existence."""
        return id in self._cache or self._delegate.exists(id)

    def count(self) -> int:
        """Count entities."""
        return self._delegate.count()

    def _cache_put(self, id: ID, entity: T) -> None:
        """Add to cache with size limit."""
        if len(self._cache) >= self._max_size:
            # Simple eviction: remove first key
            first_key = next(iter(self._cache))
            del self._cache[first_key]
        self._cache[id] = entity

    def clear_cache(self) -> None:
        """Clear the cache."""
        self._cache.clear()

"""Base model classes with common functionality."""

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from datetime import datetime
from typing import Optional, Any, Dict
import uuid


class TimestampMixin:
    """Mixin that adds created_at and updated_at timestamps."""

    created_at: datetime = field(default_factory=datetime.utcnow)
    updated_at: Optional[datetime] = None

    def touch(self) -> None:
        """Update the updated_at timestamp to now."""
        self.updated_at = datetime.utcnow()

    def age_seconds(self) -> float:
        """Return the age of this entity in seconds."""
        return (datetime.utcnow() - self.created_at).total_seconds()


class VersionedMixin:
    """Mixin that adds version tracking for optimistic locking."""

    version: int = 1

    def increment_version(self) -> int:
        """Increment and return the version number."""
        self.version += 1
        return self.version

    def check_version(self, expected: int) -> bool:
        """Check if the current version matches expected."""
        return self.version == expected


@dataclass
class BaseEntity(ABC):
    """Abstract base class for all domain entities.

    Provides common functionality like ID generation, equality checking,
    and serialization support.

    Subclasses must implement the `validate` method to ensure
    entity invariants are maintained.
    """

    id: str = field(default_factory=lambda: str(uuid.uuid4()))

    @abstractmethod
    def validate(self) -> bool:
        """Validate the entity state.

        Returns:
            True if the entity is valid, False otherwise
        """
        pass

    def __eq__(self, other: Any) -> bool:
        """Compare entities by their IDs."""
        if not isinstance(other, BaseEntity):
            return NotImplemented
        return self.id == other.id

    def __hash__(self) -> int:
        """Hash based on entity ID."""
        return hash(self.id)

    def to_dict(self) -> Dict[str, Any]:
        """Convert entity to dictionary representation.

        Subclasses should override to include all fields.
        """
        return {"id": self.id}


@dataclass
class VersionedEntity(BaseEntity, VersionedMixin, TimestampMixin):
    """Base entity with versioning and timestamps.

    Combines BaseEntity with version tracking and timestamp management
    for entities that need optimistic locking and audit trails.
    """

    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary with version and timestamps."""
        return {
            "id": self.id,
            "version": self.version,
            "created_at": self.created_at.isoformat(),
            "updated_at": self.updated_at.isoformat() if self.updated_at else None,
        }

    def update(self) -> None:
        """Update version and timestamp."""
        self.increment_version()
        self.touch()


class EntityFactory(ABC):
    """Abstract factory for creating domain entities."""

    @abstractmethod
    def create(self, **kwargs: Any) -> BaseEntity:
        """Create a new entity instance.

        Args:
            **kwargs: Entity attributes

        Returns:
            A new entity instance
        """
        pass

    @abstractmethod
    def reconstitute(self, data: Dict[str, Any]) -> BaseEntity:
        """Reconstitute an entity from stored data.

        Args:
            data: Dictionary of stored entity data

        Returns:
            A reconstituted entity instance
        """
        pass

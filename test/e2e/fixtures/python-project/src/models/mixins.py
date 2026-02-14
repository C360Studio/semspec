"""Mixin classes for extending entity functionality."""

import json
from abc import ABC, abstractmethod
from dataclasses import asdict, is_dataclass
from typing import Any, Dict, List, Optional, Type, TypeVar
from datetime import datetime


T = TypeVar('T', bound='SerializableMixin')


class SerializableMixin:
    """Mixin providing JSON serialization capabilities.

    Adds methods to convert entities to and from JSON format,
    with support for custom serializers for complex types.
    """

    _serializers: Dict[Type, callable] = {
        datetime: lambda dt: dt.isoformat(),
    }

    def to_json(self, indent: Optional[int] = None) -> str:
        """Serialize the entity to a JSON string.

        Args:
            indent: JSON indentation level (None for compact)

        Returns:
            JSON string representation
        """
        data = self._serialize()
        return json.dumps(data, indent=indent, default=self._json_default)

    @classmethod
    def from_json(cls: Type[T], json_str: str) -> T:
        """Deserialize an entity from a JSON string.

        Args:
            json_str: JSON string to deserialize

        Returns:
            A new instance of the class
        """
        data = json.loads(json_str)
        return cls._deserialize(data)

    def _serialize(self) -> Dict[str, Any]:
        """Convert to dictionary for serialization."""
        if is_dataclass(self):
            return asdict(self)
        return self.__dict__.copy()

    @classmethod
    def _deserialize(cls: Type[T], data: Dict[str, Any]) -> T:
        """Create instance from dictionary."""
        return cls(**data)

    def _json_default(self, obj: Any) -> Any:
        """Handle non-serializable types."""
        for type_, serializer in self._serializers.items():
            if isinstance(obj, type_):
                return serializer(obj)
        raise TypeError(f"Object of type {type(obj).__name__} is not JSON serializable")


class ValidatableMixin(ABC):
    """Mixin providing validation capabilities.

    Adds declarative validation rules and methods for
    checking entity validity.
    """

    _validation_errors: List[str]

    def __init_subclass__(cls, **kwargs: Any) -> None:
        """Initialize validation error list for subclasses."""
        super().__init_subclass__(**kwargs)
        cls._validation_errors = []

    @property
    def validation_errors(self) -> List[str]:
        """Get list of validation errors from last validation."""
        return self._validation_errors.copy()

    @property
    def is_valid(self) -> bool:
        """Check if entity passes all validations."""
        self._validation_errors = []
        self._run_validations()
        return len(self._validation_errors) == 0

    def add_error(self, error: str) -> None:
        """Add a validation error."""
        self._validation_errors.append(error)

    @abstractmethod
    def _run_validations(self) -> None:
        """Run all validations and populate error list.

        Subclasses should override this to add validation checks.
        Use add_error() to record validation failures.
        """
        pass

    def validate_required(self, field: str, value: Any) -> bool:
        """Validate that a required field has a value.

        Args:
            field: Name of the field
            value: Field value to check

        Returns:
            True if valid, False otherwise
        """
        if value is None or (isinstance(value, str) and not value.strip()):
            self.add_error(f"{field} is required")
            return False
        return True

    def validate_length(
        self,
        field: str,
        value: str,
        min_len: int = 0,
        max_len: int = float('inf')
    ) -> bool:
        """Validate string length is within bounds.

        Args:
            field: Name of the field
            value: String value to check
            min_len: Minimum allowed length
            max_len: Maximum allowed length

        Returns:
            True if valid, False otherwise
        """
        if value is None:
            return True  # Let validate_required handle None

        length = len(value)
        if length < min_len:
            self.add_error(f"{field} must be at least {min_len} characters")
            return False
        if length > max_len:
            self.add_error(f"{field} must be at most {max_len} characters")
            return False
        return True

    def validate_range(
        self,
        field: str,
        value: float,
        min_val: Optional[float] = None,
        max_val: Optional[float] = None
    ) -> bool:
        """Validate numeric value is within range.

        Args:
            field: Name of the field
            value: Numeric value to check
            min_val: Minimum allowed value (inclusive)
            max_val: Maximum allowed value (inclusive)

        Returns:
            True if valid, False otherwise
        """
        if value is None:
            return True

        if min_val is not None and value < min_val:
            self.add_error(f"{field} must be at least {min_val}")
            return False
        if max_val is not None and value > max_val:
            self.add_error(f"{field} must be at most {max_val}")
            return False
        return True


class ComparableMixin:
    """Mixin providing comparison operators based on a key field."""

    @property
    @abstractmethod
    def _comparison_key(self) -> Any:
        """Return the key to use for comparisons."""
        pass

    def __lt__(self, other: 'ComparableMixin') -> bool:
        if not isinstance(other, ComparableMixin):
            return NotImplemented
        return self._comparison_key < other._comparison_key

    def __le__(self, other: 'ComparableMixin') -> bool:
        if not isinstance(other, ComparableMixin):
            return NotImplemented
        return self._comparison_key <= other._comparison_key

    def __gt__(self, other: 'ComparableMixin') -> bool:
        if not isinstance(other, ComparableMixin):
            return NotImplemented
        return self._comparison_key > other._comparison_key

    def __ge__(self, other: 'ComparableMixin') -> bool:
        if not isinstance(other, ComparableMixin):
            return NotImplemented
        return self._comparison_key >= other._comparison_key

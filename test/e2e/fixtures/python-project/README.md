# Python Test Project

A comprehensive test fixture for verifying Python AST extraction.

## Structure

```
src/
├── __init__.py           # Package initialization
├── auth/                 # Authentication module
│   ├── __init__.py
│   ├── models.py         # User, Token, AuthResult dataclasses
│   ├── service.py        # AuthService with async methods
│   └── utils.py          # Validation utilities
├── models/               # Base model patterns
│   ├── __init__.py
│   ├── base.py           # Abstract base entities
│   └── mixins.py         # Serializable, Validatable mixins
├── generics/             # Generic type patterns
│   ├── __init__.py
│   └── repository.py     # Repository[T, ID] pattern
└── patterns/             # Design patterns
    ├── __init__.py
    ├── decorators.py     # @timer, @retry, @deprecated, @singleton
    ├── async_patterns.py # AsyncWorker, TaskQueue
    └── protocols.py      # Comparable, Hashable, etc.
```

## Features Tested

- **Classes**: Regular classes, inheritance, dataclasses
- **Functions**: Regular, async, with decorators
- **Type Hints**: Parameters, returns, generics (TypeVar, Generic)
- **Protocols**: Structural typing with @runtime_checkable
- **Decorators**: Function and class decorators
- **Visibility**: Public vs private (_underscore prefix)
- **Docstrings**: Module, class, and function documentation
- **Constants**: ALL_CAPS naming convention
- **Imports**: Regular, from imports, aliased

## Expected Entities

The AST parser should extract:
- ~15 classes (User, Token, AuthService, Repository, etc.)
- ~50 functions/methods
- ~10 constants (MAX_SIZE, PASSWORD_MIN_LENGTH, etc.)
- ~5 enums (UserRole, TokenType, TaskStatus)

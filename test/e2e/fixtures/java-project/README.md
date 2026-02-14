# Java Test Project

A comprehensive test fixture for verifying Java AST extraction.

## Structure

```
src/main/java/com/example/
├── auth/                    # Authentication module
│   ├── User.java            # User entity class
│   ├── UserRole.java        # Role enum with fields
│   ├── Token.java           # Immutable token class
│   ├── TokenType.java       # Token type enum
│   ├── AuthResult.java      # Result record (Java 14+)
│   ├── Authenticator.java   # Authentication interface
│   └── AuthService.java     # Service implementation
├── models/                  # Base model patterns
│   ├── BaseEntity.java      # Abstract generic entity
│   ├── Status.java          # Status enum with methods
│   └── AuditInfo.java       # Audit tracking (nested class)
├── generics/                # Generic type patterns
│   ├── Repository.java      # Generic repository interface
│   ├── InMemoryRepository.java  # Repository implementation
│   └── Page.java            # Pagination record
└── annotations/             # Custom annotations
    ├── Logged.java          # Logging annotation
    ├── Validated.java       # Validation annotation
    ├── Cached.java          # Caching annotation
    └── Retry.java           # Retry annotation
```

## Features Tested

- **Classes**: Regular classes, abstract classes, final classes
- **Interfaces**: Interface definitions with default methods
- **Enums**: Enums with fields, methods, and constructors
- **Records**: Java 14+ record types (AuthResult, Page)
- **Generics**: Generic classes and interfaces (Repository<T, ID>)
- **Annotations**: Custom annotation definitions
- **Inner Classes**: Nested static classes (AuditInfo.ChangeRecord)
- **Visibility**: public, private, protected modifiers
- **Javadoc**: Class, method, and field documentation

## Expected Entities

The AST parser should extract:
- ~10 classes (User, Token, AuthService, BaseEntity, etc.)
- ~5 interfaces (Repository, Authenticator)
- ~5 enums (UserRole, TokenType, Status)
- ~2 records (AuthResult, Page)
- ~4 annotations (Logged, Validated, Cached, Retry)
- ~50 methods
- ~20 fields

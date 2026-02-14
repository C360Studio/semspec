# JavaScript Test Project

A comprehensive test fixture for verifying JavaScript AST extraction.

## Structure

```
src/
├── index.js                 # ES6 module exports entry
├── auth/
│   ├── auth.service.js      # ES6 class with #private fields
│   ├── auth.utils.js        # Utility functions
│   └── auth.types.js        # JSDoc type definitions
├── legacy/
│   ├── commonjs.js          # require/module.exports pattern
│   └── prototype.js         # Prototype-based inheritance
└── patterns/
    ├── async.js             # async/await, generators
    ├── closures.js          # Closure patterns, HOFs
    └── functional.js        # Functional utilities
```

## Features Tested

- **ES6 Classes**: Class declarations with #private fields
- **Functions**: Named, arrow, async, generator functions
- **Constants**: const declarations
- **Variables**: let/var declarations
- **ES6 Imports**: import/export statements
- **CommonJS**: require/module.exports pattern
- **JSDoc**: Type annotations via comments
- **Closures**: Encapsulated state patterns
- **Async**: async/await, Promise patterns

## Expected Entities

The AST parser should extract:
- ~5 classes (AuthService, AuthResult, SimpleCache, EventEmitter, etc.)
- ~50 functions (arrow and regular)
- ~10 constants
- Multiple import statements

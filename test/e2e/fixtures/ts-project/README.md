# Test Project

A simple TypeScript project for E2E testing of AST processors.

## Structure

- `src/index.ts` - Application entry point
- `src/auth/` - Authentication module
  - `auth.service.ts` - Auth service class
  - `auth.types.ts` - Type definitions
  - `auth.test.ts` - Unit tests
- `src/types/` - Shared types
  - `user.ts` - User type extensions

## Running

```bash
npm run build
node dist/index.js
```

## Testing

```bash
npm test
```

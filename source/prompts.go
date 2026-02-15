package source

// analysisSystemPrompt is the system prompt for document analysis.
const analysisSystemPrompt = `You are a document metadata extractor. Analyze documents and extract structured metadata.

Always respond with valid JSON. Do not include any text outside the JSON object.`

// analysisUserPrompt is the user prompt template for document analysis.
// The %s placeholder is replaced with the document content.
const analysisUserPrompt = `Analyze this document and extract the following metadata:

1. **category**: Classify the document type. Choose exactly one:
   - "sop" - Standard Operating Procedure: defines rules, guidelines, or processes that must be followed
   - "spec" - Technical Specification: defines system behavior, requirements, or interfaces
   - "datasheet" - Data Documentation: describes data structures, formats, or schemas
   - "reference" - Reference Material: provides background information, explanations, or tutorials
   - "api" - API Documentation: describes endpoints, request/response formats, or integration guides

2. **applies_to**: File patterns this document applies to (for SOPs and specs).
   - Use glob patterns like "*.go", "handlers/*.ts", "**/*.py"
   - Leave empty array if document is general reference material
   - Examples: ["*.go"] for Go-specific rules, ["src/auth/*"] for auth-related code

3. **severity**: For SOPs only, how critical is compliance:
   - "error" - Violations MUST be fixed (MUST, REQUIRED, SHALL)
   - "warning" - Violations SHOULD be addressed (SHOULD, RECOMMENDED)
   - "info" - Advisory only (MAY, OPTIONAL)
   - Leave empty for non-SOP documents

4. **scope**: When does this document apply? Infer from content:
   - "plan" - Applies during planning/design phase (architecture decisions, migration planning,
     scope validation, breaking changes policy, "before implementation" language)
   - "code" - Applies during implementation/review (coding standards, error handling,
     naming conventions, "when writing code" language)
   - "all" - Applies to both phases (security policies, compliance, universal standards)

   Inference signals:
   - "before making changes", "design decisions require", "migration strategy" → plan
   - "when implementing", "all code must", "functions should" → code
   - "security requirements", "compliance", "all changes must" → all

5. **summary**: One-sentence description of what this document covers.

6. **requirements**: Key rules or requirements as a list. For SOPs, these are the checkable items.
   - Keep each requirement concise and actionable
   - Maximum 10 requirements
   - Leave empty if document has no specific requirements

7. **domain**: Primary semantic domain(s) this document covers. Choose from:
   - "auth" - Authentication, authorization, sessions, tokens
   - "database" - Database operations, migrations, queries, transactions
   - "api" - API design, endpoints, request/response handling
   - "security" - Security practices, cryptography, secrets management
   - "testing" - Testing practices, test organization, coverage
   - "logging" - Logging, observability, metrics, tracing
   - "messaging" - Message queues, event systems, pub/sub patterns
   - "deployment" - CI/CD, infrastructure, containerization
   - "performance" - Optimization, caching, benchmarking
   - "error-handling" - Error handling, recovery, resilience patterns
   - "validation" - Input validation, data sanitization
   - "config" - Configuration management, environment handling

   Can have multiple domains (e.g., ["auth", "security"]). Infer from content focus.
   Leave empty array if no clear domain applies.

8. **related_domains**: Domains conceptually related to this document.
   - Example: an auth doc might relate to ["security", "validation"]
   - A database doc might relate to ["error-handling", "performance"]
   - Leave empty array if no clear relationships

9. **keywords**: Key semantic terms for fuzzy matching (max 10).
   - Technical terms specific to this document
   - Used for search when file patterns and domains don't match
   - Examples: ["token refresh", "expiration", "OAuth", "JWT"] for auth docs
   - Leave empty array if document is generic

Document to analyze:
---
%s
---

Respond with JSON only:
{"category":"...","applies_to":[...],"severity":"...","scope":"...","summary":"...","requirements":[...],"domain":[...],"related_domains":[...],"keywords":[...]}`

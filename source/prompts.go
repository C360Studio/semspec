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

4. **summary**: One-sentence description of what this document covers.

5. **requirements**: Key rules or requirements as a list. For SOPs, these are the checkable items.
   - Keep each requirement concise and actionable
   - Maximum 10 requirements
   - Leave empty if document has no specific requirements

Document to analyze:
---
%s
---

Respond with JSON only:
{"category":"...","applies_to":[...],"severity":"...","summary":"...","requirements":[...]}`

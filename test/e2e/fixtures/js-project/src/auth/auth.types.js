/**
 * Type definitions using JSDoc for JavaScript type checking.
 * @module auth/types
 */

/**
 * User object representing an authenticated user.
 *
 * @typedef {Object} User
 * @property {string} id - Unique user identifier
 * @property {string} email - User's email address
 * @property {string} name - User's display name
 * @property {UserRole} role - User's authorization role
 * @property {boolean} active - Whether the account is active
 * @property {Date} createdAt - Account creation timestamp
 */

/**
 * User roles for access control.
 *
 * @typedef {'admin'|'user'|'guest'} UserRole
 */

/**
 * Token object representing an authentication token.
 *
 * @typedef {Object} Token
 * @property {string} value - The token string
 * @property {TokenType} type - Type of token
 * @property {string} userId - ID of the token owner
 * @property {Date} expiresAt - Token expiration time
 * @property {string[]} scopes - Permission scopes
 */

/**
 * Token types.
 *
 * @typedef {'access'|'refresh'|'api_key'} TokenType
 */

/**
 * Authentication result.
 *
 * @typedef {Object} AuthResultSuccess
 * @property {true} success - Authentication succeeded
 * @property {User} user - The authenticated user
 * @property {Token} accessToken - Access token
 * @property {Token|null} refreshToken - Refresh token
 * @property {null} error - No error
 */

/**
 * Failed authentication result.
 *
 * @typedef {Object} AuthResultFailure
 * @property {false} success - Authentication failed
 * @property {null} user - No user
 * @property {null} accessToken - No token
 * @property {null} refreshToken - No token
 * @property {string} error - Error message
 */

/**
 * Authentication result (success or failure).
 *
 * @typedef {AuthResultSuccess|AuthResultFailure} AuthResult
 */

/**
 * Configuration for the auth service.
 *
 * @typedef {Object} AuthConfig
 * @property {number} [tokenExpiry=86400000] - Token expiry in milliseconds
 * @property {number} [maxAttempts=5] - Maximum failed login attempts
 * @property {boolean} [requireEmailVerification=false] - Require email verification
 */

/**
 * Credentials for authentication.
 *
 * @typedef {Object} Credentials
 * @property {string} email - User's email
 * @property {string} password - User's password
 * @property {string[]} [scopes] - Requested scopes
 */

/**
 * Registration data for new users.
 *
 * @typedef {Object} RegistrationData
 * @property {string} email - User's email
 * @property {string} password - User's password
 * @property {string} name - User's display name
 * @property {UserRole} [role='user'] - User's role
 */

// Export empty object to make this a module
export default {};

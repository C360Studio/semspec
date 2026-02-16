/**
 * Authentication service module.
 * @module auth/service
 */

import { validateEmail, generateSecureToken, hashPassword } from './auth.utils.js';

/**
 * User roles for access control.
 * @readonly
 * @enum {string}
 */
export const UserRole = {
    ADMIN: 'admin',
    USER: 'user',
    GUEST: 'guest',
};

/**
 * Token types.
 * @readonly
 * @enum {string}
 */
export const TokenType = {
    ACCESS: 'access',
    REFRESH: 'refresh',
    API_KEY: 'api_key',
};

/**
 * Authentication service for managing user sessions.
 *
 * @class
 * @example
 * const auth = new AuthService();
 * const result = await auth.authenticate('user@example.com', 'password');
 */
export class AuthService {
    /** @type {Map<string, Object>} */
    #users = new Map();

    /** @type {Map<string, string>} */
    #passwords = new Map();

    /** @type {Map<string, Object>} */
    #tokens = new Map();

    /** @type {Map<string, number>} */
    #failedAttempts = new Map();

    /** @type {number} */
    #tokenExpiry;

    /** @type {number} */
    #maxAttempts;

    /**
     * Create an authentication service.
     *
     * @param {Object} config - Configuration options
     * @param {number} [config.tokenExpiry=86400000] - Token expiry in ms
     * @param {number} [config.maxAttempts=5] - Max failed login attempts
     */
    constructor(config = {}) {
        this.#tokenExpiry = config.tokenExpiry || 24 * 60 * 60 * 1000;
        this.#maxAttempts = config.maxAttempts || 5;
    }

    /**
     * Authenticate a user with email and password.
     *
     * @async
     * @param {string} email - User's email address
     * @param {string} password - User's password
     * @param {string[]} [scopes=[]] - Requested permission scopes
     * @returns {Promise<AuthResult>} Authentication result
     * @throws {AccountLockedError} If account is locked
     */
    async authenticate(email, password, scopes = []) {
        // Check for account lockout
        if (this.#isLocked(email)) {
            throw new AccountLockedError(`Account ${email} is locked`);
        }

        // Validate credentials
        const user = await this.#validateCredentials(email, password);
        if (!user) {
            this.#recordFailedAttempt(email);
            return AuthResult.failure('Invalid credentials');
        }

        // Generate tokens
        const accessToken = this.#generateToken(user.id, TokenType.ACCESS, scopes);
        const refreshToken = this.#generateToken(user.id, TokenType.REFRESH, ['refresh']);

        // Clear failed attempts on success
        this.#failedAttempts.delete(email);

        return AuthResult.success(user, accessToken, refreshToken);
    }

    /**
     * Refresh an access token using a refresh token.
     *
     * @async
     * @param {string} refreshTokenValue - The refresh token
     * @returns {Promise<AuthResult>} New authentication result
     */
    async refreshToken(refreshTokenValue) {
        const token = this.#tokens.get(refreshTokenValue);
        if (!token) {
            return AuthResult.failure('Invalid refresh token');
        }

        if (this.#isTokenExpired(token)) {
            this.#tokens.delete(refreshTokenValue);
            return AuthResult.failure('Refresh token has expired');
        }

        if (token.type !== TokenType.REFRESH) {
            return AuthResult.failure('Not a refresh token');
        }

        const user = this.#findUserById(token.userId);
        if (!user) {
            return AuthResult.failure('User not found');
        }

        const newAccessToken = this.#generateToken(user.id, TokenType.ACCESS, token.scopes);
        return AuthResult.success(user, newAccessToken, null);
    }

    /**
     * Validate an access token and return the user.
     *
     * @async
     * @param {string} tokenValue - The access token
     * @returns {Promise<Object|null>} The user if valid, null otherwise
     */
    async validateToken(tokenValue) {
        const token = this.#tokens.get(tokenValue);
        if (!token || this.#isTokenExpired(token)) {
            return null;
        }
        return this.#findUserById(token.userId);
    }

    /**
     * Revoke a token (logout).
     *
     * @param {string} tokenValue - The token to revoke
     * @returns {boolean} True if revoked
     */
    logout(tokenValue) {
        return this.#tokens.delete(tokenValue);
    }

    /**
     * Register a new user.
     *
     * @param {string} email - User's email
     * @param {string} password - User's password
     * @param {string} name - User's name
     * @param {string} [role=UserRole.USER] - User's role
     * @returns {Object} The created user
     * @throws {Error} If email is already registered
     */
    registerUser(email, password, name, role = UserRole.USER) {
        const [isValid, error] = validateEmail(email);
        if (!isValid) {
            throw new Error(error);
        }

        if (this.#users.has(email)) {
            throw new Error(`Email already registered: ${email}`);
        }

        const userId = generateSecureToken();
        const user = {
            id: userId,
            email,
            name,
            role,
            active: true,
            createdAt: new Date(),
        };

        this.#users.set(email, user);
        this.#passwords.set(email, hashPassword(password));

        return user;
    }

    /**
     * Check if an account is locked.
     * @private
     */
    #isLocked(email) {
        return (this.#failedAttempts.get(email) || 0) >= this.#maxAttempts;
    }

    /**
     * Record a failed login attempt.
     * @private
     */
    #recordFailedAttempt(email) {
        const current = this.#failedAttempts.get(email) || 0;
        this.#failedAttempts.set(email, current + 1);
    }

    /**
     * Validate credentials.
     * @private
     */
    async #validateCredentials(email, password) {
        const user = this.#users.get(email);
        if (!user) return null;

        const storedHash = this.#passwords.get(email);
        if (!storedHash || storedHash !== hashPassword(password)) {
            return null;
        }

        if (!user.active) return null;

        return user;
    }

    /**
     * Find user by ID.
     * @private
     */
    #findUserById(userId) {
        for (const user of this.#users.values()) {
            if (user.id === userId) return user;
        }
        return null;
    }

    /**
     * Generate a token.
     * @private
     */
    #generateToken(userId, type, scopes) {
        const value = generateSecureToken();
        const token = {
            value,
            type,
            userId,
            expiresAt: new Date(Date.now() + this.#tokenExpiry),
            scopes,
        };
        this.#tokens.set(value, token);
        return token;
    }

    /**
     * Check if token is expired.
     * @private
     */
    #isTokenExpired(token) {
        return new Date() > token.expiresAt;
    }
}

/**
 * Authentication result.
 * @class
 */
export class AuthResult {
    /**
     * @param {boolean} success
     * @param {Object|null} user
     * @param {Object|null} accessToken
     * @param {Object|null} refreshToken
     * @param {string|null} error
     */
    constructor(success, user, accessToken, refreshToken, error) {
        this.success = success;
        this.user = user;
        this.accessToken = accessToken;
        this.refreshToken = refreshToken;
        this.error = error;
    }

    /**
     * Create a successful result.
     * @static
     */
    static success(user, accessToken, refreshToken) {
        return new AuthResult(true, user, accessToken, refreshToken, null);
    }

    /**
     * Create a failed result.
     * @static
     */
    static failure(error) {
        return new AuthResult(false, null, null, null, error);
    }
}

/**
 * Error thrown when account is locked.
 * @class
 * @extends Error
 */
export class AccountLockedError extends Error {
    constructor(message) {
        super(message);
        this.name = 'AccountLockedError';
    }
}

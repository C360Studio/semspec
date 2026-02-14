/**
 * Authentication utility functions.
 * @module auth/utils
 */

import crypto from 'crypto';

/**
 * Minimum password length requirement.
 * @constant {number}
 */
export const PASSWORD_MIN_LENGTH = 8;

/**
 * Maximum password length.
 * @constant {number}
 */
export const PASSWORD_MAX_LENGTH = 128;

/**
 * Email validation regex pattern.
 * @constant {RegExp}
 */
export const EMAIL_PATTERN = /^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/;

/**
 * Validate an email address format.
 *
 * @param {string} email - The email to validate
 * @returns {[boolean, string|null]} Tuple of [isValid, errorMessage]
 */
export function validateEmail(email) {
    if (!email) {
        return [false, 'Email is required'];
    }

    if (typeof email !== 'string') {
        return [false, 'Email must be a string'];
    }

    const trimmed = email.trim();
    if (trimmed.length > 254) {
        return [false, 'Email too long'];
    }

    if (!EMAIL_PATTERN.test(trimmed)) {
        return [false, 'Invalid email format'];
    }

    return [true, null];
}

/**
 * Validate a password meets requirements.
 *
 * @param {string} password - The password to validate
 * @param {Object} [options] - Validation options
 * @param {boolean} [options.requireUppercase=true] - Require uppercase letter
 * @param {boolean} [options.requireLowercase=true] - Require lowercase letter
 * @param {boolean} [options.requireDigit=true] - Require digit
 * @param {boolean} [options.requireSpecial=true] - Require special character
 * @returns {[boolean, string|null]} Tuple of [isValid, errorMessage]
 */
export function validatePassword(password, options = {}) {
    const {
        requireUppercase = true,
        requireLowercase = true,
        requireDigit = true,
        requireSpecial = true,
    } = options;

    if (!password) {
        return [false, 'Password is required'];
    }

    if (password.length < PASSWORD_MIN_LENGTH) {
        return [false, `Password must be at least ${PASSWORD_MIN_LENGTH} characters`];
    }

    if (password.length > PASSWORD_MAX_LENGTH) {
        return [false, `Password must be at most ${PASSWORD_MAX_LENGTH} characters`];
    }

    if (requireUppercase && !/[A-Z]/.test(password)) {
        return [false, 'Password must contain an uppercase letter'];
    }

    if (requireLowercase && !/[a-z]/.test(password)) {
        return [false, 'Password must contain a lowercase letter'];
    }

    if (requireDigit && !/\d/.test(password)) {
        return [false, 'Password must contain a digit'];
    }

    if (requireSpecial && !/[!@#$%^&*(),.?":{}|<>]/.test(password)) {
        return [false, 'Password must contain a special character'];
    }

    return [true, null];
}

/**
 * Generate a cryptographically secure random token.
 *
 * @param {number} [length=32] - Length in bytes
 * @returns {string} Hex-encoded token
 */
export function generateSecureToken(length = 32) {
    return crypto.randomBytes(length).toString('hex');
}

/**
 * Generate a formatted API key.
 *
 * @returns {string} API key in format 'sk_xxxx_xxxx_xxxx_xxxx'
 */
export function generateApiKey() {
    const parts = Array.from({ length: 4 }, () =>
        crypto.randomBytes(4).toString('hex')
    );
    return `sk_${parts.join('_')}`;
}

/**
 * Hash a password using SHA-256.
 *
 * @param {string} password - The password to hash
 * @returns {string} Base64-encoded hash
 */
export function hashPassword(password) {
    return crypto
        .createHash('sha256')
        .update(password)
        .digest('base64');
}

/**
 * Compare two strings in constant time.
 *
 * @param {string} a - First string
 * @param {string} b - Second string
 * @returns {boolean} True if equal
 */
export function constantTimeCompare(a, b) {
    if (typeof a !== 'string' || typeof b !== 'string') {
        return false;
    }
    return crypto.timingSafeEqual(Buffer.from(a), Buffer.from(b));
}

/**
 * Normalize an email address for comparison.
 *
 * @param {string} email - The email to normalize
 * @returns {string} Normalized email
 */
export function normalizeEmail(email) {
    if (!email || typeof email !== 'string') {
        return '';
    }

    let normalized = email.toLowerCase().trim();
    const [local, domain] = normalized.split('@');

    if (!domain) {
        return normalized;
    }

    // Remove plus addressing
    const cleanLocal = local.split('+')[0];

    // Remove dots for Gmail
    if (domain === 'gmail.com' || domain === 'googlemail.com') {
        return `${cleanLocal.replace(/\./g, '')}@${domain}`;
    }

    return `${cleanLocal}@${domain}`;
}

/**
 * Mask an email address for display.
 *
 * @param {string} email - The email to mask
 * @returns {string} Masked email (e.g., "j***@example.com")
 */
export function maskEmail(email) {
    if (!email || !email.includes('@')) {
        return '***';
    }

    const [local, domain] = email.split('@');
    if (local.length <= 1) {
        return `*@${domain}`;
    }

    return `${local[0]}***@${domain}`;
}

/**
 * JavaScript Test Project
 *
 * Entry point demonstrating ES6 module exports.
 * @module testproject
 */

import { AuthService } from './auth/auth.service.js';
import { validateEmail, validatePassword } from './auth/auth.utils.js';

// Re-export for convenience
export { AuthService };
export { validateEmail, validatePassword };

/**
 * Application version
 * @constant {string}
 */
export const VERSION = '1.0.0';

/**
 * Default configuration
 * @constant {Object}
 */
export const DEFAULT_CONFIG = {
    tokenExpiry: 24 * 60 * 60 * 1000, // 24 hours
    maxLoginAttempts: 5,
    sessionTimeout: 30 * 60 * 1000, // 30 minutes
};

/**
 * Create a configured auth service instance.
 *
 * @param {Object} config - Configuration options
 * @param {number} [config.tokenExpiry] - Token expiry in ms
 * @param {number} [config.maxAttempts] - Max login attempts
 * @returns {AuthService} Configured auth service
 */
export function createAuthService(config = {}) {
    const mergedConfig = { ...DEFAULT_CONFIG, ...config };
    return new AuthService(mergedConfig);
}

/**
 * Initialize the application.
 *
 * @async
 * @param {Object} options - Initialization options
 * @returns {Promise<Object>} Initialized application context
 */
export async function initialize(options = {}) {
    const authService = createAuthService(options.auth);

    return {
        version: VERSION,
        auth: authService,
        config: { ...DEFAULT_CONFIG, ...options },
    };
}

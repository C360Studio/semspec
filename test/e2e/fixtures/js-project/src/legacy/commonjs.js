/**
 * Legacy CommonJS module for backwards compatibility testing.
 *
 * This module uses require/module.exports pattern instead of ES6 imports.
 */

'use strict';

const crypto = require('crypto');

/**
 * Maximum cache size.
 * @constant {number}
 */
const MAX_CACHE_SIZE = 1000;

/**
 * Default TTL in milliseconds.
 * @constant {number}
 */
const DEFAULT_TTL = 60000;

/**
 * Simple in-memory cache with TTL support.
 *
 * @class
 * @example
 * const cache = new SimpleCache({ ttl: 5000 });
 * cache.set('key', 'value');
 * cache.get('key'); // 'value'
 */
class SimpleCache {
    /**
     * Create a cache instance.
     *
     * @param {Object} options - Cache options
     * @param {number} [options.ttl=60000] - Default TTL in ms
     * @param {number} [options.maxSize=1000] - Max entries
     */
    constructor(options = {}) {
        this.ttl = options.ttl || DEFAULT_TTL;
        this.maxSize = options.maxSize || MAX_CACHE_SIZE;
        this.store = new Map();
    }

    /**
     * Get a value from the cache.
     *
     * @param {string} key - Cache key
     * @returns {*} The cached value or undefined
     */
    get(key) {
        const entry = this.store.get(key);
        if (!entry) {
            return undefined;
        }

        if (Date.now() > entry.expiresAt) {
            this.store.delete(key);
            return undefined;
        }

        return entry.value;
    }

    /**
     * Set a value in the cache.
     *
     * @param {string} key - Cache key
     * @param {*} value - Value to cache
     * @param {number} [ttl] - TTL override
     */
    set(key, value, ttl) {
        // Evict oldest if at capacity
        if (this.store.size >= this.maxSize) {
            const oldest = this.store.keys().next().value;
            this.store.delete(oldest);
        }

        this.store.set(key, {
            value,
            expiresAt: Date.now() + (ttl || this.ttl),
        });
    }

    /**
     * Delete a value from the cache.
     *
     * @param {string} key - Cache key
     * @returns {boolean} True if deleted
     */
    delete(key) {
        return this.store.delete(key);
    }

    /**
     * Clear all cached values.
     */
    clear() {
        this.store.clear();
    }

    /**
     * Get the number of cached entries.
     *
     * @returns {number} Cache size
     */
    size() {
        return this.store.size;
    }

    /**
     * Check if a key exists and is not expired.
     *
     * @param {string} key - Cache key
     * @returns {boolean} True if exists
     */
    has(key) {
        const entry = this.store.get(key);
        if (!entry) return false;

        if (Date.now() > entry.expiresAt) {
            this.store.delete(key);
            return false;
        }

        return true;
    }
}

/**
 * Generate a cache key from function arguments.
 *
 * @param {string} prefix - Key prefix
 * @param {Array} args - Function arguments
 * @returns {string} Cache key
 */
function generateCacheKey(prefix, args) {
    const hash = crypto
        .createHash('md5')
        .update(JSON.stringify(args))
        .digest('hex');
    return `${prefix}:${hash}`;
}

/**
 * Memoize a function with caching.
 *
 * @param {Function} fn - Function to memoize
 * @param {Object} [options] - Memoization options
 * @param {number} [options.ttl] - Cache TTL
 * @param {string} [options.keyPrefix] - Cache key prefix
 * @returns {Function} Memoized function
 */
function memoize(fn, options = {}) {
    const cache = new SimpleCache({ ttl: options.ttl });
    const prefix = options.keyPrefix || fn.name || 'memoized';

    return function memoized(...args) {
        const key = generateCacheKey(prefix, args);
        const cached = cache.get(key);

        if (cached !== undefined) {
            return cached;
        }

        const result = fn.apply(this, args);
        cache.set(key, result);
        return result;
    };
}

/**
 * Create a singleton instance.
 *
 * @param {Function} factory - Factory function
 * @returns {Function} Getter function
 */
function createSingleton(factory) {
    let instance = null;

    return function getInstance() {
        if (!instance) {
            instance = factory();
        }
        return instance;
    };
}

// CommonJS exports
module.exports = {
    SimpleCache,
    generateCacheKey,
    memoize,
    createSingleton,
    MAX_CACHE_SIZE,
    DEFAULT_TTL,
};

/**
 * Closure patterns and higher-order functions.
 * @module patterns/closures
 */

/**
 * Create a counter with encapsulated state.
 *
 * @param {number} [initial=0] - Initial count
 * @returns {Object} Counter API
 */
export function createCounter(initial = 0) {
    let count = initial;

    return {
        /**
         * Get the current count.
         * @returns {number} Current count
         */
        get() {
            return count;
        },

        /**
         * Increment the counter.
         * @returns {number} New count
         */
        increment() {
            return ++count;
        },

        /**
         * Decrement the counter.
         * @returns {number} New count
         */
        decrement() {
            return --count;
        },

        /**
         * Reset to initial value.
         */
        reset() {
            count = initial;
        },
    };
}

/**
 * Create a function that can only be called once.
 *
 * @param {Function} fn - Function to wrap
 * @returns {Function} Wrapped function
 */
export function once(fn) {
    let called = false;
    let result;

    return function onced(...args) {
        if (!called) {
            called = true;
            result = fn.apply(this, args);
        }
        return result;
    };
}

/**
 * Create a function with memoization.
 *
 * @param {Function} fn - Function to memoize
 * @param {Function} [keyFn] - Custom key function
 * @returns {Function} Memoized function
 */
export function memoize(fn, keyFn = JSON.stringify) {
    const cache = new Map();

    const memoized = function(...args) {
        const key = keyFn(args);

        if (cache.has(key)) {
            return cache.get(key);
        }

        const result = fn.apply(this, args);
        cache.set(key, result);
        return result;
    };

    memoized.cache = cache;
    memoized.clear = () => cache.clear();

    return memoized;
}

/**
 * Curry a function.
 *
 * @param {Function} fn - Function to curry
 * @returns {Function} Curried function
 */
export function curry(fn) {
    const arity = fn.length;

    function curried(...args) {
        if (args.length >= arity) {
            return fn.apply(this, args);
        }

        return function(...moreArgs) {
            return curried.apply(this, args.concat(moreArgs));
        };
    }

    return curried;
}

/**
 * Compose functions from right to left.
 *
 * @param {...Function} fns - Functions to compose
 * @returns {Function} Composed function
 */
export function compose(...fns) {
    if (fns.length === 0) {
        return arg => arg;
    }

    if (fns.length === 1) {
        return fns[0];
    }

    return fns.reduce((a, b) => (...args) => a(b(...args)));
}

/**
 * Pipe functions from left to right.
 *
 * @param {...Function} fns - Functions to pipe
 * @returns {Function} Piped function
 */
export function pipe(...fns) {
    return compose(...fns.reverse());
}

/**
 * Partial application.
 *
 * @param {Function} fn - Function to partially apply
 * @param {...*} preset - Preset arguments
 * @returns {Function} Partially applied function
 */
export function partial(fn, ...preset) {
    return function partiallyApplied(...later) {
        return fn.apply(this, [...preset, ...later]);
    };
}

/**
 * Create a factory function with private state.
 *
 * @param {Object} [initialState={}] - Initial state
 * @returns {Object} Factory with state management
 */
export function createStateFactory(initialState = {}) {
    let state = { ...initialState };
    const listeners = new Set();

    return {
        /**
         * Get current state.
         * @returns {Object} Current state
         */
        getState() {
            return { ...state };
        },

        /**
         * Update state.
         * @param {Object} updates - State updates
         */
        setState(updates) {
            const prevState = state;
            state = { ...state, ...updates };

            for (const listener of listeners) {
                listener(state, prevState);
            }
        },

        /**
         * Subscribe to state changes.
         * @param {Function} listener - Change listener
         * @returns {Function} Unsubscribe function
         */
        subscribe(listener) {
            listeners.add(listener);
            return () => listeners.delete(listener);
        },

        /**
         * Reset to initial state.
         */
        reset() {
            this.setState(initialState);
        },
    };
}

/**
 * Create a private method container using closures.
 *
 * @param {Object} publicApi - Public methods
 * @param {Object} privateMethods - Private methods
 * @returns {Object} Object with public API backed by private methods
 */
export function createPrivateScope(publicApi, privateMethods) {
    // Create bound versions of private methods
    const boundPrivate = {};
    for (const [key, method] of Object.entries(privateMethods)) {
        if (typeof method === 'function') {
            boundPrivate[key] = method.bind(null, boundPrivate);
        } else {
            boundPrivate[key] = method;
        }
    }

    // Bind public methods to private context
    const result = {};
    for (const [key, method] of Object.entries(publicApi)) {
        if (typeof method === 'function') {
            result[key] = method.bind(null, boundPrivate);
        } else {
            result[key] = method;
        }
    }

    return result;
}

/**
 * Create a lazy-evaluated value.
 *
 * @param {Function} factory - Factory function
 * @returns {Function} Getter function
 */
export function lazy(factory) {
    let value;
    let computed = false;

    return function getLazy() {
        if (!computed) {
            value = factory();
            computed = true;
        }
        return value;
    };
}

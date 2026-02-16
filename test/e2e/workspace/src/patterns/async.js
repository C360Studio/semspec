/**
 * Async/await patterns and utilities.
 * @module patterns/async
 */

/**
 * Delay execution for a specified time.
 *
 * @param {number} ms - Milliseconds to delay
 * @returns {Promise<void>} Promise that resolves after delay
 */
export function delay(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Retry a function with exponential backoff.
 *
 * @async
 * @param {Function} fn - Async function to retry
 * @param {Object} [options] - Retry options
 * @param {number} [options.maxAttempts=3] - Maximum attempts
 * @param {number} [options.initialDelay=1000] - Initial delay in ms
 * @param {number} [options.backoff=2] - Backoff multiplier
 * @param {number} [options.maxDelay=30000] - Maximum delay in ms
 * @returns {Promise<*>} Function result
 * @throws {Error} If all attempts fail
 */
export async function retry(fn, options = {}) {
    const {
        maxAttempts = 3,
        initialDelay = 1000,
        backoff = 2,
        maxDelay = 30000,
    } = options;

    let currentDelay = initialDelay;
    let lastError;

    for (let attempt = 1; attempt <= maxAttempts; attempt++) {
        try {
            return await fn();
        } catch (error) {
            lastError = error;

            if (attempt < maxAttempts) {
                await delay(currentDelay);
                currentDelay = Math.min(currentDelay * backoff, maxDelay);
            }
        }
    }

    throw lastError;
}

/**
 * Run promises with a concurrency limit.
 *
 * @async
 * @param {Array<Function>} tasks - Array of functions that return promises
 * @param {number} [limit=5] - Maximum concurrent tasks
 * @returns {Promise<Array>} Results in order
 */
export async function parallelLimit(tasks, limit = 5) {
    const results = [];
    const executing = new Set();

    for (let i = 0; i < tasks.length; i++) {
        const task = tasks[i];
        const promise = Promise.resolve().then(() => task());

        results.push(promise);
        executing.add(promise);

        const cleanup = () => executing.delete(promise);
        promise.then(cleanup, cleanup);

        if (executing.size >= limit) {
            await Promise.race(executing);
        }
    }

    return Promise.all(results);
}

/**
 * Execute async function with a timeout.
 *
 * @async
 * @template T
 * @param {Promise<T>} promise - Promise to wrap
 * @param {number} ms - Timeout in milliseconds
 * @param {string} [message='Operation timed out'] - Timeout error message
 * @returns {Promise<T>} Original promise result
 * @throws {Error} If operation times out
 */
export async function timeout(promise, ms, message = 'Operation timed out') {
    let timeoutId;

    const timeoutPromise = new Promise((_, reject) => {
        timeoutId = setTimeout(() => reject(new Error(message)), ms);
    });

    try {
        return await Promise.race([promise, timeoutPromise]);
    } finally {
        clearTimeout(timeoutId);
    }
}

/**
 * Debounce an async function.
 *
 * @param {Function} fn - Async function to debounce
 * @param {number} wait - Wait time in ms
 * @returns {Function} Debounced function
 */
export function debounceAsync(fn, wait) {
    let timeoutId = null;
    let pendingPromise = null;

    return function debounced(...args) {
        if (timeoutId) {
            clearTimeout(timeoutId);
        }

        return new Promise((resolve, reject) => {
            timeoutId = setTimeout(async () => {
                try {
                    const result = await fn.apply(this, args);
                    resolve(result);
                } catch (error) {
                    reject(error);
                }
            }, wait);
        });
    };
}

/**
 * Throttle an async function.
 *
 * @param {Function} fn - Async function to throttle
 * @param {number} limit - Minimum time between calls in ms
 * @returns {Function} Throttled function
 */
export function throttleAsync(fn, limit) {
    let lastRun = 0;
    let pendingPromise = null;

    return async function throttled(...args) {
        const now = Date.now();
        const remaining = limit - (now - lastRun);

        if (remaining <= 0) {
            lastRun = now;
            return fn.apply(this, args);
        }

        if (!pendingPromise) {
            pendingPromise = new Promise(resolve => {
                setTimeout(async () => {
                    lastRun = Date.now();
                    pendingPromise = null;
                    resolve(await fn.apply(this, args));
                }, remaining);
            });
        }

        return pendingPromise;
    };
}

/**
 * Generator function for async iteration.
 *
 * @async
 * @generator
 * @param {number} start - Starting number
 * @param {number} end - Ending number (exclusive)
 * @param {number} [delayMs=0] - Delay between yields
 * @yields {number} Numbers in range
 */
export async function* asyncRange(start, end, delayMs = 0) {
    for (let i = start; i < end; i++) {
        if (delayMs > 0) {
            await delay(delayMs);
        }
        yield i;
    }
}

/**
 * Collect async iterator into array.
 *
 * @async
 * @template T
 * @param {AsyncIterable<T>} iterable - Async iterable
 * @returns {Promise<Array<T>>} Collected array
 */
export async function collect(iterable) {
    const results = [];
    for await (const item of iterable) {
        results.push(item);
    }
    return results;
}

/**
 * Map over async iterator.
 *
 * @async
 * @generator
 * @template T, R
 * @param {AsyncIterable<T>} iterable - Source async iterable
 * @param {Function} fn - Mapping function
 * @yields {R} Mapped values
 */
export async function* asyncMap(iterable, fn) {
    for await (const item of iterable) {
        yield await fn(item);
    }
}

/**
 * Filter async iterator.
 *
 * @async
 * @generator
 * @template T
 * @param {AsyncIterable<T>} iterable - Source async iterable
 * @param {Function} predicate - Filter predicate
 * @yields {T} Filtered values
 */
export async function* asyncFilter(iterable, predicate) {
    for await (const item of iterable) {
        if (await predicate(item)) {
            yield item;
        }
    }
}

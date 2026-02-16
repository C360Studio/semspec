/**
 * Functional programming utilities.
 * @module patterns/functional
 */

/**
 * Identity function.
 *
 * @template T
 * @param {T} x - Input value
 * @returns {T} Same value
 */
export const identity = x => x;

/**
 * Constant function.
 *
 * @template T
 * @param {T} x - Value to return
 * @returns {Function} Function that always returns x
 */
export const constant = x => () => x;

/**
 * Negate a predicate function.
 *
 * @param {Function} predicate - Predicate to negate
 * @returns {Function} Negated predicate
 */
export const not = predicate => (...args) => !predicate(...args);

/**
 * Flip the arguments of a binary function.
 *
 * @param {Function} fn - Binary function
 * @returns {Function} Function with flipped arguments
 */
export const flip = fn => (a, b) => fn(b, a);

/**
 * Create a property accessor function.
 *
 * @param {string} key - Property name
 * @returns {Function} Accessor function
 */
export const prop = key => obj => obj[key];

/**
 * Create a property setter function.
 *
 * @param {string} key - Property name
 * @param {*} value - Value to set
 * @returns {Function} Setter function
 */
export const assoc = (key, value) => obj => ({ ...obj, [key]: value });

/**
 * Create a path accessor for nested properties.
 *
 * @param {string[]} path - Property path
 * @returns {Function} Path accessor
 */
export function path(pathArray) {
    return function accessor(obj) {
        return pathArray.reduce((acc, key) => acc?.[key], obj);
    };
}

/**
 * Map over an object's values.
 *
 * @param {Function} fn - Mapping function
 * @param {Object} obj - Source object
 * @returns {Object} Mapped object
 */
export function mapValues(fn, obj) {
    const result = {};
    for (const [key, value] of Object.entries(obj)) {
        result[key] = fn(value, key, obj);
    }
    return result;
}

/**
 * Filter an object by predicate.
 *
 * @param {Function} predicate - Filter predicate
 * @param {Object} obj - Source object
 * @returns {Object} Filtered object
 */
export function filterObject(predicate, obj) {
    const result = {};
    for (const [key, value] of Object.entries(obj)) {
        if (predicate(value, key, obj)) {
            result[key] = value;
        }
    }
    return result;
}

/**
 * Reduce an object.
 *
 * @param {Function} reducer - Reducer function
 * @param {*} initial - Initial value
 * @param {Object} obj - Source object
 * @returns {*} Reduced value
 */
export function reduceObject(reducer, initial, obj) {
    let acc = initial;
    for (const [key, value] of Object.entries(obj)) {
        acc = reducer(acc, value, key, obj);
    }
    return acc;
}

/**
 * Group array items by a key function.
 *
 * @param {Function} keyFn - Key extractor function
 * @param {Array} arr - Source array
 * @returns {Object} Grouped object
 */
export function groupBy(keyFn, arr) {
    return arr.reduce((groups, item) => {
        const key = keyFn(item);
        if (!groups[key]) {
            groups[key] = [];
        }
        groups[key].push(item);
        return groups;
    }, {});
}

/**
 * Zip multiple arrays together.
 *
 * @param {...Array} arrays - Arrays to zip
 * @returns {Array} Zipped array
 */
export function zip(...arrays) {
    const minLength = Math.min(...arrays.map(a => a.length));
    const result = [];

    for (let i = 0; i < minLength; i++) {
        result.push(arrays.map(a => a[i]));
    }

    return result;
}

/**
 * Flatten an array one level deep.
 *
 * @template T
 * @param {Array<T|T[]>} arr - Nested array
 * @returns {T[]} Flattened array
 */
export function flatten(arr) {
    return arr.reduce((flat, item) =>
        flat.concat(Array.isArray(item) ? item : [item]),
    []);
}

/**
 * Flatten an array completely.
 *
 * @param {Array} arr - Nested array
 * @returns {Array} Flattened array
 */
export function flattenDeep(arr) {
    return arr.reduce((flat, item) =>
        flat.concat(Array.isArray(item) ? flattenDeep(item) : item),
    []);
}

/**
 * Take first n items from array.
 *
 * @template T
 * @param {number} n - Number of items
 * @param {T[]} arr - Source array
 * @returns {T[]} First n items
 */
export function take(n, arr) {
    return arr.slice(0, n);
}

/**
 * Drop first n items from array.
 *
 * @template T
 * @param {number} n - Number of items
 * @param {T[]} arr - Source array
 * @returns {T[]} Remaining items
 */
export function drop(n, arr) {
    return arr.slice(n);
}

/**
 * Take items while predicate is true.
 *
 * @template T
 * @param {Function} predicate - Predicate function
 * @param {T[]} arr - Source array
 * @returns {T[]} Taken items
 */
export function takeWhile(predicate, arr) {
    const result = [];
    for (const item of arr) {
        if (!predicate(item)) break;
        result.push(item);
    }
    return result;
}

/**
 * Drop items while predicate is true.
 *
 * @template T
 * @param {Function} predicate - Predicate function
 * @param {T[]} arr - Source array
 * @returns {T[]} Remaining items
 */
export function dropWhile(predicate, arr) {
    let dropping = true;
    return arr.filter(item => {
        if (dropping && predicate(item)) {
            return false;
        }
        dropping = false;
        return true;
    });
}

/**
 * Get unique items from array.
 *
 * @template T
 * @param {T[]} arr - Source array
 * @returns {T[]} Unique items
 */
export function uniq(arr) {
    return [...new Set(arr)];
}

/**
 * Get unique items by key function.
 *
 * @template T
 * @param {Function} keyFn - Key extractor
 * @param {T[]} arr - Source array
 * @returns {T[]} Unique items
 */
export function uniqBy(keyFn, arr) {
    const seen = new Set();
    return arr.filter(item => {
        const key = keyFn(item);
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
    });
}

/**
 * Sort array by key function.
 *
 * @template T
 * @param {Function} keyFn - Key extractor
 * @param {T[]} arr - Source array
 * @returns {T[]} Sorted array
 */
export function sortBy(keyFn, arr) {
    return [...arr].sort((a, b) => {
        const keyA = keyFn(a);
        const keyB = keyFn(b);
        return keyA < keyB ? -1 : keyA > keyB ? 1 : 0;
    });
}

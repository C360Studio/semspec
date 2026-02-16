/**
 * Legacy prototype-based inheritance patterns.
 *
 * Demonstrates pre-ES6 class syntax for testing backwards compatibility.
 */

'use strict';

/**
 * EventEmitter constructor using prototype pattern.
 *
 * @constructor
 */
function EventEmitter() {
    this._events = {};
    this._maxListeners = 10;
}

/**
 * Add an event listener.
 *
 * @param {string} event - Event name
 * @param {Function} listener - Event handler
 * @returns {EventEmitter} this for chaining
 */
EventEmitter.prototype.on = function(event, listener) {
    if (typeof listener !== 'function') {
        throw new TypeError('Listener must be a function');
    }

    if (!this._events[event]) {
        this._events[event] = [];
    }

    if (this._events[event].length >= this._maxListeners) {
        console.warn('MaxListenersExceeded: ' + event);
    }

    this._events[event].push(listener);
    return this;
};

/**
 * Remove an event listener.
 *
 * @param {string} event - Event name
 * @param {Function} listener - Event handler to remove
 * @returns {EventEmitter} this for chaining
 */
EventEmitter.prototype.off = function(event, listener) {
    if (!this._events[event]) {
        return this;
    }

    this._events[event] = this._events[event].filter(function(l) {
        return l !== listener;
    });

    return this;
};

/**
 * Emit an event.
 *
 * @param {string} event - Event name
 * @param {...*} args - Arguments to pass to listeners
 * @returns {boolean} True if event had listeners
 */
EventEmitter.prototype.emit = function(event) {
    if (!this._events[event]) {
        return false;
    }

    var args = Array.prototype.slice.call(arguments, 1);
    var listeners = this._events[event].slice();

    for (var i = 0; i < listeners.length; i++) {
        listeners[i].apply(this, args);
    }

    return true;
};

/**
 * Add a one-time listener.
 *
 * @param {string} event - Event name
 * @param {Function} listener - Event handler
 * @returns {EventEmitter} this for chaining
 */
EventEmitter.prototype.once = function(event, listener) {
    var self = this;
    var wrapper = function() {
        self.off(event, wrapper);
        listener.apply(self, arguments);
    };
    return this.on(event, wrapper);
};

/**
 * Set the maximum number of listeners.
 *
 * @param {number} n - Maximum listeners
 * @returns {EventEmitter} this for chaining
 */
EventEmitter.prototype.setMaxListeners = function(n) {
    this._maxListeners = n;
    return this;
};

/**
 * Get listener count for an event.
 *
 * @param {string} event - Event name
 * @returns {number} Number of listeners
 */
EventEmitter.prototype.listenerCount = function(event) {
    return this._events[event] ? this._events[event].length : 0;
};

// Observable - inherits from EventEmitter
/**
 * Observable constructor extending EventEmitter.
 *
 * @constructor
 * @param {*} initialValue - Initial observable value
 */
function Observable(initialValue) {
    EventEmitter.call(this);
    this._value = initialValue;
}

// Set up inheritance
Observable.prototype = Object.create(EventEmitter.prototype);
Observable.prototype.constructor = Observable;

/**
 * Get the current value.
 *
 * @returns {*} Current value
 */
Observable.prototype.get = function() {
    return this._value;
};

/**
 * Set a new value and emit change event.
 *
 * @param {*} newValue - New value
 */
Observable.prototype.set = function(newValue) {
    var oldValue = this._value;
    this._value = newValue;

    if (oldValue !== newValue) {
        this.emit('change', newValue, oldValue);
    }
};

/**
 * Subscribe to value changes.
 *
 * @param {Function} callback - Change handler
 * @returns {Function} Unsubscribe function
 */
Observable.prototype.subscribe = function(callback) {
    this.on('change', callback);
    var self = this;
    return function unsubscribe() {
        self.off('change', callback);
    };
};

// ComputedObservable - inherits from Observable
/**
 * Computed observable that derives value from other observables.
 *
 * @constructor
 * @param {Function} computation - Function to compute value
 * @param {Observable[]} dependencies - Dependency observables
 */
function ComputedObservable(computation, dependencies) {
    var self = this;

    Observable.call(this, computation());

    // Re-compute when dependencies change
    dependencies.forEach(function(dep) {
        dep.on('change', function() {
            self.set(computation());
        });
    });
}

// Set up inheritance
ComputedObservable.prototype = Object.create(Observable.prototype);
ComputedObservable.prototype.constructor = ComputedObservable;

// Override set to make it read-only
ComputedObservable.prototype.set = function() {
    throw new Error('Cannot set computed observable directly');
};

// Export using CommonJS
module.exports = {
    EventEmitter: EventEmitter,
    Observable: Observable,
    ComputedObservable: ComputedObservable,
};

// OpenLoadBalancer SPA Framework
// Vanilla JavaScript Single Page Application framework
// Zero external dependencies

(function(global) {
    'use strict';

    /**
     * OLBSPA - Core SPA Framework
     */
    const OLBSPA = {
        version: '1.0.0',
        routes: new Map(),
        middleware: [],
        currentRoute: null,
        state: new Map(),
        stateSubscribers: new Map(),
        components: new Map(),
        eventListeners: new WeakMap(),
    };

    // ==================== Router ====================

    /**
     * Route definition
     * @typedef {Object} Route
     * @property {string} path - Route path (e.g., '/dashboard', '/backends/:id')
     * @property {Function} handler - Route handler function
     * @property {string} [title] - Page title
     * @property {Function} [beforeEnter] - Before enter guard
     * @property {Function} [afterLeave] - After leave hook
     */

    /**
     * Register a route
     * @param {string} path - Route path
     * @param {Function} handler - Route handler
     * @param {Object} options - Route options
     */
    OLBSPA.route = function(path, handler, options = {}) {
        const route = {
            path,
            handler,
            title: options.title || '',
            beforeEnter: options.beforeEnter || null,
            afterLeave: options.afterLeave || null,
            params: [],
            regex: null
        };

        // Convert path to regex for parameter extraction
        const paramNames = [];
        const regexPath = path.replace(/:([^/]+)/g, (match, name) => {
            paramNames.push(name);
            return '([^/]+)';
        });

        route.params = paramNames;
        route.regex = new RegExp('^' + regexPath + '$');

        this.routes.set(path, route);
    };

    /**
     * Navigate to a route
     * @param {string} path - Path to navigate to
     * @param {Object} [state] - State to pass
     */
    OLBSPA.navigate = function(path, state = null) {
        if (state) {
            history.pushState(state, '', '#' + path);
        } else {
            history.pushState(null, '', '#' + path);
        }
        this.handleRoute();
    };

    /**
     * Replace current route
     * @param {string} path - Path to navigate to
     * @param {Object} [state] - State to pass
     */
    OLBSPA.replace = function(path, state = null) {
        if (state) {
            history.replaceState(state, '', '#' + path);
        } else {
            history.replaceState(null, '', '#' + path);
        }
        this.handleRoute();
    };

    /**
     * Go back in history
     */
    OLBSPA.back = function() {
        history.back();
    };

    /**
     * Handle route change
     * @private
     */
    OLBSPA.handleRoute = async function() {
        const hash = window.location.hash.slice(1) || '/';
        const [path, queryString] = hash.split('?');

        // Parse query parameters
        const query = {};
        if (queryString) {
            const params = new URLSearchParams(queryString);
            for (const [key, value] of params) {
                query[key] = value;
            }
        }

        // Find matching route
        let matchedRoute = null;
        let routeParams = {};

        for (const route of this.routes.values()) {
            const match = path.match(route.regex);
            if (match) {
                matchedRoute = route;
                route.params.forEach((name, index) => {
                    routeParams[name] = decodeURIComponent(match[index + 1]);
                });
                break;
            }
        }

        // Handle 404
        if (!matchedRoute) {
            matchedRoute = this.routes.get('/404') || {
                handler: () => this.render404()
            };
        }

        // Call beforeLeave on current route
        if (this.currentRoute && this.currentRoute.afterLeave) {
            await this.currentRoute.afterLeave();
        }

        // Run middleware
        for (const mw of this.middleware) {
            const result = await mw(path, matchedRoute, routeParams);
            if (result === false) {
                return; // Navigation cancelled
            }
        }

        // Call beforeEnter on new route
        if (matchedRoute.beforeEnter) {
            const result = await matchedRoute.beforeEnter(routeParams, query);
            if (result === false) {
                return; // Navigation cancelled
            }
        }

        // Update current route
        this.currentRoute = matchedRoute;

        // Update document title
        if (matchedRoute.title) {
            document.title = matchedRoute.title + ' - OpenLoadBalancer';
        }

        // Execute route handler
        const context = {
            path,
            params: routeParams,
            query,
            navigate: this.navigate.bind(this),
            replace: this.replace.bind(this)
        };

        await matchedRoute.handler(context);

        // Update active navigation
        this.updateActiveNav(path);
    };

    /**
     * Render 404 page
     * @private
     */
    OLBSPA.render404 = function() {
        const container = document.getElementById('app-content');
        if (container) {
            // Use safe DOM construction instead of innerHTML
            container.innerHTML = '';

            const emptyState = document.createElement('div');
            emptyState.className = 'empty-state';

            const icon = document.createElement('div');
            icon.className = 'empty-state-icon';
            icon.textContent = '404';

            const title = document.createElement('h2');
            title.textContent = 'Page Not Found';

            const desc = document.createElement('p');
            desc.textContent = "The page you're looking for doesn't exist.";

            const btn = document.createElement('button');
            btn.className = 'btn btn-primary';
            btn.textContent = 'Go to Dashboard';
            btn.addEventListener('click', () => this.navigate('/dashboard'));

            emptyState.appendChild(icon);
            emptyState.appendChild(title);
            emptyState.appendChild(desc);
            emptyState.appendChild(btn);

            container.appendChild(emptyState);
        }
    };

    /**
     * Update active navigation state
     * @private
     */
    OLBSPA.updateActiveNav = function(path) {
        document.querySelectorAll('[data-nav]').forEach(el => {
            const navPath = el.getAttribute('data-nav');
            if (navPath === path || path.startsWith(navPath + '/')) {
                el.classList.add('active');
            } else {
                el.classList.remove('active');
            }
        });
    };

    /**
     * Add middleware
     * @param {Function} fn - Middleware function
     */
    OLBSPA.use = function(fn) {
        this.middleware.push(fn);
    };

    // ==================== State Management ====================

    /**
     * Set state value
     * @param {string} key - State key
     * @param {*} value - State value
     */
    OLBSPA.setState = function(key, value) {
        const oldValue = this.state.get(key);
        this.state.set(key, value);

        // Notify subscribers
        const subscribers = this.stateSubscribers.get(key);
        if (subscribers) {
            subscribers.forEach(callback => {
                try {
                    callback(value, oldValue);
                } catch (e) {
                    console.error('State subscriber error:', e);
                }
            });
        }
    };

    /**
     * Get state value
     * @param {string} key - State key
     * @param {*} [defaultValue] - Default value if not set
     * @returns {*} State value
     */
    OLBSPA.getState = function(key, defaultValue) {
        return this.state.has(key) ? this.state.get(key) : defaultValue;
    };

    /**
     * Subscribe to state changes
     * @param {string} key - State key
     * @param {Function} callback - Callback function
     * @returns {Function} Unsubscribe function
     */
    OLBSPA.subscribe = function(key, callback) {
        if (!this.stateSubscribers.has(key)) {
            this.stateSubscribers.set(key, new Set());
        }
        this.stateSubscribers.get(key).add(callback);

        // Return unsubscribe function
        return () => {
            const subs = this.stateSubscribers.get(key);
            if (subs) {
                subs.delete(callback);
            }
        };
    };

    /**
     * Create reactive state object
     * @param {Object} initialState - Initial state object
     * @returns {Object} Reactive state proxy
     */
    OLBSPA.reactive = function(initialState) {
        const self = this;
        const subscribers = new Map();

        const proxy = new Proxy(initialState, {
            get(target, prop) {
                return target[prop];
            },
            set(target, prop, value) {
                const oldValue = target[prop];
                target[prop] = value;

                if (oldValue !== value) {
                    const subs = subscribers.get(prop);
                    if (subs) {
                        subs.forEach(cb => cb(value, oldValue));
                    }
                }
                return true;
            }
        });

        proxy.$subscribe = function(prop, callback) {
            if (!subscribers.has(prop)) {
                subscribers.set(prop, new Set());
            }
            subscribers.get(prop).add(callback);

            return () => {
                const subs = subscribers.get(prop);
                if (subs) {
                    subs.delete(callback);
                }
            };
        };

        proxy.$watch = function(prop, callback) {
            // Immediate call with current value
            callback(initialState[prop], undefined);
            return proxy.$subscribe(prop, callback);
        };

        return proxy;
    };

    // ==================== Component System ====================

    /**
     * Register a component
     * @param {string} name - Component name
     * @param {Object} definition - Component definition
     */
    OLBSPA.component = function(name, definition) {
        this.components.set(name, definition);
    };

    /**
     * Render a component
     * @param {string} name - Component name
     * @param {Object} props - Component props
     * @returns {HTMLElement} Rendered element
     */
    OLBSPA.render = function(name, props = {}) {
        const definition = this.components.get(name);
        if (!definition) {
            console.error('Component not found:', name);
            return document.createElement('div');
        }

        // Create element
        let element;
        if (typeof definition.template === 'function') {
            const html = definition.template(props);
            element = this.htmlToElement(html);
        } else if (typeof definition.render === 'function') {
            element = definition.render(props);
        } else {
            element = document.createElement('div');
        }

        // Attach lifecycle
        if (definition.mounted) {
            setTimeout(() => definition.mounted(element, props), 0);
        }

        return element;
    };

    /**
     * Convert HTML string to DOM element
     * @param {string} html - HTML string
     * @returns {HTMLElement} DOM element
     */
    OLBSPA.htmlToElement = function(html) {
        const template = document.createElement('template');
        template.innerHTML = html.trim();
        return template.content.firstChild;
    };

    /**
     * Mount component to DOM
     * @param {HTMLElement|string} target - Target element or selector
     * @param {string|HTMLElement} component - Component name or element
     * @param {Object} props - Component props
     */
    OLBSPA.mount = function(target, component, props = {}) {
        const container = typeof target === 'string'
            ? document.querySelector(target)
            : target;

        if (!container) {
            console.error('Mount target not found:', target);
            return;
        }

        const element = typeof component === 'string'
            ? this.render(component, props)
            : component;

        container.innerHTML = '';
        container.appendChild(element);
    };

    // ==================== Event Delegation ====================

    /**
     * Event delegation helper
     * @param {HTMLElement} container - Container element
     * @param {string} selector - CSS selector
     * @param {string} event - Event name
     * @param {Function} handler - Event handler
     */
    OLBSPA.on = function(container, selector, event, handler) {
        const wrappedHandler = (e) => {
            const target = e.target.closest(selector);
            if (target && container.contains(target)) {
                handler(e, target);
            }
        };

        container.addEventListener(event, wrappedHandler);

        // Store for cleanup
        if (!this.eventListeners.has(container)) {
            this.eventListeners.set(container, []);
        }
        this.eventListeners.get(container).push({
            event,
            handler: wrappedHandler
        });
    };

    /**
     * Remove all delegated events from container
     * @param {HTMLElement} container - Container element
     */
    OLBSPA.off = function(container) {
        const listeners = this.eventListeners.get(container);
        if (listeners) {
            listeners.forEach(({ event, handler }) => {
                container.removeEventListener(event, handler);
            });
            this.eventListeners.delete(container);
        }
    };

    /**
     * Emit custom event
     * @param {string} name - Event name
     * @param {*} detail - Event detail
     * @param {HTMLElement} [target] - Target element (default: document)
     */
    OLBSPA.emit = function(name, detail, target = document) {
        const event = new CustomEvent('olb:' + name, {
            detail,
            bubbles: true,
            cancelable: true
        });
        target.dispatchEvent(event);
    };

    /**
     * Listen for custom event
     * @param {string} name - Event name
     * @param {Function} handler - Event handler
     * @param {HTMLElement} [target] - Target element (default: document)
     * @returns {Function} Unsubscribe function
     */
    OLBSPA.listen = function(name, handler, target = document) {
        const wrappedHandler = (e) => handler(e.detail, e);
        target.addEventListener('olb:' + name, wrappedHandler);

        return () => {
            target.removeEventListener('olb:' + name, wrappedHandler);
        };
    };

    // ==================== Utilities ====================

    /**
     * Debounce function
     * @param {Function} fn - Function to debounce
     * @param {number} delay - Delay in milliseconds
     * @returns {Function} Debounced function
     */
    OLBSPA.debounce = function(fn, delay) {
        let timeout;
        return function(...args) {
            clearTimeout(timeout);
            timeout = setTimeout(() => fn.apply(this, args), delay);
        };
    };

    /**
     * Throttle function
     * @param {Function} fn - Function to throttle
     * @param {number} limit - Limit in milliseconds
     * @returns {Function} Throttled function
     */
    OLBSPA.throttle = function(fn, limit) {
        let inThrottle;
        return function(...args) {
            if (!inThrottle) {
                fn.apply(this, args);
                inThrottle = true;
                setTimeout(() => inThrottle = false, limit);
            }
        };
    };

    /**
     * Format date
     * @param {Date|string|number} date - Date to format
     * @param {string} [format] - Format string
     * @returns {string} Formatted date
     */
    OLBSPA.formatDate = function(date, format = 'short') {
        const d = new Date(date);

        switch (format) {
            case 'short':
                return d.toLocaleDateString();
            case 'long':
                return d.toLocaleString();
            case 'time':
                return d.toLocaleTimeString();
            case 'iso':
                return d.toISOString();
            case 'relative':
                return this.relativeTime(d);
            default:
                return d.toLocaleString();
        }
    };

    /**
     * Get relative time string
     * @param {Date|string|number} date - Date
     * @returns {string} Relative time
     */
    OLBSPA.relativeTime = function(date) {
        const d = new Date(date);
        const now = new Date();
        const diff = now - d;
        const seconds = Math.floor(diff / 1000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        const days = Math.floor(hours / 24);

        if (seconds < 60) return 'just now';
        if (minutes < 60) return `${minutes}m ago`;
        if (hours < 24) return `${hours}h ago`;
        if (days < 7) return `${days}d ago`;
        return d.toLocaleDateString();
    };

    /**
     * Format bytes
     * @param {number} bytes - Bytes to format
     * @param {number} [decimals] - Decimal places
     * @returns {string} Formatted bytes
     */
    OLBSPA.formatBytes = function(bytes, decimals = 2) {
        if (bytes === 0) return '0 B';

        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));

        return parseFloat((bytes / Math.pow(k, i)).toFixed(decimals)) + ' ' + sizes[i];
    };

    /**
     * Format duration
     * @param {number} ms - Milliseconds
     * @returns {string} Formatted duration
     */
    OLBSPA.formatDuration = function(ms) {
        if (ms < 1000) return ms + 'ms';
        if (ms < 60000) return (ms / 1000).toFixed(2) + 's';
        if (ms < 3600000) return (ms / 60000).toFixed(1) + 'm';
        return (ms / 3600000).toFixed(1) + 'h';
    };

    /**
     * Escape HTML
     * @param {string} str - String to escape
     * @returns {string} Escaped string
     */
    OLBSPA.escapeHtml = function(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    };

    // ==================== Initialization ====================

    /**
     * Initialize the SPA
     */
    OLBSPA.init = function() {
        // Handle hash change
        window.addEventListener('hashchange', () => this.handleRoute());

        // Handle popstate
        window.addEventListener('popstate', () => this.handleRoute());

        // Handle initial route
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', () => this.handleRoute());
        } else {
            this.handleRoute();
        }
    };

    // Expose to global
    global.OLBSPA = OLBSPA;

})(window);

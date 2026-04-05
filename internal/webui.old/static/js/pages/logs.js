// logs.js - Logs page for OpenLoadBalancer Web UI
// Phase 3.6: Real-time log streaming with filtering and search

(function() {
    'use strict';

    // Log level colors
    const LEVEL_COLORS = {
        trace: '#6b7280',
        debug: '#3b82f6',
        info: '#10b981',
        warn: '#f59e0b',
        error: '#ef4444',
        fatal: '#dc2626'
    };

    // Log level background colors
    const LEVEL_BG_COLORS = {
        trace: '#f3f4f6',
        debug: '#dbeafe',
        info: '#d1fae5',
        warn: '#fef3c7',
        error: '#fee2e2',
        fatal: '#fecaca'
    };

    // Logs page state
    const state = {
        ws: null,
        isStreaming: true,
        logs: [],
        filteredLogs: [],
        filters: {
            level: 'all',
            route: '',
            backend: '',
            statusCode: '',
            search: '',
            startTime: null,
            endTime: null
        },
        pageSize: 100,
        currentPage: 0,
        hasMore: true,
        selectedLog: null,
        debounceTimer: null
    };

    /**
     * Initialize the logs page
     */
    function init() {
        renderPage();
        initEventListeners();
        connectWebSocket();
        loadHistoricalLogs();
    }

    /**
     * Create an element with attributes and children
     */
    function createElement(tag, attrs, children) {
        const el = document.createElement(tag);
        if (attrs) {
            Object.entries(attrs).forEach(([key, value]) => {
                if (key === 'textContent') {
                    el.textContent = value;
                } else if (key === 'className') {
                    el.className = value;
                } else {
                    el.setAttribute(key, value);
                }
            });
        }
        if (children) {
            children.forEach(child => {
                if (typeof child === 'string') {
                    el.appendChild(document.createTextNode(child));
                } else {
                    el.appendChild(child);
                }
            });
        }
        return el;
    }

    /**
     * Render the page structure
     */
    function renderPage() {
        const container = document.getElementById('app');
        if (!container) return;

        // Clear container safely
        while (container.firstChild) {
            container.removeChild(container.firstChild);
        }

        // Main page container
        const page = createElement('div', { className: 'logs-page' });

        // Header
        const header = createElement('header', { className: 'page-header' });
        const title = createElement('h1', { textContent: 'Logs' });
        const headerActions = createElement('div', { className: 'header-actions' });

        // Stream control button
        const streamBtn = createElement('button', {
            id: 'stream-toggle',
            className: 'btn-stream active',
            textContent: 'Pause Stream'
        });
        headerActions.appendChild(streamBtn);
        header.appendChild(title);
        header.appendChild(headerActions);
        page.appendChild(header);

        // Filters section
        const filtersSection = createElement('div', { className: 'logs-filters' });

        // Search input
        const searchGroup = createElement('div', { className: 'filter-group search-group' });
        const searchInput = createElement('input', {
            type: 'text',
            id: 'log-search',
            className: 'search-input',
            placeholder: 'Search logs...'
        });
        searchGroup.appendChild(searchInput);

        // Level filter
        const levelGroup = createElement('div', { className: 'filter-group' });
        const levelLabel = createElement('label', { textContent: 'Level' });
        const levelSelect = createElement('select', { id: 'filter-level' });
        const levels = ['all', 'trace', 'debug', 'info', 'warn', 'error', 'fatal'];
        levels.forEach(level => {
            const option = createElement('option', { value: level, textContent: level.toUpperCase() });
            levelSelect.appendChild(option);
        });
        levelGroup.appendChild(levelLabel);
        levelGroup.appendChild(levelSelect);

        // Route filter
        const routeGroup = createElement('div', { className: 'filter-group' });
        const routeLabel = createElement('label', { textContent: 'Route' });
        const routeInput = createElement('input', {
            type: 'text',
            id: 'filter-route',
            placeholder: 'Route name...'
        });
        routeGroup.appendChild(routeLabel);
        routeGroup.appendChild(routeInput);

        // Backend filter
        const backendGroup = createElement('div', { className: 'filter-group' });
        const backendLabel = createElement('label', { textContent: 'Backend' });
        const backendInput = createElement('input', {
            type: 'text',
            id: 'filter-backend',
            placeholder: 'Backend ID...'
        });
        backendGroup.appendChild(backendLabel);
        backendGroup.appendChild(backendInput);

        // Status code filter
        const statusGroup = createElement('div', { className: 'filter-group' });
        const statusLabel = createElement('label', { textContent: 'Status' });
        const statusSelect = createElement('select', { id: 'filter-status' });
        const statuses = [
            { value: '', label: 'All' },
            { value: '2xx', label: '2xx Success' },
            { value: '3xx', label: '3xx Redirect' },
            { value: '4xx', label: '4xx Client Error' },
            { value: '5xx', label: '5xx Server Error' }
        ];
        statuses.forEach(status => {
            const option = createElement('option', { value: status.value, textContent: status.label });
            statusSelect.appendChild(option);
        });
        statusGroup.appendChild(statusLabel);
        statusGroup.appendChild(statusSelect);

        // Time range filter
        const timeGroup = createElement('div', { className: 'filter-group time-range' });
        const timeLabel = createElement('label', { textContent: 'Time Range' });
        const timeContainer = createElement('div', { className: 'time-inputs' });
        const startInput = createElement('input', { type: 'datetime-local', id: 'filter-start' });
        const timeSeparator = createElement('span', { textContent: ' to ' });
        const endInput = createElement('input', { type: 'datetime-local', id: 'filter-end' });
        timeContainer.appendChild(startInput);
        timeContainer.appendChild(timeSeparator);
        timeContainer.appendChild(endInput);
        timeGroup.appendChild(timeLabel);
        timeGroup.appendChild(timeContainer);

        // Clear filters button
        const clearBtn = createElement('button', {
            id: 'clear-filters',
            className: 'btn-secondary',
            textContent: 'Clear Filters'
        });

        filtersSection.appendChild(searchGroup);
        filtersSection.appendChild(levelGroup);
        filtersSection.appendChild(routeGroup);
        filtersSection.appendChild(backendGroup);
        filtersSection.appendChild(statusGroup);
        filtersSection.appendChild(timeGroup);
        filtersSection.appendChild(clearBtn);
        page.appendChild(filtersSection);

        // Stats bar
        const statsBar = createElement('div', { className: 'logs-stats' });
        const statsTotal = createElement('span', { id: 'stats-total', textContent: 'Total: 0' });
        const statsFiltered = createElement('span', { id: 'stats-filtered', textContent: 'Filtered: 0' });
        const statsStreaming = createElement('span', {
            id: 'stats-streaming',
            className: 'streaming-indicator active',
            textContent: 'Live'
        });
        statsBar.appendChild(statsTotal);
        statsBar.appendChild(statsFiltered);
        statsBar.appendChild(statsStreaming);
        page.appendChild(statsBar);

        // Logs container
        const logsContainer = createElement('div', { className: 'logs-container', id: 'logs-container' });
        const logsList = createElement('div', { className: 'logs-list', id: 'logs-list' });
        const loadingIndicator = createElement('div', {
            className: 'loading-indicator',
            id: 'loading-indicator',
            textContent: 'Loading...'
        });
        logsContainer.appendChild(logsList);
        logsContainer.appendChild(loadingIndicator);
        page.appendChild(logsContainer);

        // Log detail panel
        const detailPanel = createElement('div', { className: 'log-detail-panel', id: 'log-detail' });
        const detailPlaceholder = createElement('p', {
            className: 'placeholder',
            textContent: 'Select a log entry to view details'
        });
        detailPanel.appendChild(detailPlaceholder);
        page.appendChild(detailPanel);

        container.appendChild(page);
    }

    /**
     * Initialize event listeners
     */
    function initEventListeners() {
        // Stream toggle
        const streamBtn = document.getElementById('stream-toggle');
        if (streamBtn) {
            streamBtn.addEventListener('click', toggleStream);
        }

        // Search with debounce
        const searchInput = document.getElementById('log-search');
        if (searchInput) {
            searchInput.addEventListener('input', (e) => {
                clearTimeout(state.debounceTimer);
                state.debounceTimer = setTimeout(() => {
                    state.filters.search = e.target.value;
                    applyFilters();
                }, 300);
            });
        }

        // Level filter
        const levelSelect = document.getElementById('filter-level');
        if (levelSelect) {
            levelSelect.addEventListener('change', (e) => {
                state.filters.level = e.target.value;
                applyFilters();
            });
        }

        // Route filter
        const routeInput = document.getElementById('filter-route');
        if (routeInput) {
            routeInput.addEventListener('input', (e) => {
                clearTimeout(state.debounceTimer);
                state.debounceTimer = setTimeout(() => {
                    state.filters.route = e.target.value;
                    applyFilters();
                }, 300);
            });
        }

        // Backend filter
        const backendInput = document.getElementById('filter-backend');
        if (backendInput) {
            backendInput.addEventListener('input', (e) => {
                clearTimeout(state.debounceTimer);
                state.debounceTimer = setTimeout(() => {
                    state.filters.backend = e.target.value;
                    applyFilters();
                }, 300);
            });
        }

        // Status filter
        const statusSelect = document.getElementById('filter-status');
        if (statusSelect) {
            statusSelect.addEventListener('change', (e) => {
                state.filters.statusCode = e.target.value;
                applyFilters();
            });
        }

        // Time range filters
        const startInput = document.getElementById('filter-start');
        const endInput = document.getElementById('filter-end');
        if (startInput) {
            startInput.addEventListener('change', (e) => {
                state.filters.startTime = e.target.value ? new Date(e.target.value).getTime() : null;
                applyFilters();
            });
        }
        if (endInput) {
            endInput.addEventListener('change', (e) => {
                state.filters.endTime = e.target.value ? new Date(e.target.value).getTime() : null;
                applyFilters();
            });
        }

        // Clear filters
        const clearBtn = document.getElementById('clear-filters');
        if (clearBtn) {
            clearBtn.addEventListener('click', clearFilters);
        }

        // Infinite scroll
        const logsContainer = document.getElementById('logs-container');
        if (logsContainer) {
            logsContainer.addEventListener('scroll', handleScroll);
        }
    }

    /**
     * Connect to WebSocket for real-time logs
     */
    function connectWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = protocol + '//' + window.location.host + '/ws/logs';

        try {
            state.ws = new WebSocket(wsUrl);

            state.ws.onopen = () => {
                console.log('WebSocket connected');
                updateStreamingIndicator(true);
            };

            state.ws.onmessage = (event) => {
                if (!state.isStreaming) return;

                try {
                    const log = JSON.parse(event.data);
                    addLog(log);
                } catch (e) {
                    console.error('Failed to parse log:', e);
                }
            };

            state.ws.onclose = () => {
                console.log('WebSocket disconnected');
                updateStreamingIndicator(false);
                // Reconnect after 5 seconds
                setTimeout(connectWebSocket, 5000);
            };

            state.ws.onerror = (error) => {
                console.error('WebSocket error:', error);
                updateStreamingIndicator(false);
            };
        } catch (error) {
            console.error('Failed to connect WebSocket:', error);
        }
    }

    /**
     * Toggle log streaming
     */
    function toggleStream() {
        state.isStreaming = !state.isStreaming;

        const streamBtn = document.getElementById('stream-toggle');
        if (streamBtn) {
            streamBtn.textContent = state.isStreaming ? 'Pause Stream' : 'Resume Stream';
            streamBtn.classList.toggle('active', state.isStreaming);
            streamBtn.classList.toggle('paused', !state.isStreaming);
        }

        updateStreamingIndicator(state.isStreaming);
    }

    /**
     * Update streaming indicator
     */
    function updateStreamingIndicator(isActive) {
        const indicator = document.getElementById('stats-streaming');
        if (indicator) {
            indicator.classList.toggle('active', isActive);
            indicator.textContent = isActive ? 'Live' : 'Paused';
        }
    }

    /**
     * Add a new log entry
     */
    function addLog(log) {
        state.logs.unshift(log);

        // Keep only last 10000 logs in memory
        if (state.logs.length > 10000) {
            state.logs = state.logs.slice(0, 10000);
        }

        // Apply filters to check if this log should be visible
        if (matchesFilters(log)) {
            state.filteredLogs.unshift(log);

            // Keep filtered list at reasonable size
            if (state.filteredLogs.length > state.pageSize * 2) {
                state.filteredLogs = state.filteredLogs.slice(0, state.pageSize * 2);
            }

            renderLogEntry(log, true);
            updateStats();
        }
    }

    /**
     * Load historical logs
     */
    async function loadHistoricalLogs() {
        if (!state.hasMore) return;

        const loadingIndicator = document.getElementById('loading-indicator');
        if (loadingIndicator) {
            loadingIndicator.style.display = 'block';
        }

        try {
            const params = new URLSearchParams({
                limit: state.pageSize.toString(),
                offset: (state.currentPage * state.pageSize).toString()
            });

            // Add filters
            if (state.filters.level !== 'all') {
                params.append('level', state.filters.level);
            }
            if (state.filters.startTime) {
                params.append('start', Math.floor(state.filters.startTime / 1000).toString());
            }
            if (state.filters.endTime) {
                params.append('end', Math.floor(state.filters.endTime / 1000).toString());
            }

            const response = await fetch('/api/v1/logs?' + params.toString());
            const result = await response.json();

            if (result.success && result.data) {
                const newLogs = result.data.logs || [];

                if (newLogs.length < state.pageSize) {
                    state.hasMore = false;
                }

                state.logs = state.logs.concat(newLogs);
                state.currentPage++;

                applyFilters();
            }
        } catch (error) {
            console.error('Failed to load logs:', error);
        } finally {
            if (loadingIndicator) {
                loadingIndicator.style.display = state.hasMore ? 'block' : 'none';
            }
        }
    }

    /**
     * Apply filters to logs
     */
    function applyFilters() {
        state.filteredLogs = state.logs.filter(log => matchesFilters(log));
        renderLogsList();
        updateStats();
    }

    /**
     * Check if a log matches current filters
     */
    function matchesFilters(log) {
        // Level filter
        if (state.filters.level !== 'all' && log.level !== state.filters.level) {
            return false;
        }

        // Search filter
        if (state.filters.search) {
            const searchLower = state.filters.search.toLowerCase();
            const messageMatch = log.message && log.message.toLowerCase().includes(searchLower);
            const fieldsMatch = JSON.stringify(log.fields || {}).toLowerCase().includes(searchLower);
            if (!messageMatch && !fieldsMatch) {
                return false;
            }
        }

        // Route filter
        if (state.filters.route && log.route !== state.filters.route) {
            return false;
        }

        // Backend filter
        if (state.filters.backend && log.backend !== state.filters.backend) {
            return false;
        }

        // Status code filter
        if (state.filters.statusCode && log.status_code) {
            const status = log.status_code;
            const filter = state.filters.statusCode;
            if (filter === '2xx' && (status < 200 || status >= 300)) return false;
            if (filter === '3xx' && (status < 300 || status >= 400)) return false;
            if (filter === '4xx' && (status < 400 || status >= 500)) return false;
            if (filter === '5xx' && (status < 500 || status >= 600)) return false;
        }

        // Time range filter
        const logTime = new Date(log.timestamp).getTime();
        if (state.filters.startTime && logTime < state.filters.startTime) {
            return false;
        }
        if (state.filters.endTime && logTime > state.filters.endTime) {
            return false;
        }

        return true;
    }

    /**
     * Render the logs list
     */
    function renderLogsList() {
        const logsList = document.getElementById('logs-list');
        if (!logsList) return;

        // Clear list safely
        while (logsList.firstChild) {
            logsList.removeChild(logsList.firstChild);
        }

        state.filteredLogs.forEach(log => {
            renderLogEntry(log, false);
        });
    }

    /**
     * Render a single log entry
     */
    function renderLogEntry(log, prepend) {
        const logsList = document.getElementById('logs-list');
        if (!logsList) return;

        const entry = createLogEntryElement(log);

        if (prepend) {
            logsList.insertBefore(entry, logsList.firstChild);

            // Remove old entries if too many
            while (logsList.children.length > state.pageSize) {
                logsList.removeChild(logsList.lastChild);
            }
        } else {
            logsList.appendChild(entry);
        }
    }

    /**
     * Create a log entry DOM element
     */
    function createLogEntryElement(log) {
        const level = (log.level || 'info').toLowerCase();
        const timestamp = formatTimestamp(log.timestamp);
        const hasError = log.level === 'error' || log.level === 'fatal';

        const entry = createElement('div', {
            className: 'log-entry' + (hasError ? ' has-error' : '') + (state.selectedLog === log ? ' selected' : ''),
            'data-log-id': log.id || ''
        });

        // Level badge
        const levelBadge = createElement('span', {
            className: 'log-level level-' + level,
            textContent: level.toUpperCase()
        });
        levelBadge.style.backgroundColor = LEVEL_BG_COLORS[level] || LEVEL_BG_COLORS.info;
        levelBadge.style.color = LEVEL_COLORS[level] || LEVEL_COLORS.info;

        // Timestamp
        const timeSpan = createElement('span', {
            className: 'log-timestamp',
            textContent: timestamp
        });

        // Route/Backend info
        const routeSpan = createElement('span', {
            className: 'log-route',
            textContent: log.route || log.backend || '-'
        });

        // Status code
        const statusSpan = createElement('span', {
            className: 'log-status status-' + getStatusCategory(log.status_code),
            textContent: log.status_code || '-'
        });

        // Message
        const messageSpan = createElement('span', {
            className: 'log-message',
            textContent: log.message || ''
        });

        entry.appendChild(levelBadge);
        entry.appendChild(timeSpan);
        entry.appendChild(routeSpan);
        entry.appendChild(statusSpan);
        entry.appendChild(messageSpan);

        // Click to view details
        entry.addEventListener('click', () => selectLog(log, entry));

        return entry;
    }

    /**
     * Get status category (2xx, 3xx, etc.)
     */
    function getStatusCategory(status) {
        if (!status) return 'none';
        if (status >= 200 && status < 300) return 'success';
        if (status >= 300 && status < 400) return 'redirect';
        if (status >= 400 && status < 500) return 'client-error';
        if (status >= 500) return 'server-error';
        return 'unknown';
    }

    /**
     * Format timestamp for display
     */
    function formatTimestamp(timestamp) {
        const date = new Date(timestamp);
        const hours = date.getHours().toString().padStart(2, '0');
        const minutes = date.getMinutes().toString().padStart(2, '0');
        const seconds = date.getSeconds().toString().padStart(2, '0');
        const millis = date.getMilliseconds().toString().padStart(3, '0');
        return hours + ':' + minutes + ':' + seconds + '.' + millis;
    }

    /**
     * Select a log entry and show details
     */
    function selectLog(log, element) {
        state.selectedLog = log;

        // Update selection UI
        document.querySelectorAll('.log-entry').forEach(el => {
            el.classList.remove('selected');
        });
        if (element) {
            element.classList.add('selected');
        }

        // Show details
        showLogDetails(log);
    }

    /**
     * Show log details in the detail panel
     */
    function showLogDetails(log) {
        const detailPanel = document.getElementById('log-detail');
        if (!detailPanel) return;

        // Clear panel safely
        while (detailPanel.firstChild) {
            detailPanel.removeChild(detailPanel.firstChild);
        }

        const header = createElement('div', { className: 'detail-header' });
        const level = (log.level || 'info').toLowerCase();
        const levelBadge = createElement('span', {
            className: 'detail-level level-' + level,
            textContent: level.toUpperCase()
        });
        const timestamp = createElement('span', {
            className: 'detail-timestamp',
            textContent: new Date(log.timestamp).toISOString()
        });
        header.appendChild(levelBadge);
        header.appendChild(timestamp);

        const message = createElement('div', { className: 'detail-message' });
        const messageLabel = createElement('label', { textContent: 'Message' });
        const messageText = createElement('pre', { textContent: log.message || '' });
        message.appendChild(messageLabel);
        message.appendChild(messageText);

        detailPanel.appendChild(header);
        detailPanel.appendChild(message);

        // Fields
        if (log.fields && Object.keys(log.fields).length > 0) {
            const fieldsSection = createElement('div', { className: 'detail-section' });
            const fieldsLabel = createElement('label', { textContent: 'Fields' });
            const fieldsTable = createElement('table', { className: 'detail-table' });

            Object.entries(log.fields).forEach(([key, value]) => {
                const row = createElement('tr');
                const keyCell = createElement('td', { className: 'field-key', textContent: key });
                const valueCell = createElement('td', { className: 'field-value' });

                if (typeof value === 'object') {
                    valueCell.appendChild(createElement('pre', {
                        textContent: JSON.stringify(value, null, 2)
                    }));
                } else {
                    valueCell.textContent = String(value);
                }

                row.appendChild(keyCell);
                row.appendChild(valueCell);
                fieldsTable.appendChild(row);
            });

            fieldsSection.appendChild(fieldsLabel);
            fieldsSection.appendChild(fieldsTable);
            detailPanel.appendChild(fieldsSection);
        }

        // Request details
        if (log.request) {
            const requestSection = createElement('div', { className: 'detail-section' });
            const requestLabel = createElement('label', { textContent: 'Request' });
            const requestPre = createElement('pre', {
                textContent: JSON.stringify(log.request, null, 2)
            });
            requestSection.appendChild(requestLabel);
            requestSection.appendChild(requestPre);
            detailPanel.appendChild(requestSection);
        }

        // Response details
        if (log.response) {
            const responseSection = createElement('div', { className: 'detail-section' });
            const responseLabel = createElement('label', { textContent: 'Response' });
            const responsePre = createElement('pre', {
                textContent: JSON.stringify(log.response, null, 2)
            });
            responseSection.appendChild(responseLabel);
            responseSection.appendChild(responsePre);
            detailPanel.appendChild(responseSection);
        }

        // Stack trace for errors
        if (log.stack_trace) {
            const stackSection = createElement('div', { className: 'detail-section' });
            const stackLabel = createElement('label', { textContent: 'Stack Trace' });
            const stackPre = createElement('pre', {
                className: 'stack-trace',
                textContent: log.stack_trace
            });
            stackSection.appendChild(stackLabel);
            stackSection.appendChild(stackPre);
            detailPanel.appendChild(stackSection);
        }
    }

    /**
     * Update statistics display
     */
    function updateStats() {
        const totalEl = document.getElementById('stats-total');
        const filteredEl = document.getElementById('stats-filtered');

        if (totalEl) {
            totalEl.textContent = 'Total: ' + state.logs.length.toLocaleString();
        }
        if (filteredEl) {
            filteredEl.textContent = 'Filtered: ' + state.filteredLogs.length.toLocaleString();
        }
    }

    /**
     * Clear all filters
     */
    function clearFilters() {
        state.filters = {
            level: 'all',
            route: '',
            backend: '',
            statusCode: '',
            search: '',
            startTime: null,
            endTime: null
        };

        // Reset UI
        const searchInput = document.getElementById('log-search');
        const levelSelect = document.getElementById('filter-level');
        const routeInput = document.getElementById('filter-route');
        const backendInput = document.getElementById('filter-backend');
        const statusSelect = document.getElementById('filter-status');
        const startInput = document.getElementById('filter-start');
        const endInput = document.getElementById('filter-end');

        if (searchInput) searchInput.value = '';
        if (levelSelect) levelSelect.value = 'all';
        if (routeInput) routeInput.value = '';
        if (backendInput) backendInput.value = '';
        if (statusSelect) statusSelect.value = '';
        if (startInput) startInput.value = '';
        if (endInput) endInput.value = '';

        applyFilters();
    }

    /**
     * Handle scroll for infinite loading
     */
    function handleScroll() {
        const container = document.getElementById('logs-container');
        if (!container) return;

        const scrollTop = container.scrollTop;
        const scrollHeight = container.scrollHeight;
        const clientHeight = container.clientHeight;

        // Load more when near bottom
        if (scrollTop + clientHeight >= scrollHeight - 100) {
            if (state.hasMore && !state.isLoading) {
                loadHistoricalLogs();
            }
        }
    }

    /**
     * Disconnect WebSocket
     */
    function disconnect() {
        if (state.ws) {
            state.ws.close();
            state.ws = null;
        }
        clearTimeout(state.debounceTimer);
    }

    // Initialize when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }

    // Expose to global scope
    window.LogsPage = { init, disconnect };
})();

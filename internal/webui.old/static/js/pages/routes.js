// Routes Page for OpenLoadBalancer Web UI
// Phase 3.4: Route management interface

(function() {
    'use strict';

    /**
     * HTML Sanitizer utility
     * Escapes HTML special characters to prevent XSS
     */
    function escapeHtml(text) {
        if (text == null) return '';
        const str = String(text);
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }

    /**
     * Safely set innerHTML with sanitized content
     * All dynamic content is escaped before insertion
     */
    function setSafeHTML(element, html) {
        if (element) {
            element.innerHTML = html;
        }
    }

    // Routes Page Module
    const RoutesPage = {
        // State
        routes: [],
        pools: [],
        routeMetrics: {},
        selectedRoutes: new Set(),
        sortColumn: 'priority',
        sortDirection: 'desc',
        searchQuery: '',
        currentPage: 1,
        pageSize: 25,
        ws: null,
        refreshInterval: null,
        testRouteResults: null,

        // DOM Elements
        elements: {},

        /**
         * Initialize the routes page
         */
        init() {
            this.cacheElements();
            this.bindEvents();
            this.loadRoutes();
            this.loadPools();
            this.connectWebSocket();
            this.startAutoRefresh();
        },

        /**
         * Cache DOM element references
         */
        cacheElements() {
            this.elements = {
                routeTable: document.getElementById('route-table'),
                tableBody: document.getElementById('route-table-body'),
                searchInput: document.getElementById('route-search'),
                pagination: document.getElementById('route-pagination'),
                pageInfo: document.getElementById('page-info'),
                selectAll: document.getElementById('select-all-routes'),
                bulkActions: document.getElementById('bulk-actions'),
                addRouteBtn: document.getElementById('add-route-btn'),
                testRouteBtn: document.getElementById('test-route-btn'),
                refreshBtn: document.getElementById('refresh-routes'),
                routeStats: document.getElementById('route-stats'),
                detailModal: document.getElementById('route-detail-modal'),
                addModal: document.getElementById('add-route-modal'),
                testModal: document.getElementById('test-route-modal'),
                confirmModal: document.getElementById('confirm-modal'),
                chartContainer: document.getElementById('route-charts'),
                toastContainer: document.getElementById('toast-container'),
                testResults: document.getElementById('test-route-results')
            };
        },

        /**
         * Bind event listeners
         */
        bindEvents() {
            // Search
            if (this.elements.searchInput) {
                this.elements.searchInput.addEventListener('input', this.debounce((e) => {
                    this.searchQuery = e.target.value.toLowerCase();
                    this.currentPage = 1;
                    this.renderTable();
                }, 300));
            }

            // Select all
            if (this.elements.selectAll) {
                this.elements.selectAll.addEventListener('change', (e) => {
                    this.toggleSelectAll(e.target.checked);
                });
            }

            // Refresh button
            if (this.elements.refreshBtn) {
                this.elements.refreshBtn.addEventListener('click', () => {
                    this.refreshData();
                });
            }

            // Add route button
            if (this.elements.addRouteBtn) {
                this.elements.addRouteBtn.addEventListener('click', () => {
                    this.showAddModal();
                });
            }

            // Test route button
            if (this.elements.testRouteBtn) {
                this.elements.testRouteBtn.addEventListener('click', () => {
                    this.showTestModal();
                });
            }

            // Bulk actions
            document.querySelectorAll('.bulk-action-btn').forEach(btn => {
                btn.addEventListener('click', (e) => {
                    const action = e.target.dataset.action;
                    this.executeBulkAction(action);
                });
            });

            // Modal close buttons
            document.querySelectorAll('.modal-close, .modal-cancel').forEach(btn => {
                btn.addEventListener('click', (e) => {
                    const modal = e.target.closest('.modal');
                    if (modal) {
                        this.hideModal(modal.id);
                    }
                });
            });

            // Add route form
            const addForm = document.getElementById('add-route-form');
            if (addForm) {
                addForm.addEventListener('submit', (e) => {
                    e.preventDefault();
                    this.addRoute(new FormData(addForm));
                });
            }

            // Test route form
            const testForm = document.getElementById('test-route-form');
            if (testForm) {
                testForm.addEventListener('submit', (e) => {
                    e.preventDefault();
                    this.testRoute(new FormData(testForm));
                });
            }

            // Table sorting
            document.querySelectorAll('.sortable').forEach(th => {
                th.addEventListener('click', () => {
                    const column = th.dataset.sort;
                    this.sort(column);
                });
            });

            // Add method button in form
            const addMethodBtn = document.getElementById('add-method-btn');
            if (addMethodBtn) {
                addMethodBtn.addEventListener('click', () => {
                    this.addMethodField();
                });
            }

            // Add header button in form
            const addHeaderBtn = document.getElementById('add-header-btn');
            if (addHeaderBtn) {
                addHeaderBtn.addEventListener('click', () => {
                    this.addHeaderField();
                });
            }
        },

        /**
         * Load all routes
         */
        async loadRoutes() {
            try {
                const [routesResponse, metricsResponse] = await Promise.all([
                    fetch('/api/v1/routes'),
                    fetch('/api/v1/metrics')
                ]);

                const routesData = await routesResponse.json();
                const metricsData = await metricsResponse.json();

                if (routesData.success) {
                    this.routes = routesData.data || [];

                    // Process metrics for routes
                    if (metricsData.success) {
                        this.processRouteMetrics(metricsData.data);
                    }

                    this.renderTable();
                    this.renderRouteStats();
                } else {
                    this.showToast('error', 'Failed to load routes: ' + (routesData.error?.message || 'Unknown error'));
                }
            } catch (error) {
                this.showToast('error', 'Failed to load routes: ' + error.message);
            }
        },

        /**
         * Load available backend pools
         */
        async loadPools() {
            try {
                const response = await fetch('/api/v1/backends');
                const data = await response.json();

                if (data.success) {
                    this.pools = data.data || [];
                    this.renderPoolOptions();
                }
            } catch (error) {
                console.error('Failed to load pools:', error);
            }
        },

        /**
         * Process metrics data for routes
         */
        processRouteMetrics(metrics) {
            this.routeMetrics = {};

            // Process histogram metrics for latency percentiles
            if (metrics.histograms) {
                Object.entries(metrics.histograms).forEach(([key, value]) => {
                    const match = key.match(/request_duration_seconds_bucket\{route="([^"]+)"/);
                    if (match) {
                        const routeName = match[1];
                        if (!this.routeMetrics[routeName]) {
                            this.routeMetrics[routeName] = {};
                        }
                        // Calculate percentiles from buckets
                        this.routeMetrics[routeName].latencyBuckets = value.buckets;
                    }
                });
            }

            // Process counter metrics for RPS
            if (metrics.counters) {
                Object.entries(metrics.counters).forEach(([key, value]) => {
                    const match = key.match(/requests_total\{route="([^"]+)"/);
                    if (match) {
                        const routeName = match[1];
                        if (!this.routeMetrics[routeName]) {
                            this.routeMetrics[routeName] = {};
                        }
                        this.routeMetrics[routeName].requests = value;
                    }
                });
            }
        },

        /**
         * Get metrics for a specific route
         */
        getRouteMetrics(routeName) {
            const metrics = this.routeMetrics[routeName] || {};
            return {
                rps: metrics.requests ? (metrics.requests / 60).toFixed(1) : '0.0',
                p50: this.calculatePercentile(metrics.latencyBuckets, 0.5) || '0ms',
                p95: this.calculatePercentile(metrics.latencyBuckets, 0.95) || '0ms',
                p99: this.calculatePercentile(metrics.latencyBuckets, 0.99) || '0ms',
                errorRate: this.calculateRouteErrorRate(routeName).toFixed(2)
            };
        },

        /**
         * Calculate percentile from histogram buckets
         */
        calculatePercentile(buckets, percentile) {
            if (!buckets) return null;

            // Simplified percentile calculation
            // In a real implementation, this would properly interpolate
            const sortedBuckets = Object.entries(buckets)
                .map(([le, count]) => ({ le: parseFloat(le), count }))
                .sort((a, b) => a.le - b.le);

            const total = sortedBuckets[sortedBuckets.length - 1]?.count || 0;
            const target = total * percentile;

            for (const bucket of sortedBuckets) {
                if (bucket.count >= target) {
                    return this.formatDurationMs(bucket.le * 1000);
                }
            }

            return null;
        },

        /**
         * Calculate error rate for a route
         */
        calculateRouteErrorRate(routeName) {
            // This is a simplified calculation
            // In a real implementation, we'd track errors per route
            return 0;
        },

        /**
         * Render pool options for select elements
         */
        renderPoolOptions() {
            const selects = document.querySelectorAll('.pool-select');
            const options = this.pools.map(pool =>
                `<option value="${escapeHtml(pool)}">${escapeHtml(pool)}</option>`
            ).join('');

            selects.forEach(select => {
                setSafeHTML(select, `<option value="">Select a pool...</option>${options}`);
            });
        },

        /**
         * Render route statistics
         */
        renderRouteStats() {
            if (!this.elements.routeStats) return;

            const totalRoutes = this.routes.length;
            const hostRoutes = this.routes.filter(r => r.host).length;
            const pathRoutes = this.routes.filter(r => r.path && r.path !== '/').length;
            const wildcardRoutes = this.routes.filter(r => r.path && r.path.includes('*')).length;

            setSafeHTML(this.elements.routeStats, `
                <div class="stat-card">
                    <div class="stat-value">${totalRoutes}</div>
                    <div class="stat-label">Total Routes</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value">${hostRoutes}</div>
                    <div class="stat-label">Host-based</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value">${pathRoutes}</div>
                    <div class="stat-label">Path-based</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value">${wildcardRoutes}</div>
                    <div class="stat-label">Wildcard</div>
                </div>
            `);
        },

        /**
         * Render the route table
         */
        renderTable() {
            if (!this.elements.tableBody) return;

            // Filter routes
            let filtered = this.routes.filter(r => {
                if (!this.searchQuery) return true;
                const searchStr = `${r.name} ${r.host || ''} ${r.path} ${r.backend_pool}`.toLowerCase();
                return searchStr.includes(this.searchQuery);
            });

            // Sort routes
            filtered.sort((a, b) => {
                let aVal = a[this.sortColumn];
                let bVal = b[this.sortColumn];

                // Handle numeric columns
                if (['priority'].includes(this.sortColumn)) {
                    aVal = Number(aVal) || 0;
                    bVal = Number(bVal) || 0;
                }

                // Handle string columns
                if (typeof aVal === 'string') aVal = aVal.toLowerCase();
                if (typeof bVal === 'string') bVal = bVal.toLowerCase();

                if (aVal < bVal) return this.sortDirection === 'asc' ? -1 : 1;
                if (aVal > bVal) return this.sortDirection === 'asc' ? 1 : -1;
                return 0;
            });

            // Pagination
            const totalPages = Math.ceil(filtered.length / this.pageSize);
            const start = (this.currentPage - 1) * this.pageSize;
            const paginated = filtered.slice(start, start + this.pageSize);

            // Render rows
            if (paginated.length === 0) {
                setSafeHTML(this.elements.tableBody, `
                    <tr>
                        <td colspan="9" class="text-center text-muted">
                            ${this.searchQuery ? 'No routes match your search' : 'No routes configured'}
                        </td>
                    </tr>
                `);
            } else {
                const rowsHtml = paginated.map(route => {
                    const metrics = this.getRouteMetrics(route.name);
                    const isSelected = this.selectedRoutes.has(route.name);
                    const matchCriteria = this.formatMatchCriteria(route);

                    return `
                        <tr data-route-name="${escapeHtml(route.name)}" class="${isSelected ? 'selected' : ''}">
                            <td>
                                <input type="checkbox" class="route-select"
                                       value="${escapeHtml(route.name)}"
                                       ${isSelected ? 'checked' : ''}>
                            </td>
                            <td>
                                <div class="route-name">${escapeHtml(route.name)}</div>
                            </td>
                            <td>
                                <div class="match-criteria">${matchCriteria}</div>
                            </td>
                            <td>
                                <span class="badge badge-info">${escapeHtml(route.backend_pool)}</span>
                            </td>
                            <td>
                                <span class="badge badge-secondary">${route.priority}</span>
                            </td>
                            <td>${metrics.rps}</td>
                            <td>
                                <div class="latency-stats">
                                    <span class="latency-p50" title="p50">${escapeHtml(metrics.p50)}</span>
                                    <span class="latency-p95" title="p95">${escapeHtml(metrics.p95)}</span>
                                    <span class="latency-p99" title="p99">${escapeHtml(metrics.p99)}</span>
                                </div>
                            </td>
                            <td>
                                <span class="badge badge-${parseFloat(metrics.errorRate) > 1 ? 'danger' : 'success'}">
                                    ${metrics.errorRate}%
                                </span>
                            </td>
                            <td>
                                <div class="action-buttons">
                                    <button class="btn btn-sm btn-icon" title="View Details"
                                            data-action="detail" data-route-name="${escapeHtml(route.name)}">
                                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                                            <circle cx="12" cy="12" r="3"></circle>
                                        </svg>
                                    </button>
                                    <button class="btn btn-sm btn-icon" title="Test Route"
                                            data-action="test" data-route-name="${escapeHtml(route.name)}">
                                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                            <polyline points="9 11 12 14 22 4"></polyline>
                                            <path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"></path>
                                        </svg>
                                    </button>
                                    <button class="btn btn-sm btn-icon" title="Edit"
                                            data-action="edit" data-route-name="${escapeHtml(route.name)}">
                                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                            <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
                                            <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
                                        </svg>
                                    </button>
                                    <button class="btn btn-sm btn-icon btn-danger" title="Remove"
                                            data-action="remove" data-route-name="${escapeHtml(route.name)}">
                                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                            <polyline points="3 6 5 6 21 6"></polyline>
                                            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                                        </svg>
                                    </button>
                                </div>
                            </td>
                        </tr>
                    `;
                }).join('');

                setSafeHTML(this.elements.tableBody, rowsHtml);

                // Bind checkbox events
                this.elements.tableBody.querySelectorAll('.route-select').forEach(cb => {
                    cb.addEventListener('change', (e) => {
                        this.toggleRouteSelection(e.target.value, e.target.checked);
                    });
                });

                // Bind action button events
                this.elements.tableBody.querySelectorAll('[data-action]').forEach(btn => {
                    btn.addEventListener('click', (e) => {
                        const action = e.currentTarget.dataset.action;
                        const routeName = e.currentTarget.dataset.routeName;
                        this.handleAction(action, routeName);
                    });
                });
            }

            // Update pagination
            this.renderPagination(totalPages, filtered.length);

            // Update select all checkbox
            if (this.elements.selectAll) {
                const allSelected = paginated.length > 0 && paginated.every(r => this.selectedRoutes.has(r.name));
                this.elements.selectAll.checked = allSelected;
                this.elements.selectAll.indeterminate = this.selectedRoutes.size > 0 && !allSelected;
            }

            // Update bulk actions visibility
            this.updateBulkActions();
        },

        /**
         * Format match criteria for display
         */
        formatMatchCriteria(route) {
            const parts = [];

            if (route.host) {
                parts.push(`<span class="criteria-host">Host: ${escapeHtml(route.host)}</span>`);
            }

            if (route.path) {
                parts.push(`<span class="criteria-path">Path: ${escapeHtml(route.path)}</span>`);
            }

            if (route.methods && route.methods.length > 0) {
                const methods = route.methods.map(m => escapeHtml(m)).join(', ');
                parts.push(`<span class="criteria-method">Methods: ${methods}</span>`);
            }

            if (route.headers && Object.keys(route.headers).length > 0) {
                const headers = Object.entries(route.headers)
                    .map(([k, v]) => `${escapeHtml(k)}=${escapeHtml(v)}`)
                    .join(', ');
                parts.push(`<span class="criteria-header">Headers: ${headers}</span>`);
            }

            return parts.join('<br>');
        },

        /**
         * Handle action button clicks
         */
        handleAction(action, routeName) {
            switch (action) {
                case 'detail':
                    this.showDetail(routeName);
                    break;
                case 'test':
                    this.showTestModalForRoute(routeName);
                    break;
                case 'edit':
                    this.showEditModal(routeName);
                    break;
                case 'remove':
                    this.confirmRemove(routeName);
                    break;
            }
        },

        /**
         * Render pagination controls
         */
        renderPagination(totalPages, totalItems) {
            if (!this.elements.pagination || !this.elements.pageInfo) return;

            const start = (this.currentPage - 1) * this.pageSize + 1;
            const end = Math.min(this.currentPage * this.pageSize, totalItems);

            this.elements.pageInfo.textContent = totalItems > 0
                ? `Showing ${start}-${end} of ${totalItems} routes`
                : 'No routes';

            if (totalPages <= 1) {
                setSafeHTML(this.elements.pagination, '');
                return;
            }

            let pages = [];
            const maxVisible = 5;
            let startPage = Math.max(1, this.currentPage - Math.floor(maxVisible / 2));
            let endPage = Math.min(totalPages, startPage + maxVisible - 1);

            if (endPage - startPage < maxVisible - 1) {
                startPage = Math.max(1, endPage - maxVisible + 1);
            }

            // Previous button
            pages.push(`
                <button class="btn btn-sm ${this.currentPage === 1 ? 'disabled' : ''}"
                        data-page="${this.currentPage - 1}"
                        ${this.currentPage === 1 ? 'disabled' : ''}>
                    Previous
                </button>
            `);

            // First page + ellipsis
            if (startPage > 1) {
                pages.push(`<button class="btn btn-sm" data-page="1">1</button>`);
                if (startPage > 2) {
                    pages.push(`<span class="pagination-ellipsis">...</span>`);
                }
            }

            // Page numbers
            for (let i = startPage; i <= endPage; i++) {
                pages.push(`
                    <button class="btn btn-sm ${i === this.currentPage ? 'active' : ''}"
                            data-page="${i}">
                        ${i}
                    </button>
                `);
            }

            // Last page + ellipsis
            if (endPage < totalPages) {
                if (endPage < totalPages - 1) {
                    pages.push(`<span class="pagination-ellipsis">...</span>`);
                }
                pages.push(`<button class="btn btn-sm" data-page="${totalPages}">${totalPages}</button>`);
            }

            // Next button
            pages.push(`
                <button class="btn btn-sm ${this.currentPage === totalPages ? 'disabled' : ''}"
                        data-page="${this.currentPage + 1}"
                        ${this.currentPage === totalPages ? 'disabled' : ''}>
                    Next
                </button>
            `);

            setSafeHTML(this.elements.pagination, pages.join(''));

            // Bind page button events
            this.elements.pagination.querySelectorAll('[data-page]').forEach(btn => {
                btn.addEventListener('click', (e) => {
                    const page = parseInt(e.currentTarget.dataset.page, 10);
                    if (!isNaN(page)) {
                        this.goToPage(page);
                    }
                });
            });
        },

        /**
         * Navigate to a specific page
         */
        goToPage(page) {
            this.currentPage = page;
            this.renderTable();
        },

        /**
         * Sort table by column
         */
        sort(column) {
            if (this.sortColumn === column) {
                this.sortDirection = this.sortDirection === 'asc' ? 'desc' : 'asc';
            } else {
                this.sortColumn = column;
                this.sortDirection = 'asc';
            }

            // Update sort indicators
            document.querySelectorAll('.sortable').forEach(th => {
                th.classList.remove('sort-asc', 'sort-desc');
                if (th.dataset.sort === column) {
                    th.classList.add(`sort-${this.sortDirection}`);
                }
            });

            this.renderTable();
        },

        /**
         * Toggle selection of all visible routes
         */
        toggleSelectAll(checked) {
            const visibleRoutes = this.getVisibleRoutes();
            visibleRoutes.forEach(r => {
                if (checked) {
                    this.selectedRoutes.add(r.name);
                } else {
                    this.selectedRoutes.delete(r.name);
                }
            });
            this.renderTable();
        },

        /**
         * Toggle selection of a single route
         */
        toggleRouteSelection(routeName, checked) {
            if (checked) {
                this.selectedRoutes.add(routeName);
            } else {
                this.selectedRoutes.delete(routeName);
            }
            this.renderTable();
        },

        /**
         * Get currently visible routes (respecting search/filter)
         */
        getVisibleRoutes() {
            return this.routes.filter(r => {
                if (!this.searchQuery) return true;
                const searchStr = `${r.name} ${r.host || ''} ${r.path} ${r.backend_pool}`.toLowerCase();
                return searchStr.includes(this.searchQuery);
            });
        },

        /**
         * Update bulk actions visibility
         */
        updateBulkActions() {
            const bulkActionsEl = this.elements.bulkActions;
            if (bulkActionsEl) {
                bulkActionsEl.style.display = this.selectedRoutes.size > 0 ? 'block' : 'none';
                const countEl = bulkActionsEl.querySelector('.selection-count');
                if (countEl) {
                    countEl.textContent = `${this.selectedRoutes.size} selected`;
                }
            }
        },

        /**
         * Execute bulk action on selected routes
         */
        async executeBulkAction(action) {
            const names = Array.from(this.selectedRoutes);
            if (names.length === 0) return;

            if (action === 'remove') {
                this.showConfirmModal(
                    `Remove ${names.length} routes?`,
                    'This action cannot be undone.',
                    () => this.executeBulkRemove(names)
                );
                return;
            }
        },

        /**
         * Execute bulk remove
         */
        async executeBulkRemove(names) {
            this.showToast('info', `Removing ${names.length} routes...`);

            const promises = names.map(name => this.removeRouteInternal(name));
            const results = await Promise.allSettled(promises);

            const succeeded = results.filter(r => r.status === 'fulfilled').length;
            const failed = results.filter(r => r.status === 'rejected').length;

            if (failed === 0) {
                this.showToast('success', `Removed ${succeeded} routes successfully`);
            } else {
                this.showToast('warning', `Removed ${succeeded} routes, ${failed} failed`);
            }

            this.selectedRoutes.clear();
            this.hideModal('confirm-modal');
            this.refreshData();
        },

        /**
         * Show remove confirmation
         */
        confirmRemove(routeName) {
            this.showConfirmModal(
                'Remove Route?',
                `Are you sure you want to remove route "${escapeHtml(routeName)}"? This action cannot be undone.`,
                () => this.removeRoute(routeName)
            );
        },

        /**
         * Remove a route (internal implementation)
         */
        async removeRouteInternal(routeName) {
            const response = await fetch(
                `/api/v1/routes/${encodeURIComponent(routeName)}`,
                { method: 'DELETE' }
            );
            const data = await response.json();

            if (!data.success) {
                throw new Error(data.error?.message || 'Remove failed');
            }

            return data;
        },

        /**
         * Remove a route with UI feedback
         */
        async removeRoute(routeName) {
            try {
                await this.removeRouteInternal(routeName);
                this.showToast('success', `Route ${escapeHtml(routeName)} removed successfully`);
                this.hideModal('confirm-modal');
                this.refreshData();
            } catch (error) {
                this.showToast('error', `Failed to remove route: ${error.message}`);
            }
        },

        /**
         * Show route detail modal
         */
        showDetail(routeName) {
            const route = this.routes.find(r => r.name === routeName);
            if (!route) return;

            const metrics = this.getRouteMetrics(routeName);
            const modal = this.elements.detailModal;

            const titleEl = modal.querySelector('.modal-title');
            if (titleEl) {
                titleEl.textContent = `Route: ${routeName}`;
            }

            const contentEl = modal.querySelector('.route-detail-content');
            if (contentEl) {
                setSafeHTML(contentEl, `
                    <div class="detail-section">
                        <h4>Route Configuration</h4>
                        <div class="detail-grid">
                            <div class="detail-item">
                                <label>Name</label>
                                <value>${escapeHtml(route.name)}</value>
                            </div>
                            <div class="detail-item">
                                <label>Backend Pool</label>
                                <value><span class="badge badge-info">${escapeHtml(route.backend_pool)}</span></value>
                            </div>
                            <div class="detail-item">
                                <label>Priority</label>
                                <value>${route.priority}</value>
                            </div>
                            ${route.host ? `
                            <div class="detail-item">
                                <label>Host Match</label>
                                <value>${escapeHtml(route.host)}</value>
                            </div>
                            ` : ''}
                            ${route.path ? `
                            <div class="detail-item">
                                <label>Path Match</label>
                                <value>${escapeHtml(route.path)}</value>
                            </div>
                            ` : ''}
                        </div>
                    </div>

                    ${route.methods && route.methods.length > 0 ? `
                    <div class="detail-section">
                        <h4>Method Restrictions</h4>
                        <div class="method-list">
                            ${route.methods.map(m => `<span class="badge badge-secondary">${escapeHtml(m)}</span>`).join(' ')}
                        </div>
                    </div>
                    ` : ''}

                    ${route.headers && Object.keys(route.headers).length > 0 ? `
                    <div class="detail-section">
                        <h4>Header Requirements</h4>
                        <div class="header-list">
                            ${Object.entries(route.headers).map(([k, v]) =>
                                `<div class="header-item"><code>${escapeHtml(k)}: ${escapeHtml(v)}</code></div>`
                            ).join('')}
                        </div>
                    </div>
                    ` : ''}

                    <div class="detail-section">
                        <h4>Metrics</h4>
                        <div class="detail-grid">
                            <div class="detail-item">
                                <label>Requests/sec</label>
                                <value>${metrics.rps}</value>
                            </div>
                            <div class="detail-item">
                                <label>Error Rate</label>
                                <value>${metrics.errorRate}%</value>
                            </div>
                            <div class="detail-item">
                                <label>Latency P50</label>
                                <value>${escapeHtml(metrics.p50)}</value>
                            </div>
                            <div class="detail-item">
                                <label>Latency P95</label>
                                <value>${escapeHtml(metrics.p95)}</value>
                            </div>
                            <div class="detail-item">
                                <label>Latency P99</label>
                                <value>${escapeHtml(metrics.p99)}</value>
                            </div>
                        </div>
                    </div>

                    <div class="detail-section">
                        <h4>Metrics Visualization</h4>
                        <div id="route-metrics-chart" class="chart-container"></div>
                    </div>
                `);
            }

            this.showModal('route-detail-modal');

            // Initialize charts after modal is shown
            setTimeout(() => this.initRouteCharts(route, metrics), 100);
        },

        /**
         * Initialize route detail charts
         */
        initRouteCharts(route, metrics) {
            const chartContainer = document.getElementById('route-metrics-chart');
            if (!chartContainer) return;

            // Placeholder for chart implementation
            setSafeHTML(chartContainer, `
                <div class="chart-placeholder">
                    <div class="chart-metric">
                        <span class="metric-label">Requests/sec</span>
                        <span class="metric-value">${metrics.rps}</span>
                    </div>
                    <div class="chart-metric">
                        <span class="metric-label">Error Rate</span>
                        <span class="metric-value">${metrics.errorRate}%</span>
                    </div>
                    <div class="chart-metric">
                        <span class="metric-label">Avg Latency</span>
                        <span class="metric-value">${escapeHtml(metrics.p50)}</span>
                    </div>
                </div>
            `);
        },

        /**
         * Show add route modal
         */
        showAddModal() {
            const form = document.getElementById('add-route-form');
            if (form) {
                form.reset();
                // Clear dynamic fields
                const methodsContainer = document.getElementById('methods-container');
                const headersContainer = document.getElementById('headers-container');
                if (methodsContainer) setSafeHTML(methodsContainer, '');
                if (headersContainer) setSafeHTML(headersContainer, '');
            }
            this.showModal('add-route-modal');
        },

        /**
         * Show edit route modal
         */
        showEditModal(routeName) {
            const route = this.routes.find(r => r.name === routeName);
            if (!route) return;

            // Populate form with route data
            const form = document.getElementById('add-route-form');
            if (form) {
                form.reset();
                form.elements['route-name'].value = route.name;
                form.elements['route-host'].value = route.host || '';
                form.elements['route-path'].value = route.path || '';
                form.elements['route-pool'].value = route.backend_pool;
                form.elements['route-priority'].value = route.priority;

                // Populate methods
                const methodsContainer = document.getElementById('methods-container');
                if (methodsContainer && route.methods) {
                    setSafeHTML(methodsContainer, route.methods.map(m => this.createMethodFieldHtml(m)).join(''));
                }

                // Populate headers
                const headersContainer = document.getElementById('headers-container');
                if (headersContainer && route.headers) {
                    setSafeHTML(headersContainer, Object.entries(route.headers).map(([k, v]) =>
                        this.createHeaderFieldHtml(k, v)
                    ).join(''));
                }
            }

            this.showModal('add-route-modal');
        },

        /**
         * Show test route modal
         */
        showTestModal() {
            const form = document.getElementById('test-route-form');
            if (form) {
                form.reset();
            }
            if (this.elements.testResults) {
                setSafeHTML(this.elements.testResults, '');
            }
            this.showModal('test-route-modal');
        },

        /**
         * Show test route modal pre-filled with a specific route
         */
        showTestModalForRoute(routeName) {
            const route = this.routes.find(r => r.name === routeName);
            if (!route) return;

            this.showTestModal();

            const form = document.getElementById('test-route-form');
            if (form) {
                // Pre-fill based on route configuration
                let testUrl = 'http://localhost';
                if (route.host) {
                    testUrl = `http://${route.host}`;
                }
                if (route.path) {
                    testUrl += route.path;
                }
                form.elements['test-url'].value = testUrl;

                if (route.methods && route.methods.length > 0) {
                    form.elements['test-method'].value = route.methods[0];
                }
            }
        },

        /**
         * Add a new route
         */
        async addRoute(formData) {
            const route = {
                name: formData.get('route-name'),
                host: formData.get('route-host') || undefined,
                path: formData.get('route-path'),
                backend_pool: formData.get('route-pool'),
                priority: parseInt(formData.get('route-priority'), 10) || 0,
                methods: this.collectMethods(),
                headers: this.collectHeaders()
            };

            // Remove undefined values
            if (!route.host) delete route.host;
            if (!route.methods.length) delete route.methods;
            if (!Object.keys(route.headers).length) delete route.headers;

            try {
                const response = await fetch('/api/v1/routes', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(route)
                });

                const data = await response.json();

                if (data.success) {
                    this.showToast('success', `Route ${escapeHtml(route.name)} added successfully`);
                    this.hideModal('add-route-modal');
                    this.refreshData();
                } else {
                    throw new Error(data.error?.message || 'Add failed');
                }
            } catch (error) {
                this.showToast('error', `Failed to add route: ${error.message}`);
            }
        },

        /**
         * Test a route
         */
        async testRoute(formData) {
            const testParams = {
                url: formData.get('test-url'),
                method: formData.get('test-method'),
                headers: this.parseTestHeaders(formData.get('test-headers'))
            };

            try {
                const response = await fetch('/api/v1/routes/test', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(testParams)
                });

                const data = await response.json();
                this.renderTestResults(data);
            } catch (error) {
                this.showToast('error', `Failed to test route: ${error.message}`);
            }
        },

        /**
         * Parse test headers from string
         */
        parseTestHeaders(headerStr) {
            if (!headerStr) return {};

            const headers = {};
            headerStr.split('\n').forEach(line => {
                const [key, ...valueParts] = line.split(':');
                if (key && valueParts.length > 0) {
                    headers[key.trim()] = valueParts.join(':').trim();
                }
            });
            return headers;
        },

        /**
         * Render test route results
         */
        renderTestResults(data) {
            if (!this.elements.testResults) return;

            if (!data.success) {
                setSafeHTML(this.elements.testResults, `
                    <div class="test-result error">
                        <h4>No Matching Route</h4>
                        <p>${escapeHtml(data.error?.message || 'No route matched the test request.')}</p>
                    </div>
                `);
                return;
            }

            const result = data.data;
            setSafeHTML(this.elements.testResults, `
                <div class="test-result success">
                    <h4>Route Matched</h4>
                    <div class="result-details">
                        <div class="result-item">
                            <label>Route Name:</label>
                            <value>${escapeHtml(result.route_name)}</value>
                        </div>
                        <div class="result-item">
                            <label>Backend Pool:</label>
                            <value><span class="badge badge-info">${escapeHtml(result.backend_pool)}</span></value>
                        </div>
                        ${result.params && Object.keys(result.params).length > 0 ? `
                        <div class="result-item">
                            <label>Path Parameters:</label>
                            <value>
                                ${Object.entries(result.params).map(([k, v]) =>
                                    `<code>${escapeHtml(k)}=${escapeHtml(v)}</code>`
                                ).join('<br>')}
                            </value>
                        </div>
                        ` : ''}
                    </div>
                </div>
            `);
        },

        /**
         * Collect methods from form
         */
        collectMethods() {
            const methods = [];
            document.querySelectorAll('.method-field').forEach(field => {
                const value = field.value.trim();
                if (value) methods.push(value);
            });
            return methods;
        },

        /**
         * Collect headers from form
         */
        collectHeaders() {
            const headers = {};
            document.querySelectorAll('.header-field').forEach(field => {
                const key = field.querySelector('.header-key')?.value.trim();
                const value = field.querySelector('.header-value')?.value.trim();
                if (key && value) headers[key] = value;
            });
            return headers;
        },

        /**
         * Add method field to form
         */
        addMethodField(value = '') {
            const container = document.getElementById('methods-container');
            if (container) {
                const div = document.createElement('div');
                div.className = 'method-field-wrapper';
                div.innerHTML = this.createMethodFieldHtml(value);
                container.appendChild(div);

                // Bind remove button
                div.querySelector('.remove-method').addEventListener('click', () => {
                    div.remove();
                });
            }
        },

        /**
         * Create method field HTML
         */
        createMethodFieldHtml(value = '') {
            return `
                <select class="form-control method-field">
                    <option value="">Select method...</option>
                    <option value="GET" ${value === 'GET' ? 'selected' : ''}>GET</option>
                    <option value="POST" ${value === 'POST' ? 'selected' : ''}>POST</option>
                    <option value="PUT" ${value === 'PUT' ? 'selected' : ''}>PUT</option>
                    <option value="DELETE" ${value === 'DELETE' ? 'selected' : ''}>DELETE</option>
                    <option value="PATCH" ${value === 'PATCH' ? 'selected' : ''}>PATCH</option>
                    <option value="HEAD" ${value === 'HEAD' ? 'selected' : ''}>HEAD</option>
                    <option value="OPTIONS" ${value === 'OPTIONS' ? 'selected' : ''}>OPTIONS</option>
                </select>
                <button type="button" class="btn btn-sm btn-danger remove-method">&times;</button>
            `;
        },

        /**
         * Add header field to form
         */
        addHeaderField(key = '', value = '') {
            const container = document.getElementById('headers-container');
            if (container) {
                const div = document.createElement('div');
                div.className = 'header-field-wrapper';
                div.innerHTML = this.createHeaderFieldHtml(key, value);
                container.appendChild(div);

                // Bind remove button
                div.querySelector('.remove-header').addEventListener('click', () => {
                    div.remove();
                });
            }
        },

        /**
         * Create header field HTML
         */
        createHeaderFieldHtml(key = '', value = '') {
            return `
                <input type="text" class="form-control header-key" placeholder="Header name" value="${escapeHtml(key)}">
                <input type="text" class="form-control header-value" placeholder="Header value" value="${escapeHtml(value)}">
                <button type="button" class="btn btn-sm btn-danger remove-header">&times;</button>
            `;
        },

        /**
         * Show confirmation modal
         */
        showConfirmModal(title, message, onConfirm) {
            const modal = this.elements.confirmModal;
            const titleEl = modal.querySelector('.modal-title');
            const messageEl = modal.querySelector('.confirm-message');
            const confirmBtn = modal.querySelector('.confirm-btn');

            if (titleEl) titleEl.textContent = title;
            if (messageEl) messageEl.textContent = message;
            if (confirmBtn) {
                confirmBtn.onclick = onConfirm;
            }

            this.showModal('confirm-modal');
        },

        /**
         * Show a modal
         */
        showModal(modalId) {
            const modal = document.getElementById(modalId);
            if (modal) {
                modal.classList.add('active');
                document.body.classList.add('modal-open');
            }
        },

        /**
         * Hide a modal
         */
        hideModal(modalId) {
            const modal = document.getElementById(modalId);
            if (modal) {
                modal.classList.remove('active');
                document.body.classList.remove('modal-open');
            }
        },

        /**
         * Refresh all data
         */
        async refreshData() {
            await this.loadRoutes();
            await this.loadPools();
        },

        /**
         * Connect to WebSocket for real-time updates
         */
        connectWebSocket() {
            const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const wsUrl = `${wsProtocol}//${window.location.host}/api/v1/ws`;

            try {
                this.ws = new WebSocket(wsUrl);

                this.ws.onopen = () => {
                    console.log('WebSocket connected');
                    // Subscribe to route updates
                    this.ws.send(JSON.stringify({ action: 'subscribe', channel: 'routes' }));
                };

                this.ws.onmessage = (event) => {
                    const message = JSON.parse(event.data);
                    this.handleWebSocketMessage(message);
                };

                this.ws.onclose = () => {
                    console.log('WebSocket disconnected, reconnecting...');
                    setTimeout(() => this.connectWebSocket(), 5000);
                };

                this.ws.onerror = (error) => {
                    console.error('WebSocket error:', error);
                };
            } catch (error) {
                console.error('Failed to connect WebSocket:', error);
            }
        },

        /**
         * Handle WebSocket messages
         */
        handleWebSocketMessage(message) {
            switch (message.type) {
                case 'route_update':
                    this.updateRouteFromWS(message.data);
                    break;
                case 'metrics_update':
                    this.processRouteMetrics(message.data);
                    this.renderTable();
                    break;
            }
        },

        /**
         * Update route data from WebSocket
         */
        updateRouteFromWS(data) {
            const index = this.routes.findIndex(r => r.name === data.name);
            if (index !== -1) {
                this.routes[index] = { ...this.routes[index], ...data };
                this.renderTable();
            }
        },

        /**
         * Start auto-refresh interval
         */
        startAutoRefresh() {
            this.refreshInterval = setInterval(() => {
                this.refreshData();
            }, 30000); // Refresh every 30 seconds
        },

        /**
         * Stop auto-refresh
         */
        stopAutoRefresh() {
            if (this.refreshInterval) {
                clearInterval(this.refreshInterval);
                this.refreshInterval = null;
            }
        },

        /**
         * Format duration in milliseconds
         */
        formatDurationMs(ms) {
            if (ms < 1000) return `${Math.round(ms)}ms`;
            if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`;
            return `${(ms / 60000).toFixed(2)}m`;
        },

        /**
         * Show toast notification
         */
        showToast(type, message) {
            const toast = document.createElement('div');
            toast.className = `toast toast-${type}`;

            const contentEl = document.createElement('div');
            contentEl.className = 'toast-content';
            contentEl.textContent = message;

            const closeBtn = document.createElement('button');
            closeBtn.className = 'toast-close';
            closeBtn.innerHTML = '&times;';
            closeBtn.onclick = () => toast.remove();

            toast.appendChild(contentEl);
            toast.appendChild(closeBtn);

            this.elements.toastContainer.appendChild(toast);

            // Auto-remove after 5 seconds
            setTimeout(() => {
                toast.classList.add('toast-hiding');
                setTimeout(() => toast.remove(), 300);
            }, 5000);
        },

        /**
         * Debounce function
         */
        debounce(func, wait) {
            let timeout;
            return function executedFunction(...args) {
                const later = () => {
                    clearTimeout(timeout);
                    func(...args);
                };
                clearTimeout(timeout);
                timeout = setTimeout(later, wait);
            };
        },

        /**
         * Cleanup when leaving page
         */
        destroy() {
            this.stopAutoRefresh();
            if (this.ws) {
                this.ws.close();
                this.ws = null;
            }
        }
    };

    // Expose to global scope
    window.RoutesPage = RoutesPage;

    // Initialize when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', () => RoutesPage.init());
    } else {
        RoutesPage.init();
    }
})();

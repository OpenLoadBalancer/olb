// Backends Page for OpenLoadBalancer Web UI
// Phase 3.3: Backend pool management interface

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

    // Backends Page Module
    const BackendsPage = {
        // State
        pools: [],
        backends: [],
        healthStatuses: {},
        selectedBackends: new Set(),
        currentPool: null,
        sortColumn: 'name',
        sortDirection: 'asc',
        searchQuery: '',
        currentPage: 1,
        pageSize: 25,
        ws: null,
        refreshInterval: null,

        // DOM Elements
        elements: {},

        /**
         * Initialize the backends page
         */
        init() {
            this.cacheElements();
            this.bindEvents();
            this.loadPools();
            this.connectWebSocket();
            this.startAutoRefresh();
        },

        /**
         * Cache DOM element references
         */
        cacheElements() {
            this.elements = {
                poolSelect: document.getElementById('pool-select'),
                backendTable: document.getElementById('backend-table'),
                tableBody: document.getElementById('backend-table-body'),
                searchInput: document.getElementById('backend-search'),
                pagination: document.getElementById('backend-pagination'),
                pageInfo: document.getElementById('page-info'),
                selectAll: document.getElementById('select-all-backends'),
                bulkActions: document.getElementById('bulk-actions'),
                addBackendBtn: document.getElementById('add-backend-btn'),
                refreshBtn: document.getElementById('refresh-backends'),
                poolStats: document.getElementById('pool-stats'),
                detailModal: document.getElementById('backend-detail-modal'),
                addModal: document.getElementById('add-backend-modal'),
                confirmModal: document.getElementById('confirm-modal'),
                chartContainer: document.getElementById('backend-charts'),
                toastContainer: document.getElementById('toast-container')
            };
        },

        /**
         * Bind event listeners
         */
        bindEvents() {
            // Pool selection
            if (this.elements.poolSelect) {
                this.elements.poolSelect.addEventListener('change', (e) => {
                    this.currentPool = e.target.value;
                    this.loadPoolDetails(this.currentPool);
                });
            }

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

            // Add backend button
            if (this.elements.addBackendBtn) {
                this.elements.addBackendBtn.addEventListener('click', () => {
                    this.showAddModal();
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

            // Add backend form
            const addForm = document.getElementById('add-backend-form');
            if (addForm) {
                addForm.addEventListener('submit', (e) => {
                    e.preventDefault();
                    this.addBackend(new FormData(addForm));
                });
            }

            // Table sorting
            document.querySelectorAll('.sortable').forEach(th => {
                th.addEventListener('click', () => {
                    const column = th.dataset.sort;
                    this.sort(column);
                });
            });
        },

        /**
         * Load all backend pools
         */
        async loadPools() {
            try {
                const response = await fetch('/api/v1/backends');
                const data = await response.json();

                if (data.success) {
                    this.pools = data.data || [];
                    this.renderPoolSelect();

                    // Load first pool by default
                    if (this.pools.length > 0 && !this.currentPool) {
                        this.currentPool = this.pools[0];
                        this.loadPoolDetails(this.currentPool);
                    }
                } else {
                    this.showToast('error', 'Failed to load pools: ' + (data.error?.message || 'Unknown error'));
                }
            } catch (error) {
                this.showToast('error', 'Failed to load pools: ' + error.message);
            }
        },

        /**
         * Load details for a specific pool
         */
        async loadPoolDetails(poolName) {
            if (!poolName) return;

            try {
                const [poolResponse, healthResponse] = await Promise.all([
                    fetch(`/api/v1/backends/${encodeURIComponent(poolName)}`),
                    fetch('/api/v1/health')
                ]);

                const poolData = await poolResponse.json();
                const healthData = await healthResponse.json();

                if (poolData.success) {
                    this.backends = poolData.data.backends || [];
                    this.currentPool = poolName;

                    // Update pool stats
                    this.renderPoolStats(poolData.data);

                    // Cache health statuses
                    if (healthData.success) {
                        this.healthStatuses = {};
                        (healthData.data || []).forEach(h => {
                            this.healthStatuses[h.backend_id] = h;
                        });
                    }

                    this.renderTable();
                } else {
                    this.showToast('error', 'Failed to load pool details: ' + (poolData.error?.message || 'Unknown error'));
                }
            } catch (error) {
                this.showToast('error', 'Failed to load pool details: ' + error.message);
            }
        },

        /**
         * Render pool selector dropdown
         */
        renderPoolSelect() {
            if (!this.elements.poolSelect) return;

            const options = this.pools.map(pool =>
                `<option value="${escapeHtml(pool)}" ${pool === this.currentPool ? 'selected' : ''}>
                    ${escapeHtml(pool)}
                </option>`
            );

            setSafeHTML(this.elements.poolSelect, `
                <option value="">Select a pool...</option>
                ${options.join('')}
            `);
        },

        /**
         * Render pool statistics
         */
        renderPoolStats(poolInfo) {
            if (!this.elements.poolStats) return;

            const healthyPercent = poolInfo.total > 0
                ? Math.round((poolInfo.healthy / poolInfo.total) * 100)
                : 0;

            setSafeHTML(this.elements.poolStats, `
                <div class="stat-card">
                    <div class="stat-value">${poolInfo.total}</div>
                    <div class="stat-label">Total Backends</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value text-success">${poolInfo.healthy}</div>
                    <div class="stat-label">Healthy</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value text-danger">${poolInfo.total - poolInfo.healthy}</div>
                    <div class="stat-label">Unhealthy</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value">${escapeHtml(poolInfo.algorithm)}</div>
                    <div class="stat-label">Algorithm</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value">${healthyPercent}%</div>
                    <div class="stat-label">Health Rate</div>
                </div>
            `);
        },

        /**
         * Render the backend table
         */
        renderTable() {
            if (!this.elements.tableBody) return;

            // Filter backends
            let filtered = this.backends.filter(b => {
                if (!this.searchQuery) return true;
                const searchStr = `${b.id} ${b.address} ${b.state}`.toLowerCase();
                return searchStr.includes(this.searchQuery);
            });

            // Sort backends
            filtered.sort((a, b) => {
                let aVal = a[this.sortColumn];
                let bVal = b[this.sortColumn];

                // Handle numeric columns
                if (['weight', 'active_conns', 'total_requests'].includes(this.sortColumn)) {
                    aVal = Number(aVal) || 0;
                    bVal = Number(bVal) || 0;
                }

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
                            ${this.searchQuery ? 'No backends match your search' : 'No backends in this pool'}
                        </td>
                    </tr>
                `);
            } else {
                const rowsHtml = paginated.map(backend => {
                    const health = this.healthStatuses[backend.id];
                    const isSelected = this.selectedBackends.has(backend.id);
                    const stateClass = this.getStateClass(backend.state);
                    const healthClass = backend.healthy ? 'success' : 'danger';
                    const healthText = backend.healthy ? 'Healthy' : 'Unhealthy';
                    const isDraining = backend.state === 'draining';
                    const isUp = backend.state === 'up';
                    const isMaintenance = backend.state === 'maintenance';

                    return `
                        <tr data-backend-id="${escapeHtml(backend.id)}" class="${isSelected ? 'selected' : ''}">
                            <td>
                                <input type="checkbox" class="backend-select"
                                       value="${escapeHtml(backend.id)}"
                                       ${isSelected ? 'checked' : ''}>
                            </td>
                            <td>
                                <div class="backend-name">${escapeHtml(backend.id)}</div>
                                <div class="backend-address text-muted">${escapeHtml(backend.address)}</div>
                            </td>
                            <td>
                                <span class="badge badge-${stateClass}">
                                    ${escapeHtml(backend.state)}
                                </span>
                            </td>
                            <td>
                                <span class="badge badge-${healthClass}">
                                    ${healthText}
                                </span>
                                ${health ? `<small class="text-muted">${escapeHtml(this.formatDuration(health.latency))}</small>` : ''}
                            </td>
                            <td>${backend.active_conns.toLocaleString()}</td>
                            <td>${this.calculateRPS(backend).toFixed(1)}</td>
                            <td>${escapeHtml(this.formatDuration(backend.avg_latency))}</td>
                            <td>${backend.weight}</td>
                            <td>
                                <div class="action-buttons">
                                    <button class="btn btn-sm btn-icon" title="View Details"
                                            data-action="detail" data-backend-id="${escapeHtml(backend.id)}">
                                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                                            <circle cx="12" cy="12" r="3"></circle>
                                        </svg>
                                    </button>
                                    <button class="btn btn-sm btn-icon ${isDraining ? 'disabled' : ''}"
                                            title="Drain" data-action="drain" data-backend-id="${escapeHtml(backend.id)}"
                                            ${isDraining ? 'disabled' : ''}>
                                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                            <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"></path>
                                        </svg>
                                    </button>
                                    <button class="btn btn-sm btn-icon ${isUp ? 'disabled' : ''}"
                                            title="Enable" data-action="enable" data-backend-id="${escapeHtml(backend.id)}"
                                            ${isUp ? 'disabled' : ''}>
                                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                            <polyline points="20 6 9 17 4 12"></polyline>
                                        </svg>
                                    </button>
                                    <button class="btn btn-sm btn-icon ${isMaintenance ? 'disabled' : ''}"
                                            title="Disable" data-action="disable" data-backend-id="${escapeHtml(backend.id)}"
                                            ${isMaintenance ? 'disabled' : ''}>
                                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                            <rect x="6" y="4" width="4" height="16"></rect>
                                            <rect x="14" y="4" width="4" height="16"></rect>
                                        </svg>
                                    </button>
                                    <button class="btn btn-sm btn-icon btn-danger" title="Remove"
                                            data-action="remove" data-backend-id="${escapeHtml(backend.id)}">
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
                this.elements.tableBody.querySelectorAll('.backend-select').forEach(cb => {
                    cb.addEventListener('change', (e) => {
                        this.toggleBackendSelection(e.target.value, e.target.checked);
                    });
                });

                // Bind action button events
                this.elements.tableBody.querySelectorAll('[data-action]').forEach(btn => {
                    btn.addEventListener('click', (e) => {
                        const action = e.currentTarget.dataset.action;
                        const backendId = e.currentTarget.dataset.backendId;
                        this.handleAction(action, backendId);
                    });
                });
            }

            // Update pagination
            this.renderPagination(totalPages, filtered.length);

            // Update select all checkbox
            if (this.elements.selectAll) {
                const allSelected = paginated.length > 0 && paginated.every(b => this.selectedBackends.has(b.id));
                this.elements.selectAll.checked = allSelected;
                this.elements.selectAll.indeterminate = this.selectedBackends.size > 0 && !allSelected;
            }

            // Update bulk actions visibility
            this.updateBulkActions();
        },

        /**
         * Handle action button clicks
         */
        handleAction(action, backendId) {
            switch (action) {
                case 'detail':
                    this.showDetail(backendId);
                    break;
                case 'drain':
                    this.drainBackend(backendId);
                    break;
                case 'enable':
                    this.enableBackend(backendId);
                    break;
                case 'disable':
                    this.disableBackend(backendId);
                    break;
                case 'remove':
                    this.confirmRemove(backendId);
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
                ? `Showing ${start}-${end} of ${totalItems} backends`
                : 'No backends';

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
         * Toggle selection of all visible backends
         */
        toggleSelectAll(checked) {
            const visibleBackends = this.getVisibleBackends();
            visibleBackends.forEach(b => {
                if (checked) {
                    this.selectedBackends.add(b.id);
                } else {
                    this.selectedBackends.delete(b.id);
                }
            });
            this.renderTable();
        },

        /**
         * Toggle selection of a single backend
         */
        toggleBackendSelection(backendId, checked) {
            if (checked) {
                this.selectedBackends.add(backendId);
            } else {
                this.selectedBackends.delete(backendId);
            }
            this.renderTable();
        },

        /**
         * Get currently visible backends (respecting search/filter)
         */
        getVisibleBackends() {
            return this.backends.filter(b => {
                if (!this.searchQuery) return true;
                const searchStr = `${b.id} ${b.address} ${b.state}`.toLowerCase();
                return searchStr.includes(this.searchQuery);
            });
        },

        /**
         * Update bulk actions visibility
         */
        updateBulkActions() {
            const bulkActionsEl = this.elements.bulkActions;
            if (bulkActionsEl) {
                bulkActionsEl.style.display = this.selectedBackends.size > 0 ? 'block' : 'none';
                const countEl = bulkActionsEl.querySelector('.selection-count');
                if (countEl) {
                    countEl.textContent = `${this.selectedBackends.size} selected`;
                }
            }
        },

        /**
         * Execute bulk action on selected backends
         */
        async executeBulkAction(action) {
            const ids = Array.from(this.selectedBackends);
            if (ids.length === 0) return;

            const actionNames = {
                drain: 'Drain',
                enable: 'Enable',
                disable: 'Disable',
                remove: 'Remove'
            };

            if (action === 'remove') {
                this.showConfirmModal(
                    `Remove ${ids.length} backends?`,
                    'This action cannot be undone.',
                    () => this.executeBulkRemove(ids)
                );
                return;
            }

            this.showToast('info', `${actionNames[action]}ing ${ids.length} backends...`);

            const promises = ids.map(id => this.executeBackendAction(id, action));
            const results = await Promise.allSettled(promises);

            const succeeded = results.filter(r => r.status === 'fulfilled').length;
            const failed = results.filter(r => r.status === 'rejected').length;

            if (failed === 0) {
                this.showToast('success', `${actionNames[action]}ed ${succeeded} backends successfully`);
            } else {
                this.showToast('warning', `${actionNames[action]}ed ${succeeded} backends, ${failed} failed`);
            }

            this.selectedBackends.clear();
            this.refreshData();
        },

        /**
         * Execute bulk remove
         */
        async executeBulkRemove(ids) {
            this.showToast('info', `Removing ${ids.length} backends...`);

            const promises = ids.map(id => this.removeBackendInternal(id));
            const results = await Promise.allSettled(promises);

            const succeeded = results.filter(r => r.status === 'fulfilled').length;
            const failed = results.filter(r => r.status === 'rejected').length;

            if (failed === 0) {
                this.showToast('success', `Removed ${succeeded} backends successfully`);
            } else {
                this.showToast('warning', `Removed ${succeeded} backends, ${failed} failed`);
            }

            this.selectedBackends.clear();
            this.hideModal('confirm-modal');
            this.refreshData();
        },

        /**
         * Execute action on a single backend
         */
        async executeBackendAction(backendId, action) {
            const endpoints = {
                drain: `/api/v1/backends/${encodeURIComponent(this.currentPool)}/${encodeURIComponent(backendId)}/drain`,
                enable: `/api/v1/backends/${encodeURIComponent(this.currentPool)}/${encodeURIComponent(backendId)}/enable`,
                disable: `/api/v1/backends/${encodeURIComponent(this.currentPool)}/${encodeURIComponent(backendId)}/disable`
            };

            const response = await fetch(endpoints[action], { method: 'POST' });
            const data = await response.json();

            if (!data.success) {
                throw new Error(data.error?.message || 'Action failed');
            }

            return data;
        },

        /**
         * Individual backend actions
         */
        async drainBackend(backendId) {
            try {
                await this.executeBackendAction(backendId, 'drain');
                this.showToast('success', `Backend ${escapeHtml(backendId)} is draining`);
                this.refreshData();
            } catch (error) {
                this.showToast('error', `Failed to drain backend: ${error.message}`);
            }
        },

        async enableBackend(backendId) {
            try {
                await this.executeBackendAction(backendId, 'enable');
                this.showToast('success', `Backend ${escapeHtml(backendId)} enabled`);
                this.refreshData();
            } catch (error) {
                this.showToast('error', `Failed to enable backend: ${error.message}`);
            }
        },

        async disableBackend(backendId) {
            try {
                await this.executeBackendAction(backendId, 'disable');
                this.showToast('success', `Backend ${escapeHtml(backendId)} disabled`);
                this.refreshData();
            } catch (error) {
                this.showToast('error', `Failed to disable backend: ${error.message}`);
            }
        },

        /**
         * Show remove confirmation
         */
        confirmRemove(backendId) {
            this.showConfirmModal(
                'Remove Backend?',
                `Are you sure you want to remove backend "${escapeHtml(backendId)}"? This action cannot be undone.`,
                () => this.removeBackendInternal(backendId)
            );
        },

        /**
         * Remove a backend (internal implementation)
         */
        async removeBackendInternal(backendId) {
            const response = await fetch(
                `/api/v1/backends/${encodeURIComponent(this.currentPool)}/${encodeURIComponent(backendId)}`,
                { method: 'DELETE' }
            );
            const data = await response.json();

            if (!data.success) {
                throw new Error(data.error?.message || 'Remove failed');
            }

            return data;
        },

        /**
         * Remove a backend with UI feedback
         */
        async removeBackend(backendId) {
            try {
                await this.removeBackendInternal(backendId);
                this.showToast('success', `Backend ${escapeHtml(backendId)} removed successfully`);
                this.hideModal('confirm-modal');
                this.refreshData();
            } catch (error) {
                this.showToast('error', `Failed to remove backend: ${error.message}`);
            }
        },

        /**
         * Show backend detail modal
         */
        showDetail(backendId) {
            const backend = this.backends.find(b => b.id === backendId);
            if (!backend) return;

            const health = this.healthStatuses[backendId];
            const modal = this.elements.detailModal;

            const titleEl = modal.querySelector('.modal-title');
            if (titleEl) {
                titleEl.textContent = `Backend: ${backendId}`;
            }

            const contentEl = modal.querySelector('.backend-detail-content');
            if (contentEl) {
                const healthSection = health ? `
                <div class="detail-section">
                    <h4>Health Check Status</h4>
                    <div class="detail-grid">
                        <div class="detail-item">
                            <label>Status</label>
                            <value><span class="badge badge-${health.status === 'healthy' ? 'success' : 'danger'}">${escapeHtml(health.status)}</span></value>
                        </div>
                        <div class="detail-item">
                            <label>Last Check</label>
                            <value>${health.last_check ? new Date(health.last_check).toLocaleString() : 'Never'}</value>
                        </div>
                        <div class="detail-item">
                            <label>Latency</label>
                            <value>${escapeHtml(this.formatDuration(health.latency))}</value>
                        </div>
                        ${health.error ? `
                        <div class="detail-item full-width">
                            <label>Error</label>
                            <value class="text-danger">${escapeHtml(health.error)}</value>
                        </div>
                        ` : ''}
                    </div>
                </div>
                ` : '';

                setSafeHTML(contentEl, `
                    <div class="detail-section">
                        <h4>General Information</h4>
                        <div class="detail-grid">
                            <div class="detail-item">
                                <label>ID</label>
                                <value>${escapeHtml(backend.id)}</value>
                            </div>
                            <div class="detail-item">
                                <label>Address</label>
                                <value>${escapeHtml(backend.address)}</value>
                            </div>
                            <div class="detail-item">
                                <label>State</label>
                                <value><span class="badge badge-${this.getStateClass(backend.state)}">${escapeHtml(backend.state)}</span></value>
                            </div>
                            <div class="detail-item">
                                <label>Health</label>
                                <value><span class="badge badge-${backend.healthy ? 'success' : 'danger'}">${backend.healthy ? 'Healthy' : 'Unhealthy'}</span></value>
                            </div>
                            <div class="detail-item">
                                <label>Weight</label>
                                <value>${backend.weight}</value>
                            </div>
                            <div class="detail-item">
                                <label>Max Connections</label>
                                <value>${backend.max_conns || 'Unlimited'}</value>
                            </div>
                        </div>
                    </div>

                    <div class="detail-section">
                        <h4>Statistics</h4>
                        <div class="detail-grid">
                            <div class="detail-item">
                                <label>Active Connections</label>
                                <value>${backend.active_conns.toLocaleString()}</value>
                            </div>
                            <div class="detail-item">
                                <label>Total Requests</label>
                                <value>${backend.total_requests.toLocaleString()}</value>
                            </div>
                            <div class="detail-item">
                                <label>Total Errors</label>
                                <value>${backend.total_errors.toLocaleString()}</value>
                            </div>
                            <div class="detail-item">
                                <label>Total Bytes</label>
                                <value>${this.formatBytes(backend.total_bytes)}</value>
                            </div>
                            <div class="detail-item">
                                <label>Average Latency</label>
                                <value>${escapeHtml(this.formatDuration(backend.avg_latency))}</value>
                            </div>
                            <div class="detail-item">
                                <label>Last Latency</label>
                                <value>${escapeHtml(this.formatDuration(backend.last_latency))}</value>
                            </div>
                        </div>
                    </div>

                    ${healthSection}

                    <div class="detail-section">
                        <h4>Metrics History</h4>
                        <div id="backend-metrics-chart" class="chart-container"></div>
                    </div>
                `);
            }

            this.showModal('backend-detail-modal');

            // Initialize charts after modal is shown
            setTimeout(() => this.initBackendCharts(backend), 100);
        },

        /**
         * Initialize backend detail charts
         */
        initBackendCharts(backend) {
            const chartContainer = document.getElementById('backend-metrics-chart');
            if (!chartContainer) return;

            // Placeholder for chart implementation
            // In a real implementation, this would use a charting library
            setSafeHTML(chartContainer, `
                <div class="chart-placeholder">
                    <div class="chart-metric">
                        <span class="metric-label">Requests/sec</span>
                        <span class="metric-value">${this.calculateRPS(backend).toFixed(2)}</span>
                    </div>
                    <div class="chart-metric">
                        <span class="metric-label">Error Rate</span>
                        <span class="metric-value">${this.calculateErrorRate(backend).toFixed(2)}%</span>
                    </div>
                    <div class="chart-metric">
                        <span class="metric-label">Latency</span>
                        <span class="metric-value">${escapeHtml(this.formatDuration(backend.avg_latency))}</span>
                    </div>
                </div>
            `);
        },

        /**
         * Show add backend modal
         */
        showAddModal() {
            const form = document.getElementById('add-backend-form');
            if (form) {
                form.reset();
            }
            this.showModal('add-backend-modal');
        },

        /**
         * Add a new backend
         */
        async addBackend(formData) {
            const backend = {
                id: formData.get('backend-id'),
                address: formData.get('backend-address'),
                weight: parseInt(formData.get('backend-weight'), 10) || 1
            };

            try {
                const response = await fetch(
                    `/api/v1/backends/${encodeURIComponent(this.currentPool)}`,
                    {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(backend)
                    }
                );

                const data = await response.json();

                if (data.success) {
                    this.showToast('success', `Backend ${escapeHtml(backend.id)} added successfully`);
                    this.hideModal('add-backend-modal');
                    this.refreshData();
                } else {
                    throw new Error(data.error?.message || 'Add failed');
                }
            } catch (error) {
                this.showToast('error', `Failed to add backend: ${error.message}`);
            }
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
            if (this.currentPool) {
                await this.loadPoolDetails(this.currentPool);
            }
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
                    // Subscribe to backend updates
                    this.ws.send(JSON.stringify({ action: 'subscribe', channel: 'backends' }));
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
                case 'backend_update':
                    this.updateBackendFromWS(message.data);
                    break;
                case 'health_update':
                    this.updateHealthFromWS(message.data);
                    break;
            }
        },

        /**
         * Update backend data from WebSocket
         */
        updateBackendFromWS(data) {
            const index = this.backends.findIndex(b => b.id === data.id);
            if (index !== -1) {
                this.backends[index] = { ...this.backends[index], ...data };
                this.renderTable();
            }
        },

        /**
         * Update health status from WebSocket
         */
        updateHealthFromWS(data) {
            this.healthStatuses[data.backend_id] = data;
            this.renderTable();
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
         * Calculate requests per second
         */
        calculateRPS(backend) {
            // This is a simplified calculation
            // In a real implementation, we'd track this over time
            return backend.total_requests / 60; // Assuming 1 minute window
        },

        /**
         * Calculate error rate
         */
        calculateErrorRate(backend) {
            if (backend.total_requests === 0) return 0;
            return (backend.total_errors / backend.total_requests) * 100;
        },

        /**
         * Get CSS class for state badge
         */
        getStateClass(state) {
            const classes = {
                'up': 'success',
                'down': 'danger',
                'draining': 'warning',
                'maintenance': 'info',
                'starting': 'secondary'
            };
            return classes[state] || 'secondary';
        },

        /**
         * Format duration string
         */
        formatDuration(duration) {
            if (!duration || duration === '0s') return '0ms';

            // Parse duration string like "1h2m3.456s"
            const match = duration.match(/(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?/);
            if (!match) return String(duration);

            const hours = parseInt(match[1]) || 0;
            const minutes = parseInt(match[2]) || 0;
            const seconds = parseFloat(match[3]) || 0;

            const totalMs = (hours * 3600 + minutes * 60 + seconds) * 1000;

            if (totalMs < 1000) return `${Math.round(totalMs)}ms`;
            if (totalMs < 60000) return `${(totalMs / 1000).toFixed(2)}s`;
            return `${(totalMs / 60000).toFixed(2)}m`;
        },

        /**
         * Format bytes to human readable
         */
        formatBytes(bytes) {
            if (bytes === 0) return '0 B';
            const k = 1024;
            const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
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
    window.BackendsPage = BackendsPage;

    // Initialize when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', () => BackendsPage.init());
    } else {
        BackendsPage.init();
    }
})();

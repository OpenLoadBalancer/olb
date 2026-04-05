// OpenLoadBalancer UI Components
// Phase 3.1: Vanilla JavaScript Components
// Zero external dependencies

(function(global) {
    'use strict';

    const Components = {};

    // ==================== Table Component ====================

    class Table {
        constructor(options = {}) {
            this.data = options.data || [];
            this.columns = options.columns || [];
            this.sortKey = options.sortKey || null;
            this.sortOrder = options.sortOrder || 'asc';
            this.page = options.page || 1;
            this.perPage = options.perPage || 10;
            this.onSort = options.onSort || null;
            this.onPageChange = options.onPageChange || null;
            this.onRowClick = options.onRowClick || null;
            this.selectable = options.selectable || false;
            this.selectedRows = new Set();
        }

        render() {
            const start = (this.page - 1) * this.perPage;
            const end = start + this.perPage;
            const paginatedData = this.sortedData.slice(start, end);
            const totalPages = Math.ceil(this.data.length / this.perPage);

            const container = document.createElement('div');
            container.className = 'table-container';

            // Build table using safe DOM methods
            const table = document.createElement('table');
            table.className = 'table';

            // Header
            const thead = document.createElement('thead');
            const headerRow = document.createElement('tr');

            if (this.selectable) {
                const th = document.createElement('th');
                const checkbox = document.createElement('input');
                checkbox.type = 'checkbox';
                checkbox.className = 'form-check-input select-all';
                th.appendChild(checkbox);
                headerRow.appendChild(th);
            }

            this.columns.forEach(col => {
                const th = document.createElement('th');
                if (col.sortable !== false) {
                    th.className = `sortable ${this.sortKey === col.key ? 'sort-' + this.sortOrder : ''}`;
                    th.dataset.sort = col.key;
                }
                th.textContent = col.title;

                if (col.sortable !== false) {
                    const icon = document.createElement('span');
                    icon.className = 'sort-icon';
                    icon.textContent = '↕';
                    th.appendChild(icon);
                }

                headerRow.appendChild(th);
            });

            thead.appendChild(headerRow);
            table.appendChild(thead);

            // Body
            const tbody = document.createElement('tbody');

            if (paginatedData.length === 0) {
                const tr = document.createElement('tr');
                const td = document.createElement('td');
                td.colSpan = this.columns.length + (this.selectable ? 1 : 0);
                td.className = 'text-center py-8 text-secondary';
                td.textContent = 'No data available';
                tr.appendChild(td);
                tbody.appendChild(tr);
            } else {
                paginatedData.forEach((row, index) => {
                    const tr = document.createElement('tr');
                    tr.dataset.index = String(start + index);

                    if (this.selectable) {
                        const td = document.createElement('td');
                        const checkbox = document.createElement('input');
                        checkbox.type = 'checkbox';
                        checkbox.className = 'form-check-input row-select';
                        checkbox.checked = this.selectedRows.has(start + index);
                        td.appendChild(checkbox);
                        tr.appendChild(td);
                    }

                    this.columns.forEach(col => {
                        const td = document.createElement('td');
                        const value = row[col.key];
                        if (col.render) {
                            const rendered = col.render(value, row);
                            if (typeof rendered === 'string') {
                                td.textContent = rendered;
                            } else if (rendered instanceof HTMLElement) {
                                td.appendChild(rendered);
                            }
                        } else {
                            td.textContent = String(value ?? '');
                        }
                        tr.appendChild(td);
                    });

                    tbody.appendChild(tr);
                });
            }

            table.appendChild(tbody);
            container.appendChild(table);

            // Pagination
            if (this.data.length > this.perPage) {
                container.appendChild(this.renderPagination(totalPages));
            }

            this.attachEvents(container);
            return container;
        }

        get sortedData() {
            if (!this.sortKey) return this.data;

            const col = this.columns.find(c => c.key === this.sortKey);
            if (!col) return this.data;

            return [...this.data].sort((a, b) => {
                let aVal = a[this.sortKey];
                let bVal = b[this.sortKey];

                if (col.sortFn) {
                    return this.sortOrder === 'asc'
                        ? col.sortFn(aVal, bVal)
                        : col.sortFn(bVal, aVal);
                }

                if (typeof aVal === 'string') aVal = aVal.toLowerCase();
                if (typeof bVal === 'string') bVal = bVal.toLowerCase();

                if (aVal < bVal) return this.sortOrder === 'asc' ? -1 : 1;
                if (aVal > bVal) return this.sortOrder === 'asc' ? 1 : -1;
                return 0;
            });
        }

        renderPagination(totalPages) {
            const pages = this.getPageRange(totalPages);

            const wrapper = document.createElement('div');
            wrapper.className = 'flex items-center justify-between px-4 py-3 border-t';

            const info = document.createElement('div');
            info.className = 'text-sm text-secondary';
            const start = (this.page - 1) * this.perPage + 1;
            const end = Math.min(this.page * this.perPage, this.data.length);
            info.textContent = `Showing ${start} to ${end} of ${this.data.length} entries`;
            wrapper.appendChild(info);

            const pagination = document.createElement('div');
            pagination.className = 'pagination';

            // Prev button
            const prevBtn = document.createElement('button');
            prevBtn.className = 'page-btn';
            prevBtn.dataset.page = 'prev';
            prevBtn.textContent = '←';
            prevBtn.disabled = this.page === 1;
            pagination.appendChild(prevBtn);

            // Page buttons
            pages.forEach(p => {
                if (p === '...') {
                    const span = document.createElement('span');
                    span.className = 'px-2 text-secondary';
                    span.textContent = '...';
                    pagination.appendChild(span);
                } else {
                    const btn = document.createElement('button');
                    btn.className = `page-btn ${p === this.page ? 'active' : ''}`;
                    btn.dataset.page = String(p);
                    btn.textContent = String(p);
                    pagination.appendChild(btn);
                }
            });

            // Next button
            const nextBtn = document.createElement('button');
            nextBtn.className = 'page-btn';
            nextBtn.dataset.page = 'next';
            nextBtn.textContent = '→';
            nextBtn.disabled = this.page === totalPages;
            pagination.appendChild(nextBtn);

            wrapper.appendChild(pagination);
            return wrapper;
        }

        getPageRange(totalPages) {
            const delta = 2;
            const range = [];
            const rangeWithDots = [];
            let l;

            for (let i = 1; i <= totalPages; i++) {
                if (i === 1 || i === totalPages || (i >= this.page - delta && i <= this.page + delta)) {
                    range.push(i);
                }
            }

            for (const i of range) {
                if (l) {
                    if (i - l === 2) {
                        rangeWithDots.push(l + 1);
                    } else if (i - l !== 1) {
                        rangeWithDots.push('...');
                    }
                }
                rangeWithDots.push(i);
                l = i;
            }

            return rangeWithDots;
        }

        attachEvents(container) {
            // Sort events
            container.querySelectorAll('th.sortable').forEach(th => {
                th.addEventListener('click', () => {
                    const key = th.dataset.sort;
                    if (this.sortKey === key) {
                        this.sortOrder = this.sortOrder === 'asc' ? 'desc' : 'asc';
                    } else {
                        this.sortKey = key;
                        this.sortOrder = 'asc';
                    }
                    if (this.onSort) this.onSort(this.sortKey, this.sortOrder);
                    this.refresh();
                });
            });

            // Pagination events
            container.querySelectorAll('.page-btn').forEach(btn => {
                btn.addEventListener('click', () => {
                    const page = btn.dataset.page;
                    const totalPages = Math.ceil(this.data.length / this.perPage);

                    if (page === 'prev' && this.page > 1) {
                        this.page--;
                    } else if (page === 'next' && this.page < totalPages) {
                        this.page++;
                    } else if (page !== 'prev' && page !== 'next') {
                        this.page = parseInt(page);
                    }

                    if (this.onPageChange) this.onPageChange(this.page);
                    this.refresh();
                });
            });

            // Row click events
            if (this.onRowClick) {
                container.querySelectorAll('tbody tr').forEach(tr => {
                    tr.addEventListener('click', (e) => {
                        if (e.target.tagName !== 'INPUT') {
                            const index = parseInt(tr.dataset.index);
                            this.onRowClick(this.data[index], index);
                        }
                    });
                });
            }

            // Selection events
            if (this.selectable) {
                const selectAll = container.querySelector('.select-all');
                if (selectAll) {
                    selectAll.addEventListener('change', (e) => {
                        const checkboxes = container.querySelectorAll('.row-select');
                        checkboxes.forEach((cb, i) => {
                            cb.checked = e.target.checked;
                            const index = (this.page - 1) * this.perPage + i;
                            if (e.target.checked) {
                                this.selectedRows.add(index);
                            } else {
                                this.selectedRows.delete(index);
                            }
                        });
                    });
                }

                container.querySelectorAll('.row-select').forEach((cb, i) => {
                    cb.addEventListener('change', (e) => {
                        const index = (this.page - 1) * this.perPage + i;
                        if (e.target.checked) {
                            this.selectedRows.add(index);
                        } else {
                            this.selectedRows.delete(index);
                        }
                    });
                });
            }
        }

        refresh() {
            const container = this.container;
            if (container) {
                const newTable = this.render();
                container.innerHTML = '';
                container.appendChild(newTable);
            }
        }

        mount(selector) {
            this.container = typeof selector === 'string'
                ? document.querySelector(selector)
                : selector;

            if (this.container) {
                this.container.innerHTML = '';
                this.container.appendChild(this.render());
            }

            return this;
        }
    }

    Components.Table = Table;

    // ==================== Card Component ====================

    class Card {
        constructor(options = {}) {
            this.title = options.title || '';
            this.subtitle = options.subtitle || '';
            this.content = options.content || '';
            this.footer = options.footer || '';
            this.actions = options.actions || [];
            this.className = options.className || '';
            this.hover = options.hover || false;
        }

        render() {
            const card = document.createElement('div');
            card.className = `card ${this.hover ? 'card-hover' : ''} ${this.className}`;

            // Header
            if (this.title || this.subtitle || this.actions.length > 0) {
                const header = document.createElement('div');
                header.className = 'card-header';

                const titleDiv = document.createElement('div');

                if (this.title) {
                    const titleEl = document.createElement('h3');
                    titleEl.className = 'card-title';
                    titleEl.textContent = this.title;
                    titleDiv.appendChild(titleEl);
                }

                if (this.subtitle) {
                    const subtitleEl = document.createElement('p');
                    subtitleEl.className = 'card-subtitle';
                    subtitleEl.textContent = this.subtitle;
                    titleDiv.appendChild(subtitleEl);
                }

                header.appendChild(titleDiv);

                if (this.actions.length > 0) {
                    const actionsDiv = document.createElement('div');
                    actionsDiv.className = 'flex gap-2';

                    this.actions.forEach(action => {
                        const btn = document.createElement('button');
                        btn.className = `btn btn-sm ${action.variant ? 'btn-' + action.variant : 'btn-ghost'}`;
                        if (action.id) btn.dataset.action = action.id;
                        if (action.icon) {
                            const icon = document.createElement('span');
                            icon.className = 'icon';
                            icon.textContent = action.icon;
                            btn.appendChild(icon);
                        }
                        btn.appendChild(document.createTextNode(action.label));
                        actionsDiv.appendChild(btn);
                    });

                    header.appendChild(actionsDiv);
                }

                card.appendChild(header);
            }

            // Body
            if (this.content) {
                const body = document.createElement('div');
                body.className = 'card-body';
                if (typeof this.content === 'string') {
                    body.textContent = this.content;
                } else if (this.content instanceof HTMLElement) {
                    body.appendChild(this.content);
                }
                card.appendChild(body);
            }

            // Footer
            if (this.footer) {
                const footer = document.createElement('div');
                footer.className = 'card-footer';
                if (typeof this.footer === 'string') {
                    footer.textContent = this.footer;
                } else if (this.footer instanceof HTMLElement) {
                    footer.appendChild(this.footer);
                }
                card.appendChild(footer);
            }

            return card;
        }
    }

    Components.Card = Card;

    // ==================== Badge Component ====================

    Components.Badge = {
        render(options = {}) {
            const variant = options.variant || 'default';
            const text = options.text || '';
            const dot = options.dot || false;
            const icon = options.icon || '';

            const badge = document.createElement('span');
            badge.className = `badge badge-${variant} ${dot ? 'badge-status' : ''}`;

            if (dot) {
                const dotSpan = document.createElement('span');
                dotSpan.className = 'badge-dot';
                badge.appendChild(dotSpan);
            }

            if (icon) {
                const iconSpan = document.createElement('span');
                iconSpan.className = 'icon';
                iconSpan.textContent = icon;
                badge.appendChild(iconSpan);
            }

            badge.appendChild(document.createTextNode(text));

            return badge;
        }
    };

    // ==================== Modal Component ====================

    class Modal {
        constructor(options = {}) {
            this.title = options.title || '';
            this.content = options.content || '';
            this.size = options.size || '';
            this.showClose = options.showClose !== false;
            this.footer = options.footer || '';
            this.onClose = options.onClose || null;
            this.closeOnOverlay = options.closeOnOverlay !== false;
        }

        render() {
            this.overlay = document.createElement('div');
            this.overlay.className = 'modal-overlay';

            const modal = document.createElement('div');
            modal.className = `modal ${this.size ? 'modal-' + this.size : ''}`;

            // Header
            const header = document.createElement('div');
            header.className = 'modal-header';

            const title = document.createElement('h3');
            title.className = 'modal-title';
            title.textContent = this.title;
            header.appendChild(title);

            if (this.showClose) {
                const closeBtn = document.createElement('button');
                closeBtn.className = 'modal-close';
                closeBtn.textContent = '×';
                closeBtn.addEventListener('click', () => this.close());
                header.appendChild(closeBtn);
            }

            modal.appendChild(header);

            // Body
            const body = document.createElement('div');
            body.className = 'modal-body';
            if (typeof this.content === 'string') {
                body.textContent = this.content;
            } else if (this.content instanceof HTMLElement) {
                body.appendChild(this.content);
            }
            modal.appendChild(body);

            // Footer
            if (this.footer) {
                const footer = document.createElement('div');
                footer.className = 'modal-footer';
                if (typeof this.footer === 'string') {
                    footer.textContent = this.footer;
                } else if (this.footer instanceof HTMLElement) {
                    footer.appendChild(this.footer);
                }
                modal.appendChild(footer);
            }

            this.overlay.appendChild(modal);

            // Overlay click
            if (this.closeOnOverlay) {
                this.overlay.addEventListener('click', (e) => {
                    if (e.target === this.overlay) {
                        this.close();
                    }
                });
            }

            // Escape key
            this.escHandler = (e) => {
                if (e.key === 'Escape') {
                    this.close();
                }
            };
            document.addEventListener('keydown', this.escHandler);

            return this.overlay;
        }

        open() {
            if (!this.overlay) {
                this.render();
            }
            document.body.appendChild(this.overlay);
            // Trigger reflow
            this.overlay.offsetHeight;
            this.overlay.classList.add('active');
            document.body.style.overflow = 'hidden';
        }

        close() {
            if (this.overlay) {
                this.overlay.classList.remove('active');
                setTimeout(() => {
                    if (this.overlay.parentNode) {
                        this.overlay.parentNode.removeChild(this.overlay);
                    }
                    document.body.style.overflow = '';
                    if (this.onClose) this.onClose();
                }, 200);
            }
            document.removeEventListener('keydown', this.escHandler);
        }
    }

    Components.Modal = Modal;

    // ==================== Toast Component ====================

    Components.Toast = {
        container: null,

        getContainer() {
            if (!this.container) {
                this.container = document.createElement('div');
                this.container.className = 'toast-container';
                document.body.appendChild(this.container);
            }
            return this.container;
        },

        show(options = {}) {
            const variant = options.variant || 'info';
            const title = options.title || '';
            const message = options.message || '';
            const duration = options.duration || 5000;
            const icon = options.icon || this.getIcon(variant);

            const toast = document.createElement('div');
            toast.className = `toast toast-${variant}`;

            const iconSpan = document.createElement('span');
            iconSpan.className = 'toast-icon';
            iconSpan.textContent = icon;
            toast.appendChild(iconSpan);

            const content = document.createElement('div');
            content.className = 'toast-content';

            if (title) {
                const titleEl = document.createElement('div');
                titleEl.className = 'toast-title';
                titleEl.textContent = title;
                content.appendChild(titleEl);
            }

            const messageEl = document.createElement('div');
            messageEl.className = 'toast-message';
            messageEl.textContent = message;
            content.appendChild(messageEl);

            toast.appendChild(content);

            const closeBtn = document.createElement('button');
            closeBtn.className = 'toast-close';
            closeBtn.textContent = '×';
            closeBtn.addEventListener('click', () => this.dismiss(toast));
            toast.appendChild(closeBtn);

            // Auto dismiss
            if (duration > 0) {
                setTimeout(() => this.dismiss(toast), duration);
            }

            this.getContainer().appendChild(toast);

            return toast;
        },

        dismiss(toast) {
            toast.classList.add('toast-out');
            setTimeout(() => {
                if (toast.parentNode) {
                    toast.parentNode.removeChild(toast);
                }
            }, 300);
        },

        success(message, title) {
            return this.show({ variant: 'success', title, message });
        },

        error(message, title) {
            return this.show({ variant: 'error', title, message });
        },

        warning(message, title) {
            return this.show({ variant: 'warning', title, message });
        },

        info(message, title) {
            return this.show({ variant: 'info', title, message });
        },

        getIcon(variant) {
            const icons = {
                success: '✓',
                error: '✕',
                warning: '⚠',
                info: 'ℹ'
            };
            return icons[variant] || '•';
        }
    };

    // ==================== Form Components ====================

    Components.Form = {
        createInput(options = {}) {
            const input = document.createElement('input');
            input.type = options.type || 'text';
            input.className = `form-input ${options.className || ''}`;
            input.placeholder = options.placeholder || '';
            input.value = options.value || '';
            input.name = options.name || '';
            input.id = options.id || '';

            if (options.required) input.required = true;
            if (options.disabled) input.disabled = true;
            if (options.readOnly) input.readOnly = true;

            return input;
        },

        createSelect(options = {}) {
            const select = document.createElement('select');
            select.className = `form-select ${options.className || ''}`;
            select.name = options.name || '';
            select.id = options.id || '';

            if (options.required) select.required = true;
            if (options.disabled) select.disabled = true;

            options.options?.forEach(opt => {
                const option = document.createElement('option');
                option.value = opt.value;
                option.textContent = opt.label;
                if (opt.value === options.value) option.selected = true;
                select.appendChild(option);
            });

            return select;
        },

        createTextarea(options = {}) {
            const textarea = document.createElement('textarea');
            textarea.className = `form-textarea ${options.className || ''}`;
            textarea.placeholder = options.placeholder || '';
            textarea.value = options.value || '';
            textarea.name = options.name || '';
            textarea.id = options.id || '';
            textarea.rows = options.rows || 4;

            if (options.required) textarea.required = true;
            if (options.disabled) textarea.disabled = true;
            if (options.readOnly) textarea.readOnly = true;

            return textarea;
        },

        createGroup(options = {}) {
            const group = document.createElement('div');
            group.className = 'form-group';

            if (options.label) {
                const label = document.createElement('label');
                label.className = `form-label ${options.required ? 'form-label-required' : ''}`;
                label.htmlFor = options.id || '';
                label.textContent = options.label;
                group.appendChild(label);
            }

            if (options.input) {
                group.appendChild(options.input);
            }

            if (options.hint) {
                const hint = document.createElement('p');
                hint.className = 'form-hint';
                hint.textContent = options.hint;
                group.appendChild(hint);
            }

            if (options.error) {
                const error = document.createElement('p');
                error.className = 'form-error';
                error.textContent = options.error;
                group.appendChild(error);
            }

            return group;
        }
    };

    // ==================== Loading Spinner ====================

    Components.Spinner = {
        render(options = {}) {
            const size = options.size || 'md';
            const spinner = document.createElement('div');
            spinner.className = `spinner spinner-${size}`;
            return spinner;
        },

        show(container, options = {}) {
            const overlay = document.createElement('div');
            overlay.className = `loading-overlay ${options.fixed ? 'loading-overlay-fixed' : ''}`;

            const spinner = this.render(options);
            overlay.appendChild(spinner);

            const target = typeof container === 'string'
                ? document.querySelector(container)
                : container;

            if (target) {
                target.style.position = 'relative';
                target.appendChild(overlay);
            }

            return {
                hide() {
                    if (overlay.parentNode) {
                        overlay.parentNode.removeChild(overlay);
                    }
                }
            };
        }
    };

    // ==================== Empty State ====================

    Components.EmptyState = {
        render(options = {}) {
            const icon = options.icon || '📭';
            const title = options.title || 'No data';
            const description = options.description || '';
            const action = options.action || null;

            const container = document.createElement('div');
            container.className = 'empty-state';

            const iconDiv = document.createElement('div');
            iconDiv.className = 'empty-state-icon';
            iconDiv.textContent = icon;
            container.appendChild(iconDiv);

            const titleEl = document.createElement('h3');
            titleEl.className = 'empty-state-title';
            titleEl.textContent = title;
            container.appendChild(titleEl);

            if (description) {
                const descEl = document.createElement('p');
                descEl.className = 'empty-state-description';
                descEl.textContent = description;
                container.appendChild(descEl);
            }

            if (action) {
                const btn = document.createElement('button');
                btn.className = 'btn btn-primary';
                btn.textContent = action.label;
                btn.addEventListener('click', action.onClick);
                container.appendChild(btn);
            }

            return container;
        }
    };

    // ==================== Stats Component ====================

    Components.Stats = {
        render(options = {}) {
            const stats = options.stats || [];

            const container = document.createElement('div');
            container.className = 'stat-grid';

            stats.forEach(stat => {
                const card = document.createElement('div');
                card.className = 'stat-card';

                const label = document.createElement('div');
                label.className = 'stat-label';
                label.textContent = stat.label;
                card.appendChild(label);

                const value = document.createElement('div');
                value.className = 'stat-value';
                value.textContent = String(stat.value);
                card.appendChild(value);

                if (stat.change !== undefined) {
                    const change = document.createElement('div');
                    change.className = `stat-change ${stat.change > 0 ? 'stat-change-positive' : stat.change < 0 ? 'stat-change-negative' : ''}`;

                    const icon = document.createElement('span');
                    icon.textContent = stat.change > 0 ? '↑' : stat.change < 0 ? '↓' : '−';
                    change.appendChild(icon);

                    const percent = document.createElement('span');
                    percent.textContent = `${Math.abs(stat.change)}%`;
                    change.appendChild(percent);

                    card.appendChild(change);
                }

                container.appendChild(card);
            });

            return container;
        }
    };

    // Expose to global
    global.Components = Components;

})(window);

// OpenLoadBalancer Dashboard Page
// Phase 3.2: Dashboard Page with Live Metrics

/**
 * Dashboard Page Component
 * Provides real-time metrics visualization and system overview
 */
(function(global) {
    'use strict';

    // Sparkline Chart Component
    class SparklineChart {
        constructor(canvas, options = {}) {
            this.canvas = canvas;
            this.ctx = canvas.getContext('2d');
            this.data = [];
            this.maxPoints = options.maxPoints || 60;
            this.color = options.color || '#3b82f6';
            this.fillColor = options.fillColor || 'rgba(59, 130, 246, 0.1)';
            this.lineWidth = options.lineWidth || 2;

            this.resize();
            window.addEventListener('resize', () => this.resize());
        }

        resize() {
            const rect = this.canvas.parentElement.getBoundingClientRect();
            this.canvas.width = rect.width * window.devicePixelRatio;
            this.canvas.height = rect.height * window.devicePixelRatio;
            this.ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
            this.width = rect.width;
            this.height = rect.height;
            this.draw();
        }

        push(value) {
            this.data.push(value);
            if (this.data.length > this.maxPoints) {
                this.data.shift();
            }
            this.draw();
        }

        draw() {
            const ctx = this.ctx;
            const width = this.width;
            const height = this.height;

            ctx.clearRect(0, 0, width, height);

            if (this.data.length < 2) return;

            const min = Math.min(...this.data);
            const max = Math.max(...this.data);
            const range = max - min || 1;

            ctx.beginPath();
            ctx.strokeStyle = this.color;
            ctx.lineWidth = this.lineWidth;
            ctx.lineJoin = 'round';
            ctx.lineCap = 'round';

            this.data.forEach((value, i) => {
                const x = (i / (this.maxPoints - 1)) * width;
                const y = height - ((value - min) / range) * (height - 10) - 5;

                if (i === 0) {
                    ctx.moveTo(x, y);
                } else {
                    ctx.lineTo(x, y);
                }
            });

            ctx.stroke();

            // Fill area
            ctx.lineTo(width, height);
            ctx.lineTo(0, height);
            ctx.closePath();
            ctx.fillStyle = this.fillColor;
            ctx.fill();
        }

        destroy() {
            window.removeEventListener('resize', () => this.resize());
        }
    }

    // Gauge Chart Component
    class GaugeChart {
        constructor(canvas, options = {}) {
            this.canvas = canvas;
            this.ctx = canvas.getContext('2d');
            this.value = 0;
            this.max = options.max || 100;
            this.color = options.color || '#10b981';
            this.warningColor = options.warningColor || '#f59e0b';
            this.dangerColor = options.dangerColor || '#ef4444';

            this.resize();
            window.addEventListener('resize', () => this.resize());
        }

        resize() {
            const rect = this.canvas.parentElement.getBoundingClientRect();
            this.canvas.width = rect.width * window.devicePixelRatio;
            this.canvas.height = rect.height * window.devicePixelRatio;
            this.ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
            this.width = rect.width;
            this.height = rect.height;
            this.draw();
        }

        setValue(value) {
            this.value = Math.max(0, Math.min(value, this.max));
            this.draw();
        }

        draw() {
            const ctx = this.ctx;
            const width = this.width;
            const height = this.height;
            const centerX = width / 2;
            const centerY = height * 0.8;
            const radius = Math.min(width, height * 1.5) / 2 - 10;

            ctx.clearRect(0, 0, width, height);

            // Background arc
            ctx.beginPath();
            ctx.arc(centerX, centerY, radius, Math.PI, 0);
            ctx.strokeStyle = '#e5e7eb';
            ctx.lineWidth = 12;
            ctx.lineCap = 'round';
            ctx.stroke();

            // Value arc
            const percentage = this.value / this.max;
            const endAngle = Math.PI + (Math.PI * percentage);

            let color = this.color;
            if (percentage > 0.8) color = this.dangerColor;
            else if (percentage > 0.6) color = this.warningColor;

            ctx.beginPath();
            ctx.arc(centerX, centerY, radius, Math.PI, endAngle);
            ctx.strokeStyle = color;
            ctx.lineWidth = 12;
            ctx.lineCap = 'round';
            ctx.stroke();

            // Value text
            ctx.fillStyle = '#1f2937';
            ctx.font = 'bold 24px system-ui, -apple-system, sans-serif';
            ctx.textAlign = 'center';
            ctx.fillText(Math.round(this.value).toString(), centerX, centerY - 10);

            // Label
            ctx.fillStyle = '#6b7280';
            ctx.font = '12px system-ui, -apple-system, sans-serif';
            ctx.fillText('Active', centerX, centerY + 15);
        }

        destroy() {
            window.removeEventListener('resize', () => this.resize());
        }
    }

    // Bar Chart Component
    class BarChart {
        constructor(canvas, options = {}) {
            this.canvas = canvas;
            this.ctx = canvas.getContext('2d');
            this.data = [];
            this.color = options.color || '#3b82f6';

            this.resize();
            window.addEventListener('resize', () => this.resize());
        }

        resize() {
            const rect = this.canvas.parentElement.getBoundingClientRect();
            this.canvas.width = rect.width * window.devicePixelRatio;
            this.canvas.height = rect.height * window.devicePixelRatio;
            this.ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
            this.width = rect.width;
            this.height = rect.height;
            this.draw();
        }

        setData(data) {
            this.data = data;
            this.draw();
        }

        draw() {
            const ctx = this.ctx;
            const width = this.width;
            const height = this.height;

            ctx.clearRect(0, 0, width, height);

            if (this.data.length === 0) return;

            const max = Math.max(...this.data.map(d => d.value));
            const barWidth = (width / this.data.length) * 0.6;
            const gap = (width / this.data.length) * 0.4;

            this.data.forEach((item, i) => {
                const barHeight = (item.value / max) * (height - 30);
                const x = i * (barWidth + gap) + gap / 2;
                const y = height - barHeight - 20;

                // Bar
                ctx.fillStyle = this.color;
                ctx.fillRect(x, y, barWidth, barHeight);

                // Label
                ctx.fillStyle = '#6b7280';
                ctx.font = '10px system-ui, -apple-system, sans-serif';
                ctx.textAlign = 'center';
                ctx.fillText(item.label, x + barWidth / 2, height - 5);
            });
        }

        destroy() {
            window.removeEventListener('resize', () => this.resize());
        }
    }

    // Dashboard Page Component Definition
    const DashboardPage = {
        name: 'dashboard',

        template(props) {
            return `
                <div class="dashboard">
                    <header class="dashboard-header">
                        <h1>Dashboard</h1>
                        <div class="header-actions">
                            <span class="last-update" id="last-update">Last updated: --</span>
                            <button class="btn btn-secondary" id="refresh-btn">
                                <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                    <path d="M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/>
                                </svg>
                                Refresh
                            </button>
                        </div>
                    </header>

                    <div class="kpi-grid">
                        <div class="kpi-card clickable" data-nav="/metrics">
                            <div class="kpi-header">
                                <span class="kpi-title">Request Rate</span>
                                <span class="kpi-badge success">Live</span>
                            </div>
                            <div class="kpi-value" id="request-rate">0</div>
                            <div class="kpi-subtitle">req/sec</div>
                            <div class="sparkline-container">
                                <canvas id="request-sparkline"></canvas>
                            </div>
                        </div>

                        <div class="kpi-card clickable" data-nav="/backends">
                            <div class="kpi-header">
                                <span class="kpi-title">Active Connections</span>
                            </div>
                            <div class="gauge-container">
                                <canvas id="connections-gauge"></canvas>
                            </div>
                        </div>

                        <div class="kpi-card">
                            <div class="kpi-header">
                                <span class="kpi-title">Error Rate</span>
                                <span class="kpi-badge" id="error-trend">stable</span>
                            </div>
                            <div class="kpi-value" id="error-rate">0%</div>
                            <div class="kpi-subtitle">Last 5 minutes</div>
                        </div>

                        <div class="kpi-card clickable" data-nav="/backends">
                            <div class="kpi-header">
                                <span class="kpi-title">Backend Health</span>
                            </div>
                            <div class="health-summary">
                                <div class="health-item healthy">
                                    <span class="health-count" id="healthy-count">0</span>
                                    <span class="health-label">Healthy</span>
                                </div>
                                <div class="health-divider">/</div>
                                <div class="health-item unhealthy">
                                    <span class="health-count" id="unhealthy-count">0</span>
                                    <span class="health-label">Unhealthy</span>
                                </div>
                            </div>
                            <div class="kpi-subtitle"><span id="total-backends">0</span> total backends</div>
                        </div>
                    </div>

                    <div class="charts-section">
                        <div class="chart-card">
                            <h3>Top Routes by Traffic</h3>
                            <div class="bar-chart-container">
                                <canvas id="routes-chart"></canvas>
                            </div>
                        </div>

                        <div class="chart-card">
                            <h3>Latency Distribution</h3>
                            <div class="latency-stats">
                                <div class="latency-item">
                                    <span class="latency-label">P50</span>
                                    <span class="latency-value" id="p50-latency">0ms</span>
                                </div>
                                <div class="latency-item">
                                    <span class="latency-label">P95</span>
                                    <span class="latency-value" id="p95-latency">0ms</span>
                                </div>
                                <div class="latency-item">
                                    <span class="latency-label">P99</span>
                                    <span class="latency-value" id="p99-latency">0ms</span>
                                </div>
                            </div>
                        </div>
                    </div>

                    <div class="bottom-section">
                        <div class="panel system-panel">
                            <h3>System Resources</h3>
                            <div class="resource-grid">
                                <div class="resource-item">
                                    <span class="resource-label">CPU</span>
                                    <div class="resource-bar">
                                        <div class="resource-fill" id="cpu-bar" style="width: 0%"></div>
                                    </div>
                                    <span class="resource-value" id="cpu-value">0%</span>
                                </div>
                                <div class="resource-item">
                                    <span class="resource-label">Memory</span>
                                    <div class="resource-bar">
                                        <div class="resource-fill" id="memory-bar" style="width: 0%"></div>
                                    </div>
                                    <span class="resource-value" id="memory-value">0%</span>
                                </div>
                                <div class="resource-item">
                                    <span class="resource-label">Goroutines</span>
                                    <span class="resource-value plain" id="goroutines">0</span>
                                </div>
                                <div class="resource-item">
                                    <span class="resource-label">Uptime</span>
                                    <span class="resource-value plain" id="uptime">0h 0m</span>
                                </div>
                                <div class="resource-item">
                                    <span class="resource-label">Version</span>
                                    <span class="resource-value plain" id="version">dev</span>
                                </div>
                            </div>
                        </div>

                        <div class="panel errors-panel">
                            <h3>Recent Errors</h3>
                            <div class="errors-list" id="errors-list">
                                <div class="empty-state">
                                    <p>No recent errors</p>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            `;
        },

        mounted(element, props) {
            const page = new DashboardPageInstance(element);
            page.init();

            // Store instance for cleanup
            element._pageInstance = page;
        },

        destroyed(element) {
            if (element._pageInstance) {
                element._pageInstance.destroy();
            }
        }
    };

    // Dashboard Page Instance Class
    class DashboardPageInstance {
        constructor(element) {
            this.element = element;
            this.charts = {};
            this.refreshInterval = null;
            this.state = {
                requestRate: 0,
                activeConnections: 0,
                errorRate: 0,
                errorTrend: 'stable',
                healthyBackends: 0,
                unhealthyBackends: 0,
                totalBackends: 0,
                p50Latency: 0,
                p95Latency: 0,
                p99Latency: 0,
                cpuUsage: 0,
                memoryUsage: 0,
                goroutines: 0,
                uptime: '0h 0m',
                version: 'dev',
                recentErrors: [],
                topRoutes: []
            };
        }

        init() {
            this.initCharts();
            this.bindEvents();
            this.fetchAllData();
            this.startAutoRefresh();
        }

        initCharts() {
            // Request rate sparkline
            const sparklineCanvas = this.element.querySelector('#request-sparkline');
            if (sparklineCanvas) {
                this.charts.sparkline = new SparklineChart(sparklineCanvas, {
                    color: '#3b82f6',
                    fillColor: 'rgba(59, 130, 246, 0.1)'
                });
            }

            // Connections gauge
            const gaugeCanvas = this.element.querySelector('#connections-gauge');
            if (gaugeCanvas) {
                this.charts.gauge = new GaugeChart(gaugeCanvas, {
                    max: 10000
                });
            }

            // Routes bar chart
            const routesCanvas = this.element.querySelector('#routes-chart');
            if (routesCanvas) {
                this.charts.bar = new BarChart(routesCanvas, {
                    color: '#8b5cf6'
                });
            }
        }

        bindEvents() {
            // Refresh button
            const refreshBtn = this.element.querySelector('#refresh-btn');
            if (refreshBtn) {
                refreshBtn.addEventListener('click', () => this.fetchAllData());
            }

            // Clickable cards navigation
            this.element.querySelectorAll('.kpi-card.clickable').forEach(card => {
                card.addEventListener('click', () => {
                    const nav = card.getAttribute('data-nav');
                    if (nav && global.OLBSPA) {
                        global.OLBSPA.navigate(nav);
                    }
                });
            });

            // Error row expansion
            const errorsList = this.element.querySelector('#errors-list');
            if (errorsList) {
                errorsList.addEventListener('click', (e) => {
                    const row = e.target.closest('.error-row');
                    if (row) {
                        row.classList.toggle('expanded');
                    }
                });
            }
        }

        async fetchAllData() {
            try {
                const [metrics, backends, health] = await Promise.all([
                    this.fetchMetrics(),
                    this.fetchBackends(),
                    this.fetchHealth()
                ]);

                this.updateDashboard(metrics, backends, health);
            } catch (error) {
                console.error('Failed to fetch dashboard data:', error);
                this.showError('Failed to load dashboard data');
            }
        }

        async fetchMetrics() {
            try {
                const response = await fetch('/api/metrics');
                if (!response.ok) throw new Error('Failed to fetch metrics');
                return await response.json();
            } catch (error) {
                // Return mock data for development
                return {
                    requests: {
                        total: 1523456,
                        rate: Math.random() * 1000 + 500
                    },
                    connections: {
                        active: Math.floor(Math.random() * 5000 + 1000),
                        total: 1234567
                    },
                    errors: {
                        rate: Math.random() * 2,
                        total: 1234
                    },
                    latency: {
                        p50: Math.random() * 20 + 10,
                        p95: Math.random() * 50 + 30,
                        p99: Math.random() * 100 + 50
                    },
                    routes: [
                        { path: '/api/users', requests: 45000 },
                        { path: '/api/orders', requests: 38000 },
                        { path: '/api/products', requests: 32000 },
                        { path: '/health', requests: 28000 },
                        { path: '/api/search', requests: 21000 }
                    ]
                };
            }
        }

        async fetchBackends() {
            try {
                const response = await fetch('/api/backends');
                if (!response.ok) throw new Error('Failed to fetch backends');
                return await response.json();
            } catch (error) {
                // Return mock data for development
                return {
                    backends: [
                        { id: '1', name: 'backend-1', healthy: true },
                        { id: '2', name: 'backend-2', healthy: true },
                        { id: '3', name: 'backend-3', healthy: true },
                        { id: '4', name: 'backend-4', healthy: false },
                        { id: '5', name: 'backend-5', healthy: true }
                    ]
                };
            }
        }

        async fetchHealth() {
            try {
                const response = await fetch('/api/health');
                if (!response.ok) throw new Error('Failed to fetch health');
                return await response.json();
            } catch (error) {
                // Return mock data for development
                return {
                    status: 'healthy',
                    uptime: '72h 15m 30s',
                    version: 'v1.0.0',
                    system: {
                        cpu: Math.random() * 30 + 10,
                        memory: Math.random() * 40 + 20,
                        goroutines: 1234
                    }
                };
            }
        }

        updateDashboard(metrics, backends, health) {
            const healthyCount = backends.backends.filter(b => b.healthy).length;
            const unhealthyCount = backends.backends.length - healthyCount;

            // Update error trend
            let errorTrend = 'stable';
            if (metrics.errors.rate > 1) errorTrend = 'increasing';
            else if (metrics.errors.rate < 0.1) errorTrend = 'decreasing';

            // Update sparkline
            if (this.charts.sparkline) {
                this.charts.sparkline.push(metrics.requests.rate);
            }

            // Update gauge
            if (this.charts.gauge) {
                this.charts.gauge.setValue(metrics.connections.active);
            }

            // Update bar chart
            if (this.charts.bar) {
                this.charts.bar.setData(metrics.routes.map(r => ({
                    label: r.path.replace('/api/', ''),
                    value: r.requests
                })));
            }

            // Update DOM elements
            this.updateElement('request-rate', this.formatNumber(Math.round(metrics.requests.rate)));
            this.updateElement('error-rate', metrics.errors.rate.toFixed(2) + '%');
            this.updateElement('error-trend', errorTrend, (el) => {
                el.className = 'kpi-badge ' + errorTrend;
            });
            this.updateElement('healthy-count', healthyCount);
            this.updateElement('unhealthy-count', unhealthyCount);
            this.updateElement('total-backends', backends.backends.length);
            this.updateElement('p50-latency', Math.round(metrics.latency.p50) + 'ms');
            this.updateElement('p95-latency', Math.round(metrics.latency.p95) + 'ms');
            this.updateElement('p99-latency', Math.round(metrics.latency.p99) + 'ms');
            this.updateElement('cpu-bar', null, (el) => {
                el.style.width = Math.round(health.system.cpu) + '%';
            });
            this.updateElement('cpu-value', Math.round(health.system.cpu) + '%');
            this.updateElement('memory-bar', null, (el) => {
                el.style.width = Math.round(health.system.memory) + '%';
            });
            this.updateElement('memory-value', Math.round(health.system.memory) + '%');
            this.updateElement('goroutines', health.system.goroutines);
            this.updateElement('uptime', health.uptime || '0h 0m');
            this.updateElement('version', health.version);
            this.updateElement('last-update', 'Last updated: ' + new Date().toLocaleTimeString());

            // Update recent errors
            this.updateRecentErrors();
        }

        updateElement(id, value, callback) {
            const el = this.element.querySelector('#' + id);
            if (el) {
                if (callback) {
                    callback(el);
                } else if (value !== null) {
                    el.textContent = value;
                }
            }
        }

        updateRecentErrors() {
            // Mock error data - in production this would come from an API
            const mockErrors = [
                {
                    time: '2 min ago',
                    route: '/api/orders',
                    status: '502',
                    message: 'Bad Gateway: upstream connect error',
                    backend: 'backend-4'
                },
                {
                    time: '5 min ago',
                    route: '/api/users',
                    status: '504',
                    message: 'Gateway Timeout',
                    backend: 'backend-2'
                },
                {
                    time: '12 min ago',
                    route: '/api/search',
                    status: '500',
                    message: 'Internal Server Error',
                    backend: 'backend-3'
                }
            ];

            const errorsList = this.element.querySelector('#errors-list');
            if (errorsList && mockErrors.length > 0) {
                // Clear existing content
                errorsList.innerHTML = '';

                // Build error rows using safe DOM methods
                mockErrors.forEach(error => {
                    const row = document.createElement('div');
                    row.className = 'error-row';

                    const summary = document.createElement('div');
                    summary.className = 'error-summary';

                    const timeSpan = document.createElement('span');
                    timeSpan.className = 'error-time';
                    timeSpan.textContent = error.time;

                    const routeSpan = document.createElement('span');
                    routeSpan.className = 'error-route';
                    routeSpan.textContent = error.route;

                    const statusSpan = document.createElement('span');
                    statusSpan.className = 'error-status';
                    statusSpan.textContent = error.status;

                    summary.appendChild(timeSpan);
                    summary.appendChild(routeSpan);
                    summary.appendChild(statusSpan);

                    const details = document.createElement('div');
                    details.className = 'error-details';

                    const msgP = document.createElement('p');
                    msgP.textContent = error.message;

                    const backendP = document.createElement('p');
                    backendP.className = 'error-backend';
                    backendP.textContent = 'Backend: ' + error.backend;

                    details.appendChild(msgP);
                    details.appendChild(backendP);

                    row.appendChild(summary);
                    row.appendChild(details);
                    errorsList.appendChild(row);
                });
            }
        }

        formatNumber(num) {
            if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
            if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
            return num.toString();
        }

        startAutoRefresh() {
            this.refreshInterval = setInterval(() => {
                this.fetchAllData();
            }, 5000);
        }

        stopAutoRefresh() {
            if (this.refreshInterval) {
                clearInterval(this.refreshInterval);
                this.refreshInterval = null;
            }
        }

        showError(message) {
            console.error(message);
            // Could integrate with a toast notification system
        }

        destroy() {
            this.stopAutoRefresh();
            Object.values(this.charts).forEach(chart => {
                if (chart && typeof chart.destroy === 'function') {
                    chart.destroy();
                }
            });
        }
    }

    // Register the component
    if (global.OLBSPA) {
        global.OLBSPA.component('dashboard', DashboardPage);
    }

    // Also expose for direct use
    global.DashboardPage = DashboardPage;

})(window);

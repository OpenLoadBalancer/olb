// metrics.js - Metrics page for OpenLoadBalancer Web UI
// Phase 3.5: Interactive metrics visualization with time-series charts

(function() {
    'use strict';

    // Time range presets
    const TIME_RANGES = {
        '1h': { label: '1 Hour', seconds: 3600 },
        '6h': { label: '6 Hours', seconds: 21600 },
        '24h': { label: '24 Hours', seconds: 86400 },
        '7d': { label: '7 Days', seconds: 604800 }
    };

    // Chart colors
    const CHART_COLORS = {
        primary: '#3b82f6',
        secondary: '#10b981',
        warning: '#f59e0b',
        error: '#ef4444',
        info: '#06b6d4',
        purple: '#8b5cf6',
        pink: '#ec4899',
        gray: '#6b7280'
    };

    // Metrics page state
    const state = {
        timeRange: '1h',
        customStart: null,
        customEnd: null,
        charts: {},
        metrics: {},
        selectedMetrics: new Set(),
        refreshInterval: null,
        chartBuilder: {
            metric: null,
            aggregation: 'avg',
            timeRange: '1h'
        }
    };

    /**
     * Initialize the metrics page
     */
    function init() {
        renderPage();
        initCharts();
        initEventListeners();
        loadMetrics();
        startAutoRefresh();
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

        container.innerHTML = '';

        // Main page container
        const page = createElement('div', { className: 'metrics-page' });

        // Header
        const header = createElement('header', { className: 'page-header' });
        const title = createElement('h1', { textContent: 'Metrics' });
        const headerActions = createElement('div', { className: 'header-actions' });

        // Time range selector
        const timeRangeSelector = createElement('div', { className: 'time-range-selector' });
        Object.entries(TIME_RANGES).forEach(([key, range]) => {
            const btn = createElement('button', {
                className: 'btn-time-range' + (state.timeRange === key ? ' active' : ''),
                'data-range': key,
                textContent: range.label
            });
            timeRangeSelector.appendChild(btn);
        });
        const customBtn = createElement('button', {
            className: 'btn-time-range' + (state.timeRange === 'custom' ? ' active' : ''),
            'data-range': 'custom',
            textContent: 'Custom'
        });
        timeRangeSelector.appendChild(customBtn);

        // Export actions
        const exportActions = createElement('div', { className: 'export-actions' });
        ['json', 'csv', 'prometheus'].forEach(format => {
            const btn = createElement('button', {
                className: 'btn-export',
                'data-format': format,
                textContent: 'Export ' + format.charAt(0).toUpperCase() + format.slice(1)
            });
            exportActions.appendChild(btn);
        });

        headerActions.appendChild(timeRangeSelector);
        headerActions.appendChild(exportActions);
        header.appendChild(title);
        header.appendChild(headerActions);
        page.appendChild(header);

        // Custom time range
        const customTimeRange = createElement('div', {
            className: 'custom-time-range' + (state.timeRange === 'custom' ? ' visible' : '')
        });
        const startInput = createElement('input', { type: 'datetime-local', id: 'custom-start' });
        const toSpan = createElement('span', { textContent: ' to ' });
        const endInput = createElement('input', { type: 'datetime-local', id: 'custom-end' });
        const applyBtn = createElement('button', { className: 'btn-primary', id: 'apply-custom-range', textContent: 'Apply' });
        customTimeRange.appendChild(startInput);
        customTimeRange.appendChild(toSpan);
        customTimeRange.appendChild(endInput);
        customTimeRange.appendChild(applyBtn);
        page.appendChild(customTimeRange);

        // Metrics grid
        const metricsGrid = createElement('div', { className: 'metrics-grid' });

        // Chart cards
        const chartConfigs = [
            { id: 'request-rate-chart', title: 'Request Rate', valueId: 'request-rate-value' },
            { id: 'error-rate-chart', title: 'Error Rate', valueId: 'error-rate-value' },
            { id: 'latency-chart', title: 'Latency Percentiles', valueId: 'latency-value' },
            { id: 'backend-health-chart', title: 'Backend Health', valueId: 'backend-health-value' }
        ];

        chartConfigs.forEach(config => {
            const card = createElement('div', { className: 'chart-card' });
            const chartHeader = createElement('div', { className: 'chart-header' });
            const chartTitle = createElement('h3', { textContent: config.title });
            const chartValue = createElement('span', { className: 'chart-value', id: config.valueId, textContent: '-' });
            chartHeader.appendChild(chartTitle);
            chartHeader.appendChild(chartValue);

            const canvas = createElement('canvas', { id: config.id, width: '400', height: '200' });

            card.appendChild(chartHeader);
            card.appendChild(canvas);
            metricsGrid.appendChild(card);
        });

        page.appendChild(metricsGrid);

        // Metrics explorer
        const explorer = createElement('div', { className: 'metrics-explorer' });
        const explorerHeader = createElement('div', { className: 'explorer-header' });
        const explorerTitle = createElement('h2', { textContent: 'Metric Explorer' });
        const explorerSearch = createElement('div', { className: 'explorer-search' });
        const searchInput = createElement('input', { type: 'text', id: 'metric-search', placeholder: 'Search metrics...' });
        const clearBtn = createElement('button', { className: 'btn-icon', id: 'clear-search', textContent: 'Clear' });
        explorerSearch.appendChild(searchInput);
        explorerSearch.appendChild(clearBtn);
        explorerHeader.appendChild(explorerTitle);
        explorerHeader.appendChild(explorerSearch);

        const explorerContent = createElement('div', { className: 'explorer-content' });
        const metricTree = createElement('div', { className: 'metric-tree', id: 'metric-tree' });
        const metricDetails = createElement('div', { className: 'metric-details', id: 'metric-details' });
        const placeholder = createElement('p', { className: 'placeholder', textContent: 'Select a metric to view details' });
        metricDetails.appendChild(placeholder);
        explorerContent.appendChild(metricTree);
        explorerContent.appendChild(metricDetails);

        explorer.appendChild(explorerHeader);
        explorer.appendChild(explorerContent);
        page.appendChild(explorer);

        // Chart builder
        const chartBuilder = createElement('div', { className: 'chart-builder' });
        const builderHeader = createElement('div', { className: 'builder-header' });
        const builderTitle = createElement('h2', { textContent: 'Custom Chart Builder' });
        builderHeader.appendChild(builderTitle);

        const builderControls = createElement('div', { className: 'builder-controls' });

        // Metric select
        const metricGroup = createElement('div', { className: 'control-group' });
        const metricLabel = createElement('label', { textContent: 'Metric' });
        const metricSelect = createElement('select', { id: 'builder-metric' });
        const defaultOption = createElement('option', { value: '', textContent: 'Select a metric...' });
        metricSelect.appendChild(defaultOption);
        metricGroup.appendChild(metricLabel);
        metricGroup.appendChild(metricSelect);

        // Aggregation select
        const aggGroup = createElement('div', { className: 'control-group' });
        const aggLabel = createElement('label', { textContent: 'Aggregation' });
        const aggSelect = createElement('select', { id: 'builder-aggregation' });
        ['avg', 'sum', 'min', 'max', 'count'].forEach(agg => {
            const option = createElement('option', { value: agg, textContent: agg.charAt(0).toUpperCase() + agg.slice(1) });
            aggSelect.appendChild(option);
        });
        aggGroup.appendChild(aggLabel);
        aggGroup.appendChild(aggSelect);

        // Time range select
        const rangeGroup = createElement('div', { className: 'control-group' });
        const rangeLabel = createElement('label', { textContent: 'Time Range' });
        const rangeSelect = createElement('select', { id: 'builder-time-range' });
        Object.entries(TIME_RANGES).forEach(([key, range]) => {
            const option = createElement('option', { value: key, textContent: range.label });
            rangeSelect.appendChild(option);
        });
        rangeGroup.appendChild(rangeLabel);
        rangeGroup.appendChild(rangeSelect);

        const buildBtn = createElement('button', { className: 'btn-primary', id: 'build-chart', textContent: 'Build Chart' });

        builderControls.appendChild(metricGroup);
        builderControls.appendChild(aggGroup);
        builderControls.appendChild(rangeGroup);
        builderControls.appendChild(buildBtn);

        const builderChart = createElement('div', { className: 'builder-chart' });
        const customCanvas = createElement('canvas', { id: 'custom-chart', width: '800', height: '300' });
        builderChart.appendChild(customCanvas);

        chartBuilder.appendChild(builderHeader);
        chartBuilder.appendChild(builderControls);
        chartBuilder.appendChild(builderChart);
        page.appendChild(chartBuilder);

        container.appendChild(page);
    }

    /**
     * Initialize canvas charts
     */
    function initCharts() {
        const chartIds = ['request-rate-chart', 'error-rate-chart', 'latency-chart', 'backend-health-chart'];

        chartIds.forEach(id => {
            const canvas = document.getElementById(id);
            if (canvas) {
                state.charts[id] = new TimeSeriesChart(canvas);
            }
        });
    }

    /**
     * Initialize event listeners
     */
    function initEventListeners() {
        // Time range selector
        document.querySelectorAll('.btn-time-range').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const range = e.target.dataset.range;
                setTimeRange(range);
            });
        });

        // Custom time range
        const applyCustomBtn = document.getElementById('apply-custom-range');
        if (applyCustomBtn) {
            applyCustomBtn.addEventListener('click', applyCustomTimeRange);
        }

        // Export buttons
        document.querySelectorAll('.btn-export').forEach(btn => {
            btn.addEventListener('click', (e) => {
                exportMetrics(e.target.dataset.format);
            });
        });

        // Metric search
        const searchInput = document.getElementById('metric-search');
        if (searchInput) {
            let debounceTimer;
            searchInput.addEventListener('input', (e) => {
                clearTimeout(debounceTimer);
                debounceTimer = setTimeout(() => searchMetrics(e.target.value), 300);
            });
        }

        // Clear search
        const clearBtn = document.getElementById('clear-search');
        if (clearBtn) {
            clearBtn.addEventListener('click', () => {
                const searchInput = document.getElementById('metric-search');
                if (searchInput) {
                    searchInput.value = '';
                    searchMetrics('');
                }
            });
        }

        // Chart builder
        const buildChartBtn = document.getElementById('build-chart');
        if (buildChartBtn) {
            buildChartBtn.addEventListener('click', buildCustomChart);
        }
    }

    /**
     * Set the time range
     */
    function setTimeRange(range) {
        state.timeRange = range;

        // Update UI
        document.querySelectorAll('.btn-time-range').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.range === range);
        });

        // Show/hide custom range inputs
        const customRangeDiv = document.querySelector('.custom-time-range');
        if (customRangeDiv) {
            customRangeDiv.classList.toggle('visible', range === 'custom');
        }

        // Reload data
        loadMetrics();
    }

    /**
     * Apply custom time range
     */
    function applyCustomTimeRange() {
        const startInput = document.getElementById('custom-start');
        const endInput = document.getElementById('custom-end');

        if (startInput && endInput && startInput.value && endInput.value) {
            state.customStart = new Date(startInput.value).getTime();
            state.customEnd = new Date(endInput.value).getTime();
            loadMetrics();
        }
    }

    /**
     * Load metrics data from API
     */
    async function loadMetrics() {
        try {
            const params = new URLSearchParams();

            if (state.timeRange === 'custom' && state.customStart && state.customEnd) {
                params.append('start', Math.floor(state.customStart / 1000));
                params.append('end', Math.floor(state.customEnd / 1000));
            } else {
                const range = TIME_RANGES[state.timeRange];
                if (range) {
                    const end = Math.floor(Date.now() / 1000);
                    const start = end - range.seconds;
                    params.append('start', start);
                    params.append('end', end);
                }
            }

            const response = await fetch('/api/v1/metrics?' + params.toString());
            const result = await response.json();

            if (result.success && result.data) {
                state.metrics = result.data;
                updateCharts();
                updateMetricTree();
                updateBuilderMetricSelect();
            }
        } catch (error) {
            console.error('Failed to load metrics:', error);
        }
    }

    /**
     * Update all charts with current data
     */
    function updateCharts() {
        const data = state.metrics;

        // Request rate chart
        const requestRateData = extractTimeSeries(data, 'requests_total', 'rate');
        updateChart('request-rate-chart', requestRateData, CHART_COLORS.primary);
        updateChartValue('request-rate-value', requestRateData, 'req/s');

        // Error rate chart
        const errorRateData = extractTimeSeries(data, 'errors_total', 'rate');
        updateChart('error-rate-chart', errorRateData, CHART_COLORS.error);
        updateChartValue('error-rate-value', errorRateData, 'err/s');

        // Latency percentiles
        const latencyData = extractLatencyPercentiles(data);
        updateLatencyChart(latencyData);
        updateLatencyValue('latency-value', latencyData);

        // Backend health
        const healthData = extractBackendHealth(data);
        updateChart('backend-health-chart', healthData, CHART_COLORS.secondary);
        updateHealthValue('backend-health-value', healthData);
    }

    /**
     * Extract time series data from metrics
     */
    function extractTimeSeries(metrics, metricName, aggregation) {
        const series = [];
        const metric = metrics[metricName];

        if (!metric) return series;

        if (metric.type === 'counter_vec' || metric.type === 'gauge_vec') {
            Object.entries(metric.values || {}).forEach(([labels, value]) => {
                series.push({
                    label: labels,
                    value: typeof value === 'number' ? value : 0,
                    timestamp: Date.now()
                });
            });
        } else if (metric.type === 'counter' || metric.type === 'gauge') {
            series.push({
                label: metricName,
                value: metric.value || 0,
                timestamp: Date.now()
            });
        }

        return series;
    }

    /**
     * Extract latency percentiles from histogram data
     */
    function extractLatencyPercentiles(metrics) {
        const histogram = metrics['request_duration_seconds'];
        if (!histogram || histogram.type !== 'histogram') {
            return { p50: 0, p90: 0, p95: 0, p99: 0 };
        }

        const buckets = histogram.buckets || {};
        const count = histogram.count || 0;

        if (count === 0) {
            return { p50: 0, p90: 0, p95: 0, p99: 0 };
        }

        return {
            p50: calculatePercentile(buckets, count, 0.5),
            p90: calculatePercentile(buckets, count, 0.9),
            p95: calculatePercentile(buckets, count, 0.95),
            p99: calculatePercentile(buckets, count, 0.99)
        };
    }

    /**
     * Calculate percentile from histogram buckets
     */
    function calculatePercentile(buckets, totalCount, percentile) {
        const sortedBuckets = Object.entries(buckets)
            .filter(([bound]) => bound !== '+Inf')
            .map(([bound, count]) => ({ bound: parseFloat(bound), count }))
            .sort((a, b) => a.bound - b.bound);

        const targetCount = totalCount * percentile;
        let cumulativeCount = 0;

        for (const bucket of sortedBuckets) {
            cumulativeCount += bucket.count;
            if (cumulativeCount >= targetCount) {
                return bucket.bound;
            }
        }

        return sortedBuckets.length > 0 ? sortedBuckets[sortedBuckets.length - 1].bound : 0;
    }

    /**
     * Extract backend health data
     */
    function extractBackendHealth(metrics) {
        const series = [];
        const healthMetric = metrics['backend_health_status'];

        if (healthMetric && healthMetric.type === 'gauge_vec') {
            Object.entries(healthMetric.values || {}).forEach(([labels, value]) => {
                series.push({
                    label: labels,
                    value: value === 1 ? 100 : 0,  // Convert to percentage
                    timestamp: Date.now()
                });
            });
        }

        return series;
    }

    /**
     * Update a chart with new data
     */
    function updateChart(chartId, data, color) {
        const chart = state.charts[chartId];
        if (chart) {
            chart.setData(data, color);
        }
    }

    /**
     * Update latency chart with percentile data
     */
    function updateLatencyChart(data) {
        const chart = state.charts['latency-chart'];
        if (!chart) return;

        const seriesData = [
            { label: 'p50', value: data.p50, color: CHART_COLORS.info },
            { label: 'p90', value: data.p90, color: CHART_COLORS.warning },
            { label: 'p95', value: data.p95, color: CHART_COLORS.secondary },
            { label: 'p99', value: data.p99, color: CHART_COLORS.error }
        ];

        chart.setMultiSeriesData(seriesData);
    }

    /**
     * Update chart value display
     */
    function updateChartValue(elementId, data, unit) {
        const element = document.getElementById(elementId);
        if (!element) return;

        if (data.length === 0) {
            element.textContent = '-';
            return;
        }

        const total = data.reduce((sum, d) => sum + d.value, 0);
        const avg = total / data.length;
        element.textContent = formatValue(avg, unit);
    }

    /**
     * Update latency value display
     */
    function updateLatencyValue(elementId, data) {
        const element = document.getElementById(elementId);
        if (!element) return;

        const avg = (data.p50 + data.p90 + data.p95 + data.p99) / 4;
        element.textContent = formatDuration(avg);
    }

    /**
     * Update health value display
     */
    function updateHealthValue(elementId, data) {
        const element = document.getElementById(elementId);
        if (!element) return;

        if (data.length === 0) {
            element.textContent = '-';
            return;
        }

        const healthy = data.filter(d => d.value === 100).length;
        const percentage = (healthy / data.length) * 100;
        element.textContent = percentage.toFixed(1) + '%';
    }

    /**
     * Format a numeric value with unit
     */
    function formatValue(value, unit) {
        if (value >= 1000000) {
            return (value / 1000000).toFixed(2) + 'M ' + unit;
        } else if (value >= 1000) {
            return (value / 1000).toFixed(2) + 'K ' + unit;
        } else if (value >= 1) {
            return value.toFixed(2) + ' ' + unit;
        } else {
            return value.toFixed(4) + ' ' + unit;
        }
    }

    /**
     * Format duration in seconds to human readable
     */
    function formatDuration(seconds) {
        if (seconds >= 1) {
            return seconds.toFixed(2) + 's';
        } else if (seconds >= 0.001) {
            return (seconds * 1000).toFixed(2) + 'ms';
        } else {
            return (seconds * 1000000).toFixed(2) + 'us';
        }
    }

    /**
     * Update metric tree view
     */
    function updateMetricTree() {
        const treeContainer = document.getElementById('metric-tree');
        if (!treeContainer) return;

        treeContainer.innerHTML = '';

        const categories = categorizeMetrics(state.metrics);

        Object.entries(categories).forEach(([category, metrics]) => {
            const categoryDiv = createElement('div', { className: 'metric-category' });

            const header = createElement('div', {
                className: 'category-header',
                'data-category': category
            });
            const toggle = createElement('span', { className: 'category-toggle', textContent: '▼' });
            const name = createElement('span', { className: 'category-name', textContent: category });
            const count = createElement('span', { className: 'category-count', textContent: metrics.length.toString() });
            header.appendChild(toggle);
            header.appendChild(name);
            header.appendChild(count);

            const metricsDiv = createElement('div', { className: 'category-metrics' });

            metrics.forEach(metric => {
                const item = createElement('div', {
                    className: 'metric-item',
                    'data-metric': metric.name
                });
                const nameSpan = createElement('span', { className: 'metric-name', textContent: metric.name });
                const typeSpan = createElement('span', { className: 'metric-type', textContent: metric.type });
                const valueSpan = createElement('span', { className: 'metric-value', textContent: formatMetricValue(metric) });
                const canvas = createElement('canvas', { className: 'sparkline', width: '100', height: '20' });

                item.appendChild(nameSpan);
                item.appendChild(typeSpan);
                item.appendChild(valueSpan);
                item.appendChild(canvas);
                metricsDiv.appendChild(item);

                // Add click handler
                item.addEventListener('click', () => showMetricDetails(metric.name));
            });

            // Toggle handler
            header.addEventListener('click', () => {
                metricsDiv.classList.toggle('collapsed');
                toggle.textContent = metricsDiv.classList.contains('collapsed') ? '▶' : '▼';
            });

            categoryDiv.appendChild(header);
            categoryDiv.appendChild(metricsDiv);
            treeContainer.appendChild(categoryDiv);
        });

        // Draw sparklines
        drawSparklines();
    }

    /**
     * Categorize metrics by name prefix
     */
    function categorizeMetrics(metrics) {
        const categories = {};

        Object.entries(metrics).forEach(([name, metric]) => {
            const category = name.split('_')[0] || 'other';
            if (!categories[category]) {
                categories[category] = [];
            }
            categories[category].push({ name, ...metric });
        });

        return categories;
    }

    /**
     * Format metric value for display
     */
    function formatMetricValue(metric) {
        if (metric.type === 'counter' || metric.type === 'gauge') {
            return formatCompactNumber(metric.value);
        } else if (metric.type === 'histogram') {
            return 'count: ' + formatCompactNumber(metric.count);
        } else if (metric.type === 'counter_vec' || metric.type === 'gauge_vec') {
            const values = Object.values(metric.values || {});
            const sum = values.reduce((a, b) => a + (typeof b === 'number' ? b : 0), 0);
            return formatCompactNumber(sum);
        }
        return '-';
    }

    /**
     * Format compact number
     */
    function formatCompactNumber(num) {
        if (num >= 1e9) return (num / 1e9).toFixed(1) + 'B';
        if (num >= 1e6) return (num / 1e6).toFixed(1) + 'M';
        if (num >= 1e3) return (num / 1e3).toFixed(1) + 'K';
        return num.toString();
    }

    /**
     * Draw sparklines for metrics
     */
    function drawSparklines() {
        document.querySelectorAll('.sparkline').forEach(canvas => {
            const ctx = canvas.getContext('2d');
            const width = canvas.width;
            const height = canvas.height;

            // Generate sample data (in real implementation, fetch historical data)
            const data = generateSparklineData();

            ctx.clearRect(0, 0, width, height);
            ctx.strokeStyle = CHART_COLORS.primary;
            ctx.lineWidth = 1.5;
            ctx.beginPath();

            const min = Math.min(...data);
            const max = Math.max(...data);
            const range = max - min || 1;

            data.forEach((value, i) => {
                const x = (i / (data.length - 1)) * width;
                const y = height - ((value - min) / range) * height;

                if (i === 0) {
                    ctx.moveTo(x, y);
                } else {
                    ctx.lineTo(x, y);
                }
            });

            ctx.stroke();
        });
    }

    /**
     * Generate sample sparkline data
     */
    function generateSparklineData() {
        const data = [];
        let value = 50;
        for (let i = 0; i < 20; i++) {
            value += (Math.random() - 0.5) * 20;
            value = Math.max(10, Math.min(90, value));
            data.push(value);
        }
        return data;
    }

    /**
     * Show metric details
     */
    function showMetricDetails(metricName) {
        const detailsContainer = document.getElementById('metric-details');
        if (!detailsContainer) return;

        const metric = state.metrics[metricName];
        if (!metric) return;

        detailsContainer.innerHTML = '';

        const header = createElement('div', { className: 'metric-detail-header' });
        const title = createElement('h3', { textContent: metricName });
        const badge = createElement('span', { className: 'metric-type-badge', textContent: metric.type });
        header.appendChild(title);
        header.appendChild(badge);

        const help = createElement('div', { className: 'metric-detail-help' });
        const helpText = createElement('p', { textContent: metric.help || 'No description available' });
        help.appendChild(helpText);

        const values = createElement('div', { className: 'metric-detail-values' });
        values.innerHTML = renderMetricValues(metric);

        const chartDiv = createElement('div', { className: 'metric-detail-chart' });
        const canvas = createElement('canvas', { id: 'detail-chart', width: '400', height: '200' });
        chartDiv.appendChild(canvas);

        detailsContainer.appendChild(header);
        detailsContainer.appendChild(help);
        detailsContainer.appendChild(values);
        detailsContainer.appendChild(chartDiv);

        // Draw detail chart
        const chartCanvas = document.getElementById('detail-chart');
        if (chartCanvas) {
            const chart = new TimeSeriesChart(chartCanvas);
            const data = extractTimeSeries({ [metricName]: metric }, metricName, 'value');
            chart.setData(data, CHART_COLORS.primary);
        }
    }

    /**
     * Render metric values based on type
     */
    function renderMetricValues(metric) {
        if (metric.type === 'counter' || metric.type === 'gauge') {
            return '<div class="detail-value"><span class="value-label">Current Value</span><span class="value-number">' + metric.value + '</span></div>';
        } else if (metric.type === 'histogram') {
            return '<div class="detail-value"><span class="value-label">Count</span><span class="value-number">' + metric.count + '</span></div>' +
                   '<div class="detail-value"><span class="value-label">Sum</span><span class="value-number">' + metric.sum.toFixed(4) + '</span></div>';
        } else if (metric.type === 'counter_vec' || metric.type === 'gauge_vec') {
            return Object.entries(metric.values || {}).map(([labels, value]) => {
                return '<div class="detail-value"><span class="value-label">' + labels + '</span><span class="value-number">' + value + '</span></div>';
            }).join('');
        }
        return '';
    }

    /**
     * Search metrics
     */
    function searchMetrics(query) {
        const items = document.querySelectorAll('.metric-item');
        const categories = document.querySelectorAll('.metric-category');

        const lowerQuery = query.toLowerCase();

        items.forEach(item => {
            const name = item.dataset.metric.toLowerCase();
            const visible = name.includes(lowerQuery);
            item.style.display = visible ? '' : 'none';
        });

        // Hide empty categories
        categories.forEach(category => {
            const visibleItems = category.querySelectorAll('.metric-item:not([style*="none"])');
            category.style.display = visibleItems.length > 0 ? '' : 'none';
        });
    }

    /**
     * Update chart builder metric select
     */
    function updateBuilderMetricSelect() {
        const select = document.getElementById('builder-metric');
        if (!select) return;

        // Keep first option
        const firstOption = select.querySelector('option');
        select.innerHTML = '';
        if (firstOption) select.appendChild(firstOption);

        Object.keys(state.metrics).forEach(name => {
            const option = createElement('option', { value: name, textContent: name });
            select.appendChild(option);
        });
    }

    /**
     * Build custom chart
     */
    function buildCustomChart() {
        const metricSelect = document.getElementById('builder-metric');
        const aggregationSelect = document.getElementById('builder-aggregation');
        const timeRangeSelect = document.getElementById('builder-time-range');

        if (!metricSelect || !aggregationSelect || !timeRangeSelect) return;

        const metricName = metricSelect.value;
        const aggregation = aggregationSelect.value;
        const timeRange = timeRangeSelect.value;

        if (!metricName) return;

        // Fetch data with selected parameters
        fetchCustomChartData(metricName, aggregation, timeRange);
    }

    /**
     * Fetch custom chart data
     */
    async function fetchCustomChartData(metric, aggregation, timeRange) {
        try {
            const range = TIME_RANGES[timeRange];
            const end = Math.floor(Date.now() / 1000);
            const start = end - (range ? range.seconds : 3600);

            const params = new URLSearchParams({
                metric,
                aggregation,
                start: start.toString(),
                end: end.toString()
            });

            const response = await fetch('/api/v1/metrics/timeseries?' + params.toString());
            const result = await response.json();

            if (result.success && result.data) {
                const canvas = document.getElementById('custom-chart');
                if (canvas) {
                    const chart = new TimeSeriesChart(canvas);
                    chart.setData(result.data.points, CHART_COLORS.primary);
                }
            }
        } catch (error) {
            console.error('Failed to fetch custom chart data:', error);
        }
    }

    /**
     * Export metrics in specified format
     */
    async function exportMetrics(format) {
        try {
            let endpoint;
            let filename;
            let mimeType;

            switch (format) {
                case 'json':
                    endpoint = '/api/v1/metrics';
                    filename = 'metrics.json';
                    mimeType = 'application/json';
                    break;
                case 'csv':
                    endpoint = '/api/v1/metrics?format=csv';
                    filename = 'metrics.csv';
                    mimeType = 'text/csv';
                    break;
                case 'prometheus':
                    endpoint = '/metrics';
                    filename = 'metrics.prom';
                    mimeType = 'text/plain';
                    break;
                default:
                    return;
            }

            const response = await fetch(endpoint);
            const data = await response.text();

            // Create download
            const blob = new Blob([data], { type: mimeType });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            URL.revokeObjectURL(url);
        } catch (error) {
            console.error('Failed to export metrics:', error);
        }
    }

    /**
     * Start auto-refresh
     */
    function startAutoRefresh() {
        if (state.refreshInterval) {
            clearInterval(state.refreshInterval);
        }

        state.refreshInterval = setInterval(() => {
            loadMetrics();
        }, 30000); // Refresh every 30 seconds
    }

    /**
     * Stop auto-refresh
     */
    function stopAutoRefresh() {
        if (state.refreshInterval) {
            clearInterval(state.refreshInterval);
            state.refreshInterval = null;
        }
    }

    /**
     * TimeSeriesChart - Canvas-based time series chart
     */
    class TimeSeriesChart {
        constructor(canvas) {
            this.canvas = canvas;
            this.ctx = canvas.getContext('2d');
            this.data = [];
            this.multiSeriesData = [];
            this.color = CHART_COLORS.primary;
            this.padding = { top: 20, right: 20, bottom: 30, left: 50 };
        }

        setData(data, color) {
            this.data = data || [];
            this.color = color || this.color;
            this.multiSeriesData = [];
            this.draw();
        }

        setMultiSeriesData(data) {
            this.multiSeriesData = data || [];
            this.data = [];
            this.draw();
        }

        draw() {
            const ctx = this.ctx;
            const width = this.canvas.width;
            const height = this.canvas.height;
            const { top, right, bottom, left } = this.padding;

            ctx.clearRect(0, 0, width, height);

            // Draw grid
            this.drawGrid(width, height, top, right, bottom, left);

            // Draw data
            if (this.multiSeriesData.length > 0) {
                this.drawMultiSeries(width, height, top, right, bottom, left);
            } else if (this.data.length > 0) {
                this.drawSingleSeries(width, height, top, right, bottom, left);
            }

            // Draw axes
            this.drawAxes(width, height, top, right, bottom, left);
        }

        drawGrid(width, height, top, right, bottom, left) {
            const ctx = this.ctx;
            const chartWidth = width - left - right;
            const chartHeight = height - top - bottom;

            ctx.strokeStyle = '#e5e7eb';
            ctx.lineWidth = 1;

            // Horizontal grid lines
            for (let i = 0; i <= 5; i++) {
                const y = top + (chartHeight / 5) * i;
                ctx.beginPath();
                ctx.moveTo(left, y);
                ctx.lineTo(width - right, y);
                ctx.stroke();
            }

            // Vertical grid lines
            for (let i = 0; i <= 5; i++) {
                const x = left + (chartWidth / 5) * i;
                ctx.beginPath();
                ctx.moveTo(x, top);
                ctx.lineTo(x, height - bottom);
                ctx.stroke();
            }
        }

        drawSingleSeries(width, height, top, right, bottom, left) {
            const ctx = this.ctx;
            const chartWidth = width - left - right;
            const chartHeight = height - top - bottom;

            const values = this.data.map(d => d.value);
            const min = Math.min(...values);
            const max = Math.max(...values);
            const range = max - min || 1;

            ctx.strokeStyle = this.color;
            ctx.lineWidth = 2;
            ctx.beginPath();

            this.data.forEach((point, i) => {
                const x = left + (i / (this.data.length - 1 || 1)) * chartWidth;
                const y = top + chartHeight - ((point.value - min) / range) * chartHeight;

                if (i === 0) {
                    ctx.moveTo(x, y);
                } else {
                    ctx.lineTo(x, y);
                }
            });

            ctx.stroke();

            // Draw area under line
            ctx.fillStyle = this.color + '20';  // Add transparency
            ctx.lineTo(left + chartWidth, top + chartHeight);
            ctx.lineTo(left, top + chartHeight);
            ctx.closePath();
            ctx.fill();
        }

        drawMultiSeries(width, height, top, right, bottom, left) {
            const ctx = this.ctx;
            const chartWidth = width - left - right;
            const chartHeight = height - top - bottom;

            const allValues = this.multiSeriesData.flatMap(d => d.value);
            const min = Math.min(...allValues);
            const max = Math.max(...allValues);
            const range = max - min || 1;

            this.multiSeriesData.forEach(series => {
                ctx.strokeStyle = series.color || CHART_COLORS.primary;
                ctx.lineWidth = 2;
                ctx.beginPath();

                const x = left + chartWidth * 0.1;  // Start at 10%
                const y = top + chartHeight - ((series.value - min) / range) * chartHeight;

                ctx.moveTo(x, y);
                ctx.lineTo(left + chartWidth * 0.9, y);
                ctx.stroke();

                // Draw label
                ctx.fillStyle = series.color || CHART_COLORS.primary;
                ctx.font = '12px sans-serif';
                ctx.fillText(series.label, left + chartWidth * 0.92, y + 4);
            });
        }

        drawAxes(width, height, top, right, bottom, left) {
            const ctx = this.ctx;

            ctx.strokeStyle = '#9ca3af';
            ctx.lineWidth = 1;
            ctx.font = '11px sans-serif';
            ctx.fillStyle = '#6b7280';

            // Y-axis
            ctx.beginPath();
            ctx.moveTo(left, top);
            ctx.lineTo(left, height - bottom);
            ctx.stroke();

            // X-axis
            ctx.beginPath();
            ctx.moveTo(left, height - bottom);
            ctx.lineTo(width - right, height - bottom);
            ctx.stroke();
        }
    }

    // Initialize when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }

    // Expose to global scope for navigation
    window.MetricsPage = { init, stopAutoRefresh };
})();

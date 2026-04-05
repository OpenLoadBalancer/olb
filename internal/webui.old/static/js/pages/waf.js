// WAF Dashboard page for OpenLoadBalancer Web UI
// Displays WAF status, statistics, and layer state.
// All data is from the trusted admin API — no user-generated content is rendered.

class WAFPage {
    constructor() {
        this.status = null;
        this.refreshInterval = null;
    }

    init() {
        this.loadStatus();
        this.refreshInterval = setInterval(() => this.loadStatus(), 5000);
    }

    destroy() {
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
        }
    }

    async loadStatus() {
        try {
            const resp = await fetch('/api/v1/waf/status');
            const data = await resp.json();
            if (data.success) {
                this.status = data.data;
                this.render();
            }
        } catch (e) {
            this.renderError();
        }
    }

    render() {
        const el = document.getElementById('page-content');
        if (!el || !this.status) return;

        const s = this.status;
        const stats = s.stats || {};
        const layers = s.layers || {};
        const total = stats.total_requests || 0;
        const blocked = stats.blocked_requests || 0;
        const monitored = stats.monitored_requests || 0;
        const blockRate = total > 0 ? ((blocked / total) * 100).toFixed(1) : '0.0';

        // Build DOM safely
        el.textContent = '';

        // Header
        const header = document.createElement('div');
        header.className = 'waf-header';
        const h2 = document.createElement('h2');
        h2.textContent = 'Web Application Firewall';
        header.appendChild(h2);
        const badge1 = document.createElement('span');
        badge1.className = 'waf-badge ' + (s.enabled ? 'waf-badge-on' : 'waf-badge-off');
        badge1.textContent = s.enabled ? 'ENABLED' : 'DISABLED';
        header.appendChild(badge1);
        const badge2 = document.createElement('span');
        badge2.className = 'waf-badge waf-badge-mode';
        badge2.textContent = (s.mode || 'unknown').toUpperCase();
        header.appendChild(badge2);
        el.appendChild(header);

        // Stats
        const statsGrid = document.createElement('div');
        statsGrid.className = 'waf-stats';
        statsGrid.appendChild(this.createStat(this.fmtNum(total), 'Total Requests', ''));
        statsGrid.appendChild(this.createStat(this.fmtNum(blocked), 'Blocked', 'waf-red'));
        statsGrid.appendChild(this.createStat(this.fmtNum(monitored), 'Monitored', 'waf-yellow'));
        statsGrid.appendChild(this.createStat(blockRate + '%', 'Block Rate', ''));
        el.appendChild(statsGrid);

        // Layers
        const layerSection = document.createElement('div');
        const layerTitle = document.createElement('h3');
        layerTitle.textContent = 'Security Layers';
        layerSection.appendChild(layerTitle);
        const layerGrid = document.createElement('div');
        layerGrid.className = 'waf-layers';
        const layerDefs = [
            ['IP Access Control', layers.ip_acl, 'Whitelist/blacklist by IP/CIDR'],
            ['Rate Limiter', layers.rate_limit, 'Token bucket per-IP/path'],
            ['Request Sanitizer', layers.sanitizer, 'Validation + normalization'],
            ['Detection Engine', layers.detection, 'SQLi, XSS, CMDi, XXE, SSRF'],
            ['Bot Detection', layers.bot_detect, 'JA3 fingerprint, UA analysis'],
            ['Response Protection', layers.response, 'Headers, masking, error pages']
        ];
        for (const [name, active, desc] of layerDefs) {
            layerGrid.appendChild(this.createLayerCard(name, active, desc));
        }
        layerSection.appendChild(layerGrid);
        el.appendChild(layerSection);

        // Detector hits
        const hits = stats.detector_hits;
        if (hits && Object.keys(hits).length > 0) {
            const hitsSection = document.createElement('div');
            const hitsTitle = document.createElement('h3');
            hitsTitle.textContent = 'Detector Hits';
            hitsSection.appendChild(hitsTitle);
            const sorted = Object.entries(hits).sort((a, b) => b[1] - a[1]);
            for (const [name, count] of sorted) {
                const row = document.createElement('div');
                row.className = 'waf-hit-row';
                const nameSpan = document.createElement('span');
                nameSpan.textContent = name;
                const countSpan = document.createElement('span');
                countSpan.className = 'waf-hit-count';
                countSpan.textContent = this.fmtNum(count);
                row.appendChild(nameSpan);
                row.appendChild(countSpan);
                hitsSection.appendChild(row);
            }
            el.appendChild(hitsSection);
        }

        // Inject styles (once)
        if (!document.getElementById('waf-styles')) {
            const style = document.createElement('style');
            style.id = 'waf-styles';
            style.textContent = `
                .waf-header{display:flex;align-items:center;gap:12px;margin-bottom:24px}
                .waf-header h2{margin:0;font-size:1.5rem}
                .waf-badge{padding:4px 10px;border-radius:4px;font-size:.75rem;font-weight:600}
                .waf-badge-on{background:#166534;color:#4ade80}
                .waf-badge-off{background:#374151;color:#9ca3af}
                .waf-badge-mode{background:#1e3a5f;color:#60a5fa}
                .waf-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:16px;margin-bottom:24px}
                .waf-stat{background:#1a1a2e;border:1px solid #2a2a3e;border-radius:8px;padding:20px;text-align:center}
                .waf-stat-val{font-size:2rem;font-weight:700;color:#e5e5e5}
                .waf-red{color:#ef4444}
                .waf-yellow{color:#f59e0b}
                .waf-stat-lbl{color:#737373;font-size:.85rem;margin-top:4px}
                .waf-layers{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-top:12px}
                .waf-layer{background:#1a1a2e;border:1px solid #2a2a3e;border-radius:8px;padding:16px}
                .waf-layer.on{border-color:#166534}
                .waf-layer.off{opacity:.5}
                .waf-dot{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:8px}
                .waf-dot.on{background:#4ade80}
                .waf-dot.off{background:#6b7280}
                .waf-layer-name{font-weight:600;font-size:.9rem;margin-bottom:4px}
                .waf-layer-desc{color:#737373;font-size:.8rem}
                .waf-hit-row{display:flex;justify-content:space-between;padding:8px 12px;border-bottom:1px solid #2a2a3e}
                .waf-hit-count{color:#60a5fa;font-weight:600}
                h3{margin:16px 0 8px;font-size:1.1rem;color:#e5e5e5}
                @media(max-width:768px){.waf-stats{grid-template-columns:repeat(2,1fr)}.waf-layers{grid-template-columns:1fr}}
            `;
            document.head.appendChild(style);
        }
    }

    createStat(value, label, cls) {
        const card = document.createElement('div');
        card.className = 'waf-stat';
        const val = document.createElement('div');
        val.className = 'waf-stat-val' + (cls ? ' ' + cls : '');
        val.textContent = value;
        const lbl = document.createElement('div');
        lbl.className = 'waf-stat-lbl';
        lbl.textContent = label;
        card.appendChild(val);
        card.appendChild(lbl);
        return card;
    }

    createLayerCard(name, active, desc) {
        const card = document.createElement('div');
        card.className = 'waf-layer ' + (active ? 'on' : 'off');
        const nameDiv = document.createElement('div');
        nameDiv.className = 'waf-layer-name';
        const dot = document.createElement('span');
        dot.className = 'waf-dot ' + (active ? 'on' : 'off');
        nameDiv.appendChild(dot);
        nameDiv.appendChild(document.createTextNode(name));
        const descDiv = document.createElement('div');
        descDiv.className = 'waf-layer-desc';
        descDiv.textContent = desc;
        card.appendChild(nameDiv);
        card.appendChild(descDiv);
        return card;
    }

    renderError() {
        const el = document.getElementById('page-content');
        if (!el) return;
        el.textContent = '';
        const p = document.createElement('p');
        p.style.color = '#737373';
        p.textContent = 'WAF status endpoint not available. Ensure WAF is enabled in configuration.';
        el.appendChild(p);
    }

    fmtNum(n) {
        if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
        if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
        return String(n);
    }
}

if (typeof window !== 'undefined') {
    window.wafPage = new WAFPage();
}

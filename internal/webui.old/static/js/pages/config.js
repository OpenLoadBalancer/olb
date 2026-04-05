// Config page for OpenLoadBalancer Web UI
// Provides config viewing, diff comparison, and reload functionality

class ConfigPage {
    constructor() {
        this.currentConfig = null;
        this.fileConfig = null;
        this.validationErrors = [];
        this.currentView = 'yaml'; // 'yaml', 'diff', 'tree'
        this.collapsedSections = new Set();
    }

    // Initialize the config page
    init() {
        this.bindEvents();
        this.loadConfig();
    }

    // Bind event handlers
    bindEvents() {
        // View toggle buttons
        const yamlBtn = document.getElementById('view-yaml');
        const diffBtn = document.getElementById('view-diff');
        const treeBtn = document.getElementById('view-tree');

        if (yamlBtn) yamlBtn.addEventListener('click', () => this.switchView('yaml'));
        if (diffBtn) diffBtn.addEventListener('click', () => this.switchView('diff'));
        if (treeBtn) treeBtn.addEventListener('click', () => this.switchView('tree'));

        // Action buttons
        const reloadBtn = document.getElementById('reload-config');
        const validateBtn = document.getElementById('validate-config');
        const downloadBtn = document.getElementById('download-config');

        if (reloadBtn) reloadBtn.addEventListener('click', () => this.showReloadModal());
        if (validateBtn) validateBtn.addEventListener('click', () => this.validateConfig());
        if (downloadBtn) downloadBtn.addEventListener('click', () => this.downloadConfig());

        // Modal buttons
        const confirmReload = document.getElementById('confirm-reload');
        const cancelReload = document.getElementById('cancel-reload');
        const closeModal = document.getElementById('close-modal');

        if (confirmReload) confirmReload.addEventListener('click', () => this.reloadConfig());
        if (cancelReload) cancelReload.addEventListener('click', () => this.hideReloadModal());
        if (closeModal) closeModal.addEventListener('click', () => this.hideReloadModal());

        // Collapsible sections
        const configDisplay = document.getElementById('config-display');
        if (configDisplay) {
            configDisplay.addEventListener('click', (e) => {
                if (e.target.classList.contains('collapse-toggle')) {
                    this.toggleSection(e.target.dataset.section);
                }
            });
        }
    }

    // Load configuration from API
    async loadConfig() {
        try {
            this.showLoading(true);

            // Load running config
            const runningResponse = await fetch('/api/v1/config/running');
            if (runningResponse.ok) {
                const runningData = await runningResponse.json();
                if (runningData.success) {
                    this.currentConfig = runningData.data;
                }
            }

            // Load file config
            const fileResponse = await fetch('/api/v1/config/file');
            if (fileResponse.ok) {
                const fileData = await fileResponse.json();
                if (fileData.success) {
                    this.fileConfig = fileData.data;
                }
            }

            // Load validation status
            await this.loadValidationStatus();

            this.render();
        } catch (error) {
            this.showError('Failed to load configuration: ' + error.message);
        } finally {
            this.showLoading(false);
        }
    }

    // Load configuration validation status
    async loadValidationStatus() {
        try {
            const response = await fetch('/api/v1/config/validate');
            if (response.ok) {
                const data = await response.json();
                if (data.success) {
                    this.validationErrors = data.data.errors || [];
                }
            }
        } catch (error) {
            console.error('Failed to load validation status:', error);
        }
    }

    // Switch between views
    switchView(view) {
        this.currentView = view;

        // Update active button state
        document.querySelectorAll('.view-btn').forEach(btn => {
            btn.classList.remove('active');
        });
        const viewBtn = document.getElementById(`view-${view}`);
        if (viewBtn) viewBtn.classList.add('active');

        this.render();
    }

    // Render the current view
    render() {
        const display = document.getElementById('config-display');
        if (!display) return;

        // Clear display safely
        while (display.firstChild) {
            display.removeChild(display.firstChild);
        }

        switch (this.currentView) {
            case 'yaml':
                this.renderYAMLView(display);
                break;
            case 'diff':
                this.renderDiffView(display);
                break;
            case 'tree':
                this.renderTreeView(display);
                break;
        }

        // Update validation indicator
        this.updateValidationIndicator();
    }

    // Render YAML view with syntax highlighting
    renderYAMLView(container) {
        if (!this.currentConfig) {
            const emptyState = document.createElement('div');
            emptyState.className = 'empty-state';
            emptyState.textContent = 'No configuration loaded';
            container.appendChild(emptyState);
            return;
        }

        const yaml = this.objectToYAML(this.currentConfig);
        const lines = yaml.split('\n');

        const yamlView = document.createElement('div');
        yamlView.className = 'yaml-view';

        lines.forEach((line, index) => {
            const lineNum = index + 1;
            const indent = line.match(/^(\s*)/)[1].length;
            const sectionId = this.getSectionId(line, index);
            const isCollapsible = this.isCollapsibleLine(line);

            const lineDiv = document.createElement('div');
            lineDiv.className = 'yaml-line';
            lineDiv.dataset.line = lineNum;
            lineDiv.style.paddingLeft = `${indent * 2}ch`;

            const lineNumber = document.createElement('span');
            lineNumber.className = 'line-number';
            lineNumber.textContent = lineNum;
            lineDiv.appendChild(lineNumber);

            if (isCollapsible && sectionId) {
                const toggle = document.createElement('span');
                toggle.className = 'collapse-toggle';
                if (this.collapsedSections.has(sectionId)) {
                    toggle.classList.add('collapsed');
                }
                toggle.dataset.section = sectionId;
                lineDiv.appendChild(toggle);
            } else {
                const spacer = document.createElement('span');
                spacer.className = 'collapse-spacer';
                lineDiv.appendChild(spacer);
            }

            const content = document.createElement('span');
            content.className = 'line-content';
            this.populateYAMLLine(content, line);
            lineDiv.appendChild(content);

            yamlView.appendChild(lineDiv);

            // Add collapsible section container
            if (isCollapsible && sectionId) {
                const sectionDiv = document.createElement('div');
                sectionDiv.className = 'section-content';
                if (this.collapsedSections.has(sectionId)) {
                    sectionDiv.classList.add('collapsed');
                }
                sectionDiv.dataset.section = sectionId;
                yamlView.appendChild(sectionDiv);
            }
        });

        container.appendChild(yamlView);
    }

    // Populate YAML line with syntax highlighting
    populateYAMLLine(element, line) {
        // Comments
        if (line.trim().startsWith('#')) {
            const comment = document.createElement('span');
            comment.className = 'yaml-comment';
            comment.textContent = line;
            element.appendChild(comment);
            return;
        }

        // Array items
        if (line.trim().startsWith('- ')) {
            const match = line.match(/^(\s*)-\s*(.*)$/);
            if (match) {
                const [, indent, rest] = match;
                if (indent) element.appendChild(document.createTextNode(indent));

                const dash = document.createElement('span');
                dash.className = 'yaml-dash';
                dash.textContent = '-';
                element.appendChild(dash);

                element.appendChild(document.createTextNode(' '));
                this.populateYAMLValue(element, rest);
                return;
            }
        }

        // Key-value pairs
        const kvMatch = line.match(/^(\s*)([^:]+):\s*(.*)$/);
        if (kvMatch) {
            const [, indent, key, value] = kvMatch;
            if (indent) element.appendChild(document.createTextNode(indent));

            const keySpan = document.createElement('span');
            keySpan.className = 'yaml-key';
            keySpan.textContent = key;
            element.appendChild(keySpan);

            element.appendChild(document.createTextNode(':'));

            if (value) {
                element.appendChild(document.createTextNode(' '));
                this.populateYAMLValue(element, value);
            }
            return;
        }

        element.textContent = line;
    }

    // Populate YAML value with syntax highlighting
    populateYAMLValue(element, value) {
        const trimmed = value.trim();

        // Strings
        if (trimmed.startsWith('"') && trimmed.endsWith('"')) {
            const str = document.createElement('span');
            str.className = 'yaml-string';
            str.textContent = value;
            element.appendChild(str);
            return;
        }

        // Numbers
        if (/^-?\d+(\.\d+)?$/.test(trimmed)) {
            const num = document.createElement('span');
            num.className = 'yaml-number';
            num.textContent = value;
            element.appendChild(num);
            return;
        }

        // Booleans
        if (/^(true|false|yes|no|on|off)$/.test(trimmed.toLowerCase())) {
            const bool = document.createElement('span');
            bool.className = 'yaml-boolean';
            bool.textContent = value;
            element.appendChild(bool);
            return;
        }

        // Null
        if (/^(null|~)$/.test(trimmed.toLowerCase())) {
            const nullSpan = document.createElement('span');
            nullSpan.className = 'yaml-null';
            nullSpan.textContent = value;
            element.appendChild(nullSpan);
            return;
        }

        element.textContent = value;
    }

    // Render diff view comparing running vs file config
    renderDiffView(container) {
        if (!this.currentConfig || !this.fileConfig) {
            const emptyState = document.createElement('div');
            emptyState.className = 'empty-state';
            emptyState.textContent = 'Both running and file configurations must be loaded to compare';
            container.appendChild(emptyState);
            return;
        }

        const runningYAML = this.objectToYAML(this.currentConfig);
        const fileYAML = this.objectToYAML(this.fileConfig);
        const diff = this.computeDiff(fileYAML, runningYAML);

        const diffView = document.createElement('div');
        diffView.className = 'diff-view';

        const header = document.createElement('div');
        header.className = 'diff-header';

        const leftLabel = document.createElement('div');
        leftLabel.className = 'diff-label';
        leftLabel.textContent = 'File Config';
        header.appendChild(leftLabel);

        const rightLabel = document.createElement('div');
        rightLabel.className = 'diff-label';
        rightLabel.textContent = 'Running Config';
        header.appendChild(rightLabel);

        diffView.appendChild(header);

        const content = document.createElement('div');
        content.className = 'diff-content';

        diff.forEach(part => {
            const lines = part.value.split('\n').filter(l => l !== '');
            lines.forEach(line => {
                const cssClass = part.added ? 'diff-added' : part.removed ? 'diff-removed' : 'diff-unchanged';
                const side = part.added ? 'right' : part.removed ? 'left' : 'both';

                if (side === 'left' || side === 'both') {
                    const leftLine = document.createElement('div');
                    leftLine.className = `diff-line ${cssClass} left`;
                    leftLine.textContent = line;
                    content.appendChild(leftLine);
                }
                if (side === 'right' || side === 'both') {
                    const rightLine = document.createElement('div');
                    rightLine.className = `diff-line ${cssClass} right`;
                    rightLine.textContent = line;
                    content.appendChild(rightLine);
                }
            });
        });

        diffView.appendChild(content);
        container.appendChild(diffView);
    }

    // Render tree view
    renderTreeView(container) {
        if (!this.currentConfig) {
            const emptyState = document.createElement('div');
            emptyState.className = 'empty-state';
            emptyState.textContent = 'No configuration loaded';
            container.appendChild(emptyState);
            return;
        }

        const treeView = document.createElement('div');
        treeView.className = 'tree-view';
        this.populateTreeNode(treeView, this.currentConfig, 'root', 0);
        container.appendChild(treeView);
    }

    // Populate a tree node recursively
    populateTreeNode(container, obj, key, depth) {
        const indent = depth * 20;

        if (obj === null) {
            const node = document.createElement('div');
            node.className = 'tree-node';
            node.style.paddingLeft = `${indent}px`;

            const keySpan = document.createElement('span');
            keySpan.className = 'tree-key';
            keySpan.textContent = `${key}:`;
            node.appendChild(keySpan);

            const nullSpan = document.createElement('span');
            nullSpan.className = 'tree-null';
            nullSpan.textContent = ' null';
            node.appendChild(nullSpan);

            container.appendChild(node);
            return;
        }

        if (typeof obj !== 'object') {
            const node = document.createElement('div');
            node.className = 'tree-node';
            node.style.paddingLeft = `${indent}px`;

            const keySpan = document.createElement('span');
            keySpan.className = 'tree-key';
            keySpan.textContent = `${key}:`;
            node.appendChild(keySpan);

            const typeClass = typeof obj === 'string' ? 'tree-string' : typeof obj === 'number' ? 'tree-number' : 'tree-boolean';
            const displayValue = typeof obj === 'string' ? ` "${obj}"` : ` ${String(obj)}`;

            const valueSpan = document.createElement('span');
            valueSpan.className = typeClass;
            valueSpan.textContent = displayValue;
            node.appendChild(valueSpan);

            container.appendChild(node);
            return;
        }

        if (Array.isArray(obj)) {
            const node = document.createElement('div');
            node.className = 'tree-node tree-array';
            node.style.paddingLeft = `${indent}px`;

            const keySpan = document.createElement('span');
            keySpan.className = 'tree-key';
            keySpan.textContent = `${key}:`;
            node.appendChild(keySpan);

            const bracket = document.createElement('span');
            bracket.className = 'tree-bracket';
            bracket.textContent = ` [${obj.length} items]`;
            node.appendChild(bracket);

            const children = document.createElement('div');
            children.className = 'tree-children';
            obj.forEach((item, i) => {
                this.populateTreeNode(children, item, `[${i}]`, depth + 1);
            });
            node.appendChild(children);

            container.appendChild(node);
        } else {
            const keys = Object.keys(obj);
            const node = document.createElement('div');
            node.className = 'tree-node tree-object';
            node.style.paddingLeft = `${indent}px`;

            const keySpan = document.createElement('span');
            keySpan.className = 'tree-key';
            keySpan.textContent = `${key}:`;
            node.appendChild(keySpan);

            const bracket = document.createElement('span');
            bracket.className = 'tree-bracket';
            bracket.textContent = ` {${keys.length} keys}`;
            node.appendChild(bracket);

            const children = document.createElement('div');
            children.className = 'tree-children';
            keys.forEach(k => {
                this.populateTreeNode(children, obj[k], k, depth + 1);
            });
            node.appendChild(children);

            container.appendChild(node);
        }
    }

    // Simple diff algorithm
    computeDiff(oldStr, newStr) {
        const oldLines = oldStr.split('\n');
        const newLines = newStr.split('\n');
        const result = [];

        let i = 0, j = 0;
        while (i < oldLines.length || j < newLines.length) {
            if (i >= oldLines.length) {
                result.push({ added: true, removed: false, value: newLines[j] + '\n' });
                j++;
            } else if (j >= newLines.length) {
                result.push({ added: false, removed: true, value: oldLines[i] + '\n' });
                i++;
            } else if (oldLines[i] === newLines[j]) {
                result.push({ added: false, removed: false, value: oldLines[i] + '\n' });
                i++;
                j++;
            } else {
                // Simple approach: mark as removed then added
                result.push({ added: false, removed: true, value: oldLines[i] + '\n' });
                result.push({ added: true, removed: false, value: newLines[j] + '\n' });
                i++;
                j++;
            }
        }

        return result;
    }

    // Convert object to YAML string
    objectToYAML(obj, indent = 0) {
        let yaml = '';
        const spaces = '  '.repeat(indent);

        if (obj === null) {
            return 'null';
        }

        if (typeof obj !== 'object') {
            if (typeof obj === 'string') {
                // Quote strings that need it
                if (obj.includes(':') || obj.includes('#') || obj.includes('\n') || obj.startsWith(' ') || obj === '') {
                    return `"${obj.replace(/"/g, '\\"')}"`;
                }
                return obj;
            }
            return String(obj);
        }

        if (Array.isArray(obj)) {
            if (obj.length === 0) {
                return '[]';
            }
            obj.forEach(item => {
                if (typeof item === 'object' && item !== null) {
                    yaml += `${spaces}- `;
                    const itemYaml = this.objectToYAML(item, indent + 1);
                    yaml += itemYaml.trimStart();
                } else {
                    yaml += `${spaces}- ${this.objectToYAML(item, 0)}\n`;
                }
            });
        } else {
            const keys = Object.keys(obj);
            if (keys.length === 0) {
                return '{}';
            }
            keys.forEach(key => {
                const value = obj[key];
                if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
                    yaml += `${spaces}${key}:\n`;
                    yaml += this.objectToYAML(value, indent + 1);
                } else if (Array.isArray(value)) {
                    yaml += `${spaces}${key}:\n`;
                    yaml += this.objectToYAML(value, indent + 1);
                } else {
                    yaml += `${spaces}${key}: ${this.objectToYAML(value, 0)}\n`;
                }
            });
        }

        return yaml;
    }

    // Check if a line can be collapsed
    isCollapsibleLine(line) {
        const trimmed = line.trim();
        // Lines ending with : that have children
        return trimmed.endsWith(':') && !trimmed.startsWith('#');
    }

    // Get section ID for collapsible sections
    getSectionId(line, index) {
        const match = line.match(/^\s*([^:]+):/);
        if (match) {
            return `section-${index}-${match[1]}`;
        }
        return null;
    }

    // Toggle section collapse state
    toggleSection(sectionId) {
        if (this.collapsedSections.has(sectionId)) {
            this.collapsedSections.delete(sectionId);
        } else {
            this.collapsedSections.add(sectionId);
        }
        this.render();
    }

    // Update validation indicator
    updateValidationIndicator() {
        const indicator = document.getElementById('validation-indicator');
        if (!indicator) return;

        // Clear existing content
        while (indicator.firstChild) {
            indicator.removeChild(indicator.firstChild);
        }

        if (this.validationErrors.length === 0) {
            indicator.className = 'validation-status valid';
            const icon = document.createElement('span');
            icon.className = 'status-icon';
            icon.textContent = '✓';
            indicator.appendChild(icon);
            indicator.appendChild(document.createTextNode(' Configuration is valid'));
        } else {
            indicator.className = 'validation-status invalid';
            const icon = document.createElement('span');
            icon.className = 'status-icon';
            icon.textContent = '✗';
            indicator.appendChild(icon);
            indicator.appendChild(document.createTextNode(` ${this.validationErrors.length} validation error(s)`));
        }

        // Show validation details
        const details = document.getElementById('validation-details');
        if (details && this.validationErrors.length > 0) {
            while (details.firstChild) {
                details.removeChild(details.firstChild);
            }

            this.validationErrors.forEach(err => {
                const errorDiv = document.createElement('div');
                errorDiv.className = 'validation-error';

                const fieldSpan = document.createElement('span');
                fieldSpan.className = 'error-field';
                fieldSpan.textContent = `${err.field || 'Config'}:`;
                errorDiv.appendChild(fieldSpan);

                const msgSpan = document.createElement('span');
                msgSpan.className = 'error-message';
                msgSpan.textContent = ` ${err.message}`;
                errorDiv.appendChild(msgSpan);

                details.appendChild(errorDiv);
            });
        }
    }

    // Show reload confirmation modal
    showReloadModal() {
        const modal = document.getElementById('reload-modal');
        if (modal) {
            modal.classList.add('visible');
        }
    }

    // Hide reload modal
    hideReloadModal() {
        const modal = document.getElementById('reload-modal');
        if (modal) {
            modal.classList.remove('visible');
        }
    }

    // Reload configuration
    async reloadConfig() {
        try {
            this.hideReloadModal();
            this.showLoading(true);

            const response = await fetch('/api/v1/system/reload', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' }
            });

            const data = await response.json();

            if (data.success) {
                this.showNotification('Configuration reloaded successfully', 'success');
                // Reload the page to show updated config
                await this.loadConfig();
            } else {
                this.showNotification('Reload failed: ' + (data.error?.message || 'Unknown error'), 'error');
            }
        } catch (error) {
            this.showNotification('Reload failed: ' + error.message, 'error');
        } finally {
            this.showLoading(false);
        }
    }

    // Validate configuration
    async validateConfig() {
        try {
            this.showLoading(true);

            const response = await fetch('/api/v1/config/validate', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ config: this.currentConfig })
            });

            const data = await response.json();

            if (data.success) {
                this.validationErrors = data.data.errors || [];
                this.updateValidationIndicator();

                if (this.validationErrors.length === 0) {
                    this.showNotification('Configuration is valid', 'success');
                } else {
                    this.showNotification(`Found ${this.validationErrors.length} validation error(s)`, 'warning');
                }
            }
        } catch (error) {
            this.showNotification('Validation failed: ' + error.message, 'error');
        } finally {
            this.showLoading(false);
        }
    }

    // Download configuration as YAML file
    downloadConfig() {
        if (!this.currentConfig) return;

        const yaml = this.objectToYAML(this.currentConfig);
        const blob = new Blob([yaml], { type: 'text/yaml' });
        const url = URL.createObjectURL(blob);

        const a = document.createElement('a');
        a.href = url;
        a.download = `olb-config-${new Date().toISOString().split('T')[0]}.yaml`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);

        URL.revokeObjectURL(url);
    }

    // Show/hide loading indicator
    showLoading(show) {
        const loader = document.getElementById('config-loading');
        if (loader) {
            loader.style.display = show ? 'flex' : 'none';
        }
    }

    // Show error message
    showError(message) {
        const display = document.getElementById('config-display');
        if (display) {
            while (display.firstChild) {
                display.removeChild(display.firstChild);
            }
            const errorDiv = document.createElement('div');
            errorDiv.className = 'error-state';
            errorDiv.textContent = message;
            display.appendChild(errorDiv);
        }
    }

    // Show notification
    showNotification(message, type = 'info') {
        const container = document.getElementById('notification-container') || document.body;
        const notification = document.createElement('div');
        notification.className = `notification ${type}`;
        notification.textContent = message;

        container.appendChild(notification);

        setTimeout(() => {
            notification.classList.add('fade-out');
            setTimeout(() => notification.remove(), 300);
        }, 3000);
    }
}

// Initialize config page when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        window.configPage = new ConfigPage();
        window.configPage.init();
    });
} else {
    window.configPage = new ConfigPage();
    window.configPage.init();
}

export default ConfigPage;

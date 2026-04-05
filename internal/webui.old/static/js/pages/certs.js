// Certificates page for OpenLoadBalancer Web UI
// Provides certificate inventory, expiry monitoring, and management

class CertificatesPage {
    constructor() {
        this.certificates = [];
        this.acmeStatus = {};
        this.selectedCert = null;
        this.filter = 'all'; // 'all', 'valid', 'expiring', 'expired'
        this.sortBy = 'expiry'; // 'expiry', 'domain', 'issuer'
        this.sortDesc = false;
    }

    // Initialize the certificates page
    init() {
        this.bindEvents();
        this.loadCertificates();
        this.loadACMEStatus();
    }

    // Bind event handlers
    bindEvents() {
        // Filter buttons
        document.querySelectorAll('.filter-btn').forEach(btn => {
            btn.addEventListener('click', (e) => {
                this.setFilter(e.target.dataset.filter);
            });
        });

        // Sort headers
        document.querySelectorAll('.sortable').forEach(header => {
            header.addEventListener('click', (e) => {
                const sortKey = e.target.dataset.sort;
                if (sortKey) {
                    this.setSort(sortKey);
                }
            });
        });

        // Action buttons
        document.getElementById('add-cert')?.addEventListener('click', () => this.showAddModal());
        document.getElementById('refresh-certs')?.addEventListener('click', () => this.refreshCertificates());

        // Modal buttons
        document.getElementById('close-add-modal')?.addEventListener('click', () => this.hideAddModal());
        document.getElementById('cancel-add')?.addEventListener('click', () => this.hideAddModal());
        document.getElementById('save-cert')?.addEventListener('click', () => this.saveCertificate());
        document.getElementById('close-details-modal')?.addEventListener('click', () => this.hideDetailsModal());

        // Certificate source toggle
        document.querySelectorAll('input[name="cert-source"]').forEach(radio => {
            radio.addEventListener('change', (e) => this.toggleCertSource(e.target.value));
        });

        // File upload handling
        document.getElementById('cert-file')?.addEventListener('change', (e) => this.handleFileSelect(e, 'cert'));
        document.getElementById('key-file')?.addEventListener('change', (e) => this.handleFileSelect(e, 'key'));

        // Drag and drop
        const dropZone = document.getElementById('drop-zone');
        if (dropZone) {
            dropZone.addEventListener('dragover', (e) => {
                e.preventDefault();
                dropZone.classList.add('drag-over');
            });
            dropZone.addEventListener('dragleave', () => {
                dropZone.classList.remove('drag-over');
            });
            dropZone.addEventListener('drop', (e) => {
                e.preventDefault();
                dropZone.classList.remove('drag-over');
                this.handleDrop(e);
            });
        }
    }

    // Load certificates from API
    async loadCertificates() {
        try {
            this.showLoading(true);

            const response = await fetch('/api/v1/certificates');
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}`);
            }

            const data = await response.json();
            if (data.success) {
                this.certificates = data.data || [];
                this.render();
            } else {
                this.showError('Failed to load certificates: ' + (data.error?.message || 'Unknown error'));
            }
        } catch (error) {
            this.showError('Failed to load certificates: ' + error.message);
        } finally {
            this.showLoading(false);
        }
    }

    // Load ACME status from API
    async loadACMEStatus() {
        try {
            const response = await fetch('/api/v1/certificates/acme/status');
            if (response.ok) {
                const data = await response.json();
                if (data.success) {
                    this.acmeStatus = data.data || {};
                }
            }
        } catch (error) {
            console.error('Failed to load ACME status:', error);
        }
    }

    // Refresh certificates list
    async refreshCertificates() {
        await this.loadCertificates();
        await this.loadACMEStatus();
        this.showNotification('Certificates refreshed', 'success');
    }

    // Set filter
    setFilter(filter) {
        this.filter = filter;

        // Update active button
        document.querySelectorAll('.filter-btn').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.filter === filter);
        });

        this.render();
    }

    // Set sort
    setSort(sortBy) {
        if (this.sortBy === sortBy) {
            this.sortDesc = !this.sortDesc;
        } else {
            this.sortBy = sortBy;
            this.sortDesc = false;
        }
        this.render();
    }

    // Get filtered and sorted certificates
    getFilteredCertificates() {
        let filtered = [...this.certificates];

        // Apply filter
        if (this.filter !== 'all') {
            const now = Date.now() / 1000;
            filtered = filtered.filter(cert => {
                const daysUntilExpiry = (cert.expires_at - now) / 86400;
                switch (this.filter) {
                    case 'valid':
                        return daysUntilExpiry > 30;
                    case 'expiring':
                        return daysUntilExpiry > 0 && daysUntilExpiry <= 30;
                    case 'expired':
                        return daysUntilExpiry <= 0;
                    default:
                        return true;
                }
            });
        }

        // Apply sort
        filtered.sort((a, b) => {
            let comparison = 0;
            switch (this.sortBy) {
                case 'expiry':
                    comparison = a.expires_at - b.expires_at;
                    break;
                case 'domain':
                    comparison = (a.domains[0] || '').localeCompare(b.domains[0] || '');
                    break;
                case 'issuer':
                    comparison = (a.issuer || '').localeCompare(b.issuer || '');
                    break;
            }
            return this.sortDesc ? -comparison : comparison;
        });

        return filtered;
    }

    // Render the certificates table
    render() {
        const tbody = document.getElementById('certs-table-body');
        if (!tbody) return;

        const filtered = this.getFilteredCertificates();

        // Clear table
        while (tbody.firstChild) {
            tbody.removeChild(tbody.firstChild);
        }

        if (filtered.length === 0) {
            const row = document.createElement('tr');
            const cell = document.createElement('td');
            cell.colSpan = 7;
            cell.className = 'empty-cell';
            cell.textContent = 'No certificates found';
            row.appendChild(cell);
            tbody.appendChild(row);
            return;
        }

        filtered.forEach(cert => {
            const row = this.createCertificateRow(cert);
            tbody.appendChild(row);
        });

        // Update stats
        this.updateStats();
    }

    // Create a table row for a certificate
    createCertificateRow(cert) {
        const row = document.createElement('tr');
        row.className = this.getStatusClass(cert);

        const now = Date.now() / 1000;
        const daysUntilExpiry = Math.floor((cert.expires_at - now) / 86400);

        // Domain cell
        const domainCell = document.createElement('td');
        const primaryDomain = cert.domains?.[0] || 'Unknown';
        const domainText = document.createElement('div');
        domainText.className = 'domain-name';
        domainText.textContent = primaryDomain;
        domainCell.appendChild(domainText);

        if (cert.domains?.length > 1) {
            const moreDomains = document.createElement('div');
            moreDomains.className = 'more-domains';
            moreDomains.textContent = `+${cert.domains.length - 1} more`;
            domainCell.appendChild(moreDomains);
        }
        row.appendChild(domainCell);

        // Issuer cell
        const issuerCell = document.createElement('td');
        issuerCell.textContent = cert.issuer || 'Unknown';
        row.appendChild(issuerCell);

        // Serial cell
        const serialCell = document.createElement('td');
        serialCell.className = 'mono';
        serialCell.textContent = this.formatSerial(cert.serial);
        row.appendChild(serialCell);

        // Valid from cell
        const validFromCell = document.createElement('td');
        validFromCell.textContent = this.formatDate(cert.not_before);
        row.appendChild(validFromCell);

        // Valid to cell
        const validToCell = document.createElement('td');
        validToCell.textContent = this.formatDate(cert.expires_at);
        row.appendChild(validToCell);

        // Days until expiry cell
        const daysCell = document.createElement('td');
        const daysBadge = document.createElement('span');
        daysBadge.className = `days-badge ${this.getDaysClass(daysUntilExpiry)}`;
        if (daysUntilExpiry < 0) {
            daysBadge.textContent = `${Math.abs(daysUntilExpiry)} days ago`;
        } else {
            daysBadge.textContent = `${daysUntilExpiry} days`;
        }
        daysCell.appendChild(daysBadge);
        row.appendChild(daysCell);

        // Status cell
        const statusCell = document.createElement('td');
        const statusBadge = document.createElement('span');
        statusBadge.className = `status-badge ${this.getStatusClass(cert)}`;
        statusBadge.textContent = this.getStatusText(cert);
        statusCell.appendChild(statusBadge);

        // ACME indicator
        if (cert.acme) {
            const acmeIndicator = document.createElement('span');
            acmeIndicator.className = 'acme-indicator';
            acmeIndicator.title = 'ACME/Let\'s Encrypt';
            acmeIndicator.textContent = ' 🔒';
            statusCell.appendChild(acmeIndicator);
        }
        row.appendChild(statusCell);

        // Actions cell
        const actionsCell = document.createElement('td');
        actionsCell.className = 'actions-cell';

        const viewBtn = document.createElement('button');
        viewBtn.className = 'btn-icon';
        viewBtn.title = 'View Details';
        viewBtn.textContent = '👁';
        viewBtn.addEventListener('click', () => this.showDetailsModal(cert));
        actionsCell.appendChild(viewBtn);

        if (cert.acme) {
            const renewBtn = document.createElement('button');
            renewBtn.className = 'btn-icon';
            renewBtn.title = 'Force Renewal';
            renewBtn.textContent = '🔄';
            renewBtn.addEventListener('click', () => this.renewCertificate(cert));
            actionsCell.appendChild(renewBtn);
        }

        const downloadBtn = document.createElement('button');
        downloadBtn.className = 'btn-icon';
        downloadBtn.title = 'Download Chain';
        downloadBtn.textContent = '⬇';
        downloadBtn.addEventListener('click', () => this.downloadCertificate(cert));
        actionsCell.appendChild(downloadBtn);

        row.appendChild(actionsCell);

        return row;
    }

    // Update certificate statistics
    updateStats() {
        const now = Date.now() / 1000;
        const stats = {
            total: this.certificates.length,
            valid: 0,
            expiring: 0,
            expired: 0
        };

        this.certificates.forEach(cert => {
            const daysUntilExpiry = (cert.expires_at - now) / 86400;
            if (daysUntilExpiry <= 0) {
                stats.expired++;
            } else if (daysUntilExpiry <= 30) {
                stats.expiring++;
            } else {
                stats.valid++;
            }
        });

        // Update stat cards
        const totalEl = document.getElementById('stat-total');
        const validEl = document.getElementById('stat-valid');
        const expiringEl = document.getElementById('stat-expiring');
        const expiredEl = document.getElementById('stat-expired');

        if (totalEl) totalEl.textContent = stats.total;
        if (validEl) validEl.textContent = stats.valid;
        if (expiringEl) expiringEl.textContent = stats.expiring;
        if (expiredEl) expiredEl.textContent = stats.expired;
    }

    // Get status class for certificate
    getStatusClass(cert) {
        const now = Date.now() / 1000;
        const daysUntilExpiry = (cert.expires_at - now) / 86400;

        if (daysUntilExpiry <= 0) return 'status-expired';
        if (daysUntilExpiry <= 7) return 'status-critical';
        if (daysUntilExpiry <= 30) return 'status-warning';
        return 'status-valid';
    }

    // Get status text for certificate
    getStatusText(cert) {
        const now = Date.now() / 1000;
        const daysUntilExpiry = (cert.expires_at - now) / 86400;

        if (daysUntilExpiry <= 0) return 'Expired';
        if (daysUntilExpiry <= 7) return 'Critical';
        if (daysUntilExpiry <= 30) return 'Expiring Soon';
        return 'Valid';
    }

    // Get days class for styling
    getDaysClass(days) {
        if (days <= 0) return 'days-expired';
        if (days <= 7) return 'days-critical';
        if (days <= 30) return 'days-warning';
        return 'days-valid';
    }

    // Format serial number
    formatSerial(serial) {
        if (!serial) return 'N/A';
        // Show first 8 and last 8 characters with ellipsis
        if (serial.length > 20) {
            return serial.substring(0, 8) + '...' + serial.substring(serial.length - 8);
        }
        return serial;
    }

    // Format Unix timestamp to date string
    formatDate(timestamp) {
        if (!timestamp) return 'N/A';
        const date = new Date(timestamp * 1000);
        return date.toLocaleDateString('en-US', {
            year: 'numeric',
            month: 'short',
            day: 'numeric'
        });
    }

    // Show add certificate modal
    showAddModal() {
        const modal = document.getElementById('add-cert-modal');
        if (modal) {
            modal.classList.add('visible');
            this.toggleCertSource('upload');
        }
    }

    // Hide add certificate modal
    hideAddModal() {
        const modal = document.getElementById('add-cert-modal');
        if (modal) {
            modal.classList.remove('visible');
            this.resetAddForm();
        }
    }

    // Show certificate details modal
    showDetailsModal(cert) {
        this.selectedCert = cert;
        const modal = document.getElementById('details-modal');
        if (!modal) return;

        // Populate details
        const detailsContent = document.getElementById('cert-details-content');
        if (detailsContent) {
            while (detailsContent.firstChild) {
                detailsContent.removeChild(detailsContent.firstChild);
            }

            // Certificate info sections
            const sections = [
                { title: 'Subject', data: cert.subject },
                { title: 'Issuer', data: cert.issuer },
                { title: 'Serial Number', data: cert.serial },
                { title: 'Valid From', data: this.formatDate(cert.not_before) },
                { title: 'Valid Until', data: this.formatDate(cert.expires_at) },
                { title: 'Signature Algorithm', data: cert.signature_algorithm },
                { title: 'Key Usage', data: cert.key_usage?.join(', ') },
                { title: 'Extended Key Usage', data: cert.ext_key_usage?.join(', ') }
            ];

            sections.forEach(section => {
                if (section.data) {
                    const row = document.createElement('div');
                    row.className = 'detail-row';

                    const label = document.createElement('span');
                    label.className = 'detail-label';
                    label.textContent = section.title + ':';
                    row.appendChild(label);

                    const value = document.createElement('span');
                    value.className = 'detail-value';
                    value.textContent = section.data;
                    row.appendChild(value);

                    detailsContent.appendChild(row);
                }
            });

            // SANs list
            if (cert.domains?.length > 0) {
                const sanSection = document.createElement('div');
                sanSection.className = 'detail-section';

                const sanTitle = document.createElement('h4');
                sanTitle.textContent = 'Subject Alternative Names';
                sanSection.appendChild(sanTitle);

                const sanList = document.createElement('ul');
                sanList.className = 'san-list';
                cert.domains.forEach(domain => {
                    const li = document.createElement('li');
                    li.textContent = domain;
                    sanList.appendChild(li);
                });
                sanSection.appendChild(sanList);
                detailsContent.appendChild(sanSection);
            }

            // Full chain
            if (cert.chain) {
                const chainSection = document.createElement('div');
                chainSection.className = 'detail-section';

                const chainTitle = document.createElement('h4');
                chainTitle.textContent = 'Certificate Chain (PEM)';
                chainSection.appendChild(chainTitle);

                const chainPre = document.createElement('pre');
                chainPre.className = 'cert-chain';
                chainPre.textContent = cert.chain;
                chainSection.appendChild(chainPre);
                detailsContent.appendChild(chainSection);
            }
        }

        modal.classList.add('visible');
    }

    // Hide details modal
    hideDetailsModal() {
        const modal = document.getElementById('details-modal');
        if (modal) {
            modal.classList.remove('visible');
            this.selectedCert = null;
        }
    }

    // Toggle certificate source (upload vs ACME)
    toggleCertSource(source) {
        const uploadSection = document.getElementById('upload-section');
        const acmeSection = document.getElementById('acme-section');

        if (uploadSection) {
            uploadSection.style.display = source === 'upload' ? 'block' : 'none';
        }
        if (acmeSection) {
            acmeSection.style.display = source === 'acme' ? 'block' : 'none';
        }
    }

    // Handle file selection
    handleFileSelect(event, type) {
        const file = event.target.files[0];
        if (file) {
            const reader = new FileReader();
            reader.onload = (e) => {
                if (type === 'cert') {
                    this.selectedCertFile = e.target.result;
                } else {
                    this.selectedKeyFile = e.target.result;
                }
            };
            reader.readAsText(file);
        }
    }

    // Handle drag and drop
    handleDrop(event) {
        const files = event.dataTransfer.files;
        for (let file of files) {
            const reader = new FileReader();
            reader.onload = (e) => {
                const content = e.target.result;
                if (file.name.endsWith('.crt') || file.name.endsWith('.pem') || file.name.endsWith('.cert')) {
                    this.selectedCertFile = content;
                    const certInput = document.getElementById('cert-file');
                    if (certInput) {
                        // Create a new FileList with the dropped file
                        const dt = new DataTransfer();
                        dt.items.add(file);
                        certInput.files = dt.files;
                    }
                } else if (file.name.endsWith('.key')) {
                    this.selectedKeyFile = content;
                    const keyInput = document.getElementById('key-file');
                    if (keyInput) {
                        const dt = new DataTransfer();
                        dt.items.add(file);
                        keyInput.files = dt.files;
                    }
                }
            };
            reader.readAsText(file);
        }
    }

    // Save new certificate
    async saveCertificate() {
        const source = document.querySelector('input[name="cert-source"]:checked')?.value;

        try {
            if (source === 'upload') {
                if (!this.selectedCertFile || !this.selectedKeyFile) {
                    this.showNotification('Please select both certificate and key files', 'error');
                    return;
                }

                const response = await fetch('/api/v1/certificates', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        cert_pem: this.selectedCertFile,
                        key_pem: this.selectedKeyFile
                    })
                });

                const data = await response.json();
                if (data.success) {
                    this.showNotification('Certificate added successfully', 'success');
                    this.hideAddModal();
                    this.loadCertificates();
                } else {
                    this.showNotification('Failed to add certificate: ' + (data.error?.message || 'Unknown error'), 'error');
                }
            } else if (source === 'acme') {
                const domain = document.getElementById('acme-domain')?.value;
                const email = document.getElementById('acme-email')?.value;

                if (!domain || !email) {
                    this.showNotification('Please enter domain and email', 'error');
                    return;
                }

                const response = await fetch('/api/v1/certificates/acme', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ domain, email })
                });

                const data = await response.json();
                if (data.success) {
                    this.showNotification('ACME certificate request initiated', 'success');
                    this.hideAddModal();
                    this.loadCertificates();
                } else {
                    this.showNotification('ACME request failed: ' + (data.error?.message || 'Unknown error'), 'error');
                }
            }
        } catch (error) {
            this.showNotification('Failed to save certificate: ' + error.message, 'error');
        }
    }

    // Renew ACME certificate
    async renewCertificate(cert) {
        if (!cert.acme) {
            this.showNotification('Only ACME certificates can be renewed automatically', 'error');
            return;
        }

        try {
            const response = await fetch(`/api/v1/certificates/${cert.id}/renew`, {
                method: 'POST'
            });

            const data = await response.json();
            if (data.success) {
                this.showNotification('Certificate renewal initiated', 'success');
                setTimeout(() => this.loadCertificates(), 2000);
            } else {
                this.showNotification('Renewal failed: ' + (data.error?.message || 'Unknown error'), 'error');
            }
        } catch (error) {
            this.showNotification('Renewal failed: ' + error.message, 'error');
        }
    }

    // Download certificate chain
    downloadCertificate(cert) {
        if (!cert.chain) {
            this.showNotification('No certificate chain available', 'error');
            return;
        }

        const blob = new Blob([cert.chain], { type: 'application/x-pem-file' });
        const url = URL.createObjectURL(blob);

        const a = document.createElement('a');
        a.href = url;
        a.download = `${cert.domains?.[0] || 'certificate'}.pem`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);

        URL.revokeObjectURL(url);
    }

    // Reset add form
    resetAddForm() {
        this.selectedCertFile = null;
        this.selectedKeyFile = null;

        const certFile = document.getElementById('cert-file');
        const keyFile = document.getElementById('key-file');
        const acmeDomain = document.getElementById('acme-domain');
        const acmeEmail = document.getElementById('acme-email');

        if (certFile) certFile.value = '';
        if (keyFile) keyFile.value = '';
        if (acmeDomain) acmeDomain.value = '';
        if (acmeEmail) acmeEmail.value = '';
    }

    // Show/hide loading indicator
    showLoading(show) {
        const loader = document.getElementById('certs-loading');
        if (loader) {
            loader.style.display = show ? 'flex' : 'none';
        }
    }

    // Show error message
    showError(message) {
        const tbody = document.getElementById('certs-table-body');
        if (tbody) {
            while (tbody.firstChild) {
                tbody.removeChild(tbody.firstChild);
            }
            const row = document.createElement('tr');
            const cell = document.createElement('td');
            cell.colSpan = 7;
            cell.className = 'error-cell';
            cell.textContent = message;
            row.appendChild(cell);
            tbody.appendChild(row);
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

// Initialize certificates page when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        window.certsPage = new CertificatesPage();
        window.certsPage.init();
    });
} else {
    window.certsPage = new CertificatesPage();
    window.certsPage.init();
}

export default CertificatesPage;

// Cluster page for OpenLoadBalancer Web UI
// Provides cluster status, member management, and Raft log info

class ClusterPage {
    constructor() {
        this.clusterStatus = null;
        this.members = [];
        this.sortBy = 'id';
        this.sortDesc = false;
        this.refreshInterval = null;
        this.ws = null;
    }

    // Initialize the cluster page
    init() {
        this.bindEvents();
        this.loadClusterStatus();
        this.loadMembers();
        this.connectWebSocket();
        this.startAutoRefresh();
    }

    // Bind event handlers
    bindEvents() {
        // Action buttons
        var refreshBtn = document.getElementById('refresh-cluster');
        var joinBtn = document.getElementById('join-cluster-btn');
        var leaveBtn = document.getElementById('leave-cluster-btn');

        if (refreshBtn) refreshBtn.addEventListener('click', () => this.refresh());
        if (joinBtn) joinBtn.addEventListener('click', () => this.showJoinModal());
        if (leaveBtn) leaveBtn.addEventListener('click', () => this.showLeaveModal());

        // Join modal buttons
        var closeJoinModal = document.getElementById('close-join-modal');
        var cancelJoin = document.getElementById('cancel-join');
        var confirmJoin = document.getElementById('confirm-join');

        if (closeJoinModal) closeJoinModal.addEventListener('click', () => this.hideJoinModal());
        if (cancelJoin) cancelJoin.addEventListener('click', () => this.hideJoinModal());
        if (confirmJoin) confirmJoin.addEventListener('click', () => this.joinCluster());

        // Leave modal buttons
        var closeLeaveModal = document.getElementById('close-leave-modal');
        var cancelLeave = document.getElementById('cancel-leave');
        var confirmLeave = document.getElementById('confirm-leave');

        if (closeLeaveModal) closeLeaveModal.addEventListener('click', () => this.hideLeaveModal());
        if (cancelLeave) cancelLeave.addEventListener('click', () => this.hideLeaveModal());
        if (confirmLeave) confirmLeave.addEventListener('click', () => this.leaveCluster());

        // Modal overlay clicks
        document.querySelectorAll('.modal .modal-overlay').forEach(overlay => {
            overlay.addEventListener('click', () => {
                this.hideJoinModal();
                this.hideLeaveModal();
            });
        });

        // Sort headers
        document.querySelectorAll('.sortable').forEach(header => {
            header.addEventListener('click', (e) => {
                var sortKey = e.currentTarget.dataset.sort;
                if (sortKey) {
                    this.setSort(sortKey);
                }
            });
        });

        // Enter key on seed address input
        var seedInput = document.getElementById('seed-address');
        if (seedInput) {
            seedInput.addEventListener('keydown', (e) => {
                if (e.key === 'Enter') {
                    e.preventDefault();
                    this.joinCluster();
                }
            });
        }
    }

    // Load cluster status from API
    async loadClusterStatus() {
        try {
            var response = await fetch('/api/v1/cluster/status');
            if (!response.ok) {
                throw new Error('HTTP ' + response.status);
            }

            var data = await response.json();
            if (data.success) {
                this.clusterStatus = data.data || {};
                this.renderStatus();
            } else {
                this.showNotification('Failed to load cluster status: ' + (data.error && data.error.message || 'Unknown error'), 'error');
            }
        } catch (error) {
            console.error('Failed to load cluster status:', error);
            this.renderStatusOffline();
        }
    }

    // Load cluster members from API
    async loadMembers() {
        try {
            this.showLoading(true);

            var response = await fetch('/api/v1/cluster/members');
            if (!response.ok) {
                throw new Error('HTTP ' + response.status);
            }

            var data = await response.json();
            if (data.success) {
                this.members = data.data || [];
                this.renderMembers();
            } else {
                this.showError('Failed to load members: ' + (data.error && data.error.message || 'Unknown error'));
            }
        } catch (error) {
            console.error('Failed to load members:', error);
            this.showError('Failed to load members: ' + error.message);
        } finally {
            this.showLoading(false);
        }
    }

    // Refresh all data
    async refresh() {
        await Promise.all([
            this.loadClusterStatus(),
            this.loadMembers()
        ]);
        this.showNotification('Cluster data refreshed', 'success');
    }

    // Render cluster status cards
    renderStatus() {
        var status = this.clusterStatus || {};

        // Node ID
        var nodeIdEl = document.getElementById('stat-node-id-value');
        if (nodeIdEl) {
            nodeIdEl.textContent = this.truncateId(status.node_id || status.id || '--');
            nodeIdEl.title = status.node_id || status.id || '';
        }

        // State
        var stateEl = document.getElementById('stat-state-value');
        var stateCard = document.getElementById('stat-state');
        if (stateEl) {
            var state = (status.state || status.role || '--').toLowerCase();
            stateEl.textContent = this.capitalizeFirst(state);

            // Update card styling based on state
            if (stateCard) {
                stateCard.className = 'stat-card';
                if (state === 'leader') stateCard.classList.add('stat-leader');
                else if (state === 'follower') stateCard.classList.add('stat-follower');
                else if (state === 'candidate') stateCard.classList.add('stat-candidate');
                else if (state === 'down' || state === 'shutdown') stateCard.classList.add('stat-down');
            }
        }

        // Leader
        var leaderEl = document.getElementById('stat-leader-value');
        if (leaderEl) {
            leaderEl.textContent = this.truncateId(status.leader_id || status.leader || '--');
            leaderEl.title = status.leader_id || status.leader || '';
        }

        // Term
        var termEl = document.getElementById('stat-term-value');
        if (termEl) {
            termEl.textContent = status.term != null ? String(status.term) : '--';
        }

        // Raft log info
        var commitEl = document.getElementById('raft-commit-index');
        if (commitEl) {
            commitEl.textContent = status.commit_index != null ? String(status.commit_index) : '--';
        }

        var appliedEl = document.getElementById('raft-applied-index');
        if (appliedEl) {
            appliedEl.textContent = status.applied_index != null ? String(status.applied_index) : '--';
        }

        var logLenEl = document.getElementById('raft-log-length');
        if (logLenEl) {
            logLenEl.textContent = status.log_length != null ? String(status.log_length) : '--';
        }
    }

    // Render status when cluster is offline/unreachable
    renderStatusOffline() {
        var fields = ['stat-node-id-value', 'stat-state-value', 'stat-leader-value', 'stat-term-value',
                      'raft-commit-index', 'raft-applied-index', 'raft-log-length'];
        fields.forEach(function(id) {
            var el = document.getElementById(id);
            if (el) el.textContent = '--';
        });

        var stateCard = document.getElementById('stat-state');
        if (stateCard) {
            stateCard.className = 'stat-card stat-down';
        }
    }

    // Render members table
    renderMembers() {
        var tbody = document.getElementById('members-table-body');
        if (!tbody) return;

        // Clear existing rows using safe DOM methods
        while (tbody.firstChild) {
            tbody.removeChild(tbody.firstChild);
        }

        var sorted = this.getSortedMembers();

        if (sorted.length === 0) {
            var tr = document.createElement('tr');
            var td = document.createElement('td');
            td.colSpan = 6;
            td.className = 'empty-cell';
            td.textContent = 'No cluster members found. Join a cluster to see members.';
            tr.appendChild(td);
            tbody.appendChild(tr);
            return;
        }

        var self = this;
        sorted.forEach(function(member) {
            var tr = document.createElement('tr');

            // Node ID
            var tdId = document.createElement('td');
            var nodeIdSpan = document.createElement('span');
            nodeIdSpan.className = 'node-id';
            nodeIdSpan.textContent = self.truncateId(member.id || member.node_id || '--');
            nodeIdSpan.title = member.id || member.node_id || '';
            tdId.appendChild(nodeIdSpan);
            tr.appendChild(tdId);

            // Address
            var tdAddr = document.createElement('td');
            var addrSpan = document.createElement('span');
            addrSpan.className = 'node-address';
            addrSpan.textContent = member.address || member.addr || '--';
            tdAddr.appendChild(addrSpan);
            tr.appendChild(tdAddr);

            // State
            var tdState = document.createElement('td');
            tdState.appendChild(self.createStateBadge(member.state || 'unknown'));
            tr.appendChild(tdState);

            // Role
            var tdRole = document.createElement('td');
            tdRole.appendChild(self.createRoleBadge(member.role || member.state || 'unknown'));
            tr.appendChild(tdRole);

            // Last Seen
            var tdLastSeen = document.createElement('td');
            var lastSeenSpan = document.createElement('span');
            lastSeenSpan.className = 'last-seen';
            if (member.last_seen || member.last_contact) {
                var ts = member.last_seen || member.last_contact;
                var formatted = self.formatLastSeen(ts);
                lastSeenSpan.textContent = formatted.text;
                lastSeenSpan.className = 'last-seen ' + formatted.className;
                lastSeenSpan.title = new Date(ts).toLocaleString();
            } else {
                lastSeenSpan.textContent = '--';
            }
            tdLastSeen.appendChild(lastSeenSpan);
            tr.appendChild(tdLastSeen);

            // Health
            var tdHealth = document.createElement('td');
            tdHealth.appendChild(self.createHealthBadge(member.health || member.healthy));
            tr.appendChild(tdHealth);

            tbody.appendChild(tr);
        });

        // Update sort header indicators
        this.updateSortHeaders();
    }

    // Create state badge element
    createStateBadge(state) {
        var normalizedState = (state || 'unknown').toLowerCase();
        var badge = document.createElement('span');
        var dot = document.createElement('span');
        dot.className = 'badge-dot';

        var badgeClass = 'state-badge state-badge-';
        var label = this.capitalizeFirst(normalizedState);

        switch (normalizedState) {
            case 'leader':
                badgeClass += 'leader';
                break;
            case 'follower':
                badgeClass += 'follower';
                break;
            case 'candidate':
                badgeClass += 'candidate';
                break;
            case 'down':
            case 'shutdown':
            case 'dead':
                badgeClass += 'down';
                label = 'Down';
                break;
            default:
                badgeClass += 'unknown';
                label = 'Unknown';
                break;
        }

        badge.className = badgeClass;
        badge.appendChild(dot);
        badge.appendChild(document.createTextNode(label));
        return badge;
    }

    // Create role badge element
    createRoleBadge(role) {
        var normalizedRole = (role || 'unknown').toLowerCase();
        var badge = document.createElement('span');
        badge.className = 'role-badge';

        switch (normalizedRole) {
            case 'leader':
                badge.classList.add('role-badge-leader');
                badge.textContent = 'Leader';
                break;
            case 'follower':
                badge.classList.add('role-badge-follower');
                badge.textContent = 'Follower';
                break;
            case 'candidate':
                badge.classList.add('role-badge-candidate');
                badge.textContent = 'Candidate';
                break;
            default:
                badge.textContent = this.capitalizeFirst(normalizedRole);
                break;
        }

        return badge;
    }

    // Create health badge element
    createHealthBadge(health) {
        var badge = document.createElement('span');
        badge.className = 'health-badge';

        if (health === true || health === 'healthy' || health === 'passing') {
            badge.classList.add('health-badge-healthy');
            badge.textContent = 'Healthy';
        } else if (health === false || health === 'unhealthy' || health === 'critical') {
            badge.classList.add('health-badge-unhealthy');
            badge.textContent = 'Unhealthy';
        } else {
            badge.classList.add('health-badge-unknown');
            badge.textContent = 'Unknown';
        }

        return badge;
    }

    // Format last seen timestamp
    formatLastSeen(timestamp) {
        if (!timestamp) return { text: '--', className: '' };

        var ts = new Date(timestamp);
        var now = new Date();
        var diffMs = now - ts;
        var diffSeconds = Math.floor(diffMs / 1000);
        var diffMinutes = Math.floor(diffSeconds / 60);
        var diffHours = Math.floor(diffMinutes / 60);
        var diffDays = Math.floor(diffHours / 24);

        var text;
        var className;

        if (diffSeconds < 30) {
            text = 'Just now';
            className = 'last-seen-recent';
        } else if (diffSeconds < 60) {
            text = diffSeconds + 's ago';
            className = 'last-seen-recent';
        } else if (diffMinutes < 60) {
            text = diffMinutes + 'm ago';
            className = diffMinutes < 5 ? 'last-seen-recent' : 'last-seen-stale';
        } else if (diffHours < 24) {
            text = diffHours + 'h ago';
            className = 'last-seen-stale';
        } else {
            text = diffDays + 'd ago';
            className = 'last-seen-lost';
        }

        return { text: text, className: className };
    }

    // Get sorted members
    getSortedMembers() {
        var sorted = this.members.slice();
        var sortBy = this.sortBy;
        var sortDesc = this.sortDesc;

        sorted.sort(function(a, b) {
            var aVal = a[sortBy] || '';
            var bVal = b[sortBy] || '';

            // Handle node_id / id aliasing
            if (sortBy === 'id') {
                aVal = a.id || a.node_id || '';
                bVal = b.id || b.node_id || '';
            }

            if (typeof aVal === 'string') aVal = aVal.toLowerCase();
            if (typeof bVal === 'string') bVal = bVal.toLowerCase();

            if (aVal < bVal) return sortDesc ? 1 : -1;
            if (aVal > bVal) return sortDesc ? -1 : 1;
            return 0;
        });

        return sorted;
    }

    // Set sort column
    setSort(key) {
        if (this.sortBy === key) {
            this.sortDesc = !this.sortDesc;
        } else {
            this.sortBy = key;
            this.sortDesc = false;
        }
        this.renderMembers();
    }

    // Update sort header visual indicators
    updateSortHeaders() {
        var self = this;
        document.querySelectorAll('.data-table th.sortable').forEach(function(th) {
            th.classList.remove('sort-asc', 'sort-desc');
            if (th.dataset.sort === self.sortBy) {
                th.classList.add(self.sortDesc ? 'sort-desc' : 'sort-asc');
            }
        });
    }

    // Show join cluster modal
    showJoinModal() {
        var modal = document.getElementById('join-modal');
        if (modal) {
            modal.classList.add('visible');
            var seedInput = document.getElementById('seed-address');
            if (seedInput) {
                seedInput.value = '';
                seedInput.focus();
            }
        }
    }

    // Hide join cluster modal
    hideJoinModal() {
        var modal = document.getElementById('join-modal');
        if (modal) {
            modal.classList.remove('visible');
        }
    }

    // Show leave cluster modal
    showLeaveModal() {
        var modal = document.getElementById('leave-modal');
        if (modal) {
            modal.classList.add('visible');
        }
    }

    // Hide leave cluster modal
    hideLeaveModal() {
        var modal = document.getElementById('leave-modal');
        if (modal) {
            modal.classList.remove('visible');
        }
    }

    // Join cluster
    async joinCluster() {
        var seedInput = document.getElementById('seed-address');
        if (!seedInput) return;

        var address = seedInput.value.trim();
        if (!address) {
            this.showNotification('Please enter a seed address', 'warning');
            return;
        }

        try {
            var response = await fetch('/api/v1/cluster/join', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ address: address })
            });

            var data = await response.json();
            if (response.ok && data.success) {
                this.hideJoinModal();
                this.showNotification('Successfully joined cluster via ' + address, 'success');
                await this.refresh();
            } else {
                this.showNotification('Failed to join cluster: ' + (data.error && data.error.message || 'Unknown error'), 'error');
            }
        } catch (error) {
            this.showNotification('Failed to join cluster: ' + error.message, 'error');
        }
    }

    // Leave cluster
    async leaveCluster() {
        try {
            var response = await fetch('/api/v1/cluster/leave', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' }
            });

            var data = await response.json();
            if (response.ok && data.success) {
                this.hideLeaveModal();
                this.showNotification('Successfully left the cluster', 'success');
                await this.refresh();
            } else {
                this.showNotification('Failed to leave cluster: ' + (data.error && data.error.message || 'Unknown error'), 'error');
            }
        } catch (error) {
            this.showNotification('Failed to leave cluster: ' + error.message, 'error');
        }
    }

    // Connect to WebSocket for live updates
    connectWebSocket() {
        if (typeof WSClient === 'undefined') return;

        this.ws = new WSClient({ debug: false });
        var self = this;

        this.ws.on('cluster_status', function(data) {
            if (data && data.payload) {
                self.clusterStatus = data.payload;
            } else if (data) {
                self.clusterStatus = data;
            }
            self.renderStatus();
        });

        this.ws.on('cluster_members', function(data) {
            if (data && data.payload) {
                self.members = data.payload;
            } else if (data && Array.isArray(data)) {
                self.members = data;
            }
            self.renderMembers();
        });

        this.ws.on('open', function() {
            self.ws.subscribe('cluster');
        });

        this.ws.connect();
    }

    // Start auto-refresh interval
    startAutoRefresh() {
        var self = this;
        this.refreshInterval = setInterval(function() {
            self.loadClusterStatus();
            self.loadMembers();
        }, 10000); // Refresh every 10 seconds
    }

    // Stop auto-refresh
    stopAutoRefresh() {
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
            this.refreshInterval = null;
        }
    }

    // Show loading overlay
    showLoading(show) {
        var overlay = document.getElementById('members-loading');
        if (overlay) {
            overlay.style.display = show ? 'flex' : 'none';
        }
    }

    // Show error in table
    showError(message) {
        var tbody = document.getElementById('members-table-body');
        if (!tbody) return;

        while (tbody.firstChild) {
            tbody.removeChild(tbody.firstChild);
        }
        var tr = document.createElement('tr');
        var td = document.createElement('td');
        td.colSpan = 6;
        td.className = 'error-cell';
        td.textContent = message;
        tr.appendChild(td);
        tbody.appendChild(tr);
    }

    // Show notification
    showNotification(message, type) {
        type = type || 'info';
        var container = document.getElementById('notification-container');
        if (!container) return;

        var notification = document.createElement('div');
        notification.className = 'notification ' + type;
        notification.textContent = message;

        container.appendChild(notification);

        setTimeout(function() {
            notification.classList.add('fade-out');
            setTimeout(function() {
                if (notification.parentNode) {
                    notification.parentNode.removeChild(notification);
                }
            }, 300);
        }, 4000);
    }

    // Truncate long IDs for display
    truncateId(id) {
        if (!id || id.length <= 12) return id || '--';
        return id.substring(0, 8) + '...' + id.substring(id.length - 4);
    }

    // Capitalize first letter
    capitalizeFirst(str) {
        if (!str) return '';
        return str.charAt(0).toUpperCase() + str.slice(1);
    }

    // Cleanup when leaving the page
    destroy() {
        this.stopAutoRefresh();
        if (this.ws) {
            this.ws.disconnect();
            this.ws = null;
        }
    }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', function() {
    var page = new ClusterPage();
    page.init();

    // Cleanup on page unload
    window.addEventListener('beforeunload', function() {
        page.destroy();
    });
});

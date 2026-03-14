// OpenLoadBalancer Navigation
// Phase 3.1: Sidebar Navigation and Breadcrumbs

(function(global) {
    'use strict';

    const Navigation = {
        items: [
            { id: 'dashboard', label: 'Dashboard', icon: '⊞', path: '/dashboard', title: 'Dashboard' },
            { id: 'backends', label: 'Backends', icon: '◫', path: '/backends', title: 'Backends' },
            { id: 'routes', label: 'Routes', icon: '⎇', path: '/routes', title: 'Routes' },
            { id: 'metrics', label: 'Metrics', icon: '◧', path: '/metrics', title: 'Metrics' },
            { id: 'logs', label: 'Logs', icon: '▤', path: '/logs', title: 'Logs' },
            { type: 'divider' },
            { id: 'config', label: 'Configuration', icon: '⚙', path: '/config', title: 'Configuration' },
            { id: 'cluster', label: 'Cluster', icon: '⊕', path: '/cluster', title: 'Cluster' },
            { id: 'certs', label: 'Certificates', icon: '🔒', path: '/certs', title: 'Certificates' }
        ],

        init() {
            this.renderSidebar();
            this.renderBreadcrumbs();
            this.setupMobileToggle();
        },

        renderSidebar() {
            const sidebar = document.getElementById('sidebar');
            if (!sidebar) return;
            sidebar.innerHTML = '';

            const header = document.createElement('div');
            header.className = 'sidebar-header';
            const logo = document.createElement('a');
            logo.className = 'flex items-center gap-3 text-white no-underline';
            logo.href = '#/dashboard';
            logo.innerHTML = '<span class="text-2xl">⚡</span><span class="font-bold text-lg">OpenLB</span>';
            header.appendChild(logo);
            sidebar.appendChild(header);

            const nav = document.createElement('nav');
            nav.className = 'sidebar-nav';

            this.items.forEach(item => {
                if (item.type === 'divider') {
                    const divider = document.createElement('div');
                    divider.className = 'my-4 border-t border-white/10';
                    nav.appendChild(divider);
                } else {
                    const link = document.createElement('a');
                    link.className = 'nav-item';
                    link.dataset.nav = item.path;
                    link.href = '#' + item.path;
                    link.innerHTML = '<span class="nav-icon">' + item.icon + '</span><span class="nav-text">' + item.label + '</span>';
                    link.addEventListener('click', (e) => { e.preventDefault(); this.navigate(item.path); });
                    nav.appendChild(link);
                }
            });

            sidebar.appendChild(nav);

            const footer = document.createElement('div');
            footer.className = 'sidebar-footer';
            const version = document.createElement('div');
            version.className = 'text-xs text-gray-500';
            version.textContent = 'v' + (window.OLB_VERSION || 'dev');
            footer.appendChild(version);
            sidebar.appendChild(footer);
        },

        navigate(path) {
            if (window.OLBSPA) { window.OLBSPA.navigate(path); }
            else { window.location.hash = path; }
            this.updateActiveState(path);
            this.renderBreadcrumbs();
            const sidebar = document.getElementById('sidebar');
            if (sidebar) { sidebar.classList.remove('open'); }
        },

        updateActiveState(path) {
            document.querySelectorAll('.nav-item').forEach(item => {
                const itemPath = item.getAttribute('data-nav');
                if (itemPath === path || path.startsWith(itemPath + '/')) { item.classList.add('active'); }
                else { item.classList.remove('active'); }
            });
        },

        renderBreadcrumbs() {
            const container = document.getElementById('breadcrumbs');
            if (!container) return;
            const hash = window.location.hash.slice(1) || '/dashboard';
            const parts = hash.split('/').filter(p => p);
            const breadcrumbs = document.createElement('nav');
            breadcrumbs.className = 'breadcrumbs';
            breadcrumbs.innerHTML = '<div class="breadcrumb-item"><a href="#/dashboard">Home</a></div>';
            breadcrumbs.querySelector('a').addEventListener('click', (e) => { e.preventDefault(); this.navigate('/dashboard'); });

            let currentPath = '';
            parts.forEach((part, index) => {
                currentPath += '/' + part;
                const separator = document.createElement('span');
                separator.className = 'breadcrumb-separator';
                separator.textContent = '/';
                breadcrumbs.appendChild(separator);
                const item = document.createElement('div');
                item.className = 'breadcrumb-item';
                if (index === parts.length - 1) {
                    item.classList.add('active');
                    const navItem = this.items.find(i => i.path === currentPath);
                    item.textContent = navItem ? navItem.label : part.charAt(0).toUpperCase() + part.slice(1);
                } else {
                    const link = document.createElement('a');
                    link.href = '#' + currentPath;
                    link.textContent = part.charAt(0).toUpperCase() + part.slice(1);
                    link.addEventListener('click', (e) => { e.preventDefault(); this.navigate(currentPath); });
                    item.appendChild(link);
                }
                breadcrumbs.appendChild(item);
            });
            container.innerHTML = '';
            container.appendChild(breadcrumbs);
        },

        setupMobileToggle() {
            const toggle = document.getElementById('sidebar-toggle');
            const sidebar = document.getElementById('sidebar');
            if (toggle && sidebar) {
                toggle.addEventListener('click', () => { sidebar.classList.toggle('open'); });
                document.addEventListener('click', (e) => {
                    if (sidebar.classList.contains('open') && !sidebar.contains(e.target) && !toggle.contains(e.target)) {
                        sidebar.classList.remove('open');
                    }
                });
            }
        }
    };

    global.Navigation = Navigation;

})(window);

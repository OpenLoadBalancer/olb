// OpenLoadBalancer WebSocket Client
// Phase 3.1: WebSocket Client with Auto-Reconnect

(function(global) {
    'use strict';

    class WSClient {
        constructor(options = {}) {
            this.url = options.url || this.getDefaultUrl();
            this.reconnectInterval = options.reconnectInterval || 5000;
            this.maxReconnectInterval = options.maxReconnectInterval || 30000;
            this.reconnectDecay = options.reconnectDecay || 1.5;
            this.maxReconnectAttempts = options.maxReconnectAttempts || 0;
            this.heartbeatInterval = options.heartbeatInterval || 30000;
            this.debug = options.debug || false;

            this.ws = null;
            this.reconnectAttempts = 0;
            this.shouldReconnect = true;
            this.handlers = new Map();
            this.status = 'disconnected';
            this.heartbeatTimer = null;
            this.reconnectTimer = null;

            this.connect = this.connect.bind(this);
            this.disconnect = this.disconnect.bind(this);
            this.send = this.send.bind(this);
        }

        getDefaultUrl() {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            return protocol + '//' + window.location.host + '/ws';
        }

        connect() {
            if (this.ws && (this.ws.readyState === WebSocket.CONNECTING || this.ws.readyState === WebSocket.OPEN)) {
                this.log('Already connected or connecting');
                return;
            }

            this.shouldReconnect = true;
            this.status = 'connecting';
            this.emit('status', { status: this.status });

            try {
                this.log('Connecting to', this.url);
                this.ws = new WebSocket(this.url);

                this.ws.onopen = (event) => {
                    this.log('Connected');
                    this.status = 'connected';
                    this.reconnectAttempts = 0;
                    this.emit('open', event);
                    this.emit('status', { status: this.status });
                    this.startHeartbeat();
                };

                this.ws.onmessage = (event) => {
                    this.handleMessage(event.data);
                };

                this.ws.onclose = (event) => {
                    this.log('Disconnected', event.code, event.reason);
                    this.status = 'disconnected';
                    this.stopHeartbeat();
                    this.emit('close', event);
                    this.emit('status', { status: this.status });

                    if (this.shouldReconnect) {
                        this.scheduleReconnect();
                    }
                };

                this.ws.onerror = (error) => {
                    this.log('Error', error);
                    this.emit('error', error);
                };

            } catch (error) {
                this.log('Connection error', error);
                this.emit('error', error);
                this.scheduleReconnect();
            }
        }

        disconnect() {
            this.shouldReconnect = false;
            this.stopHeartbeat();

            if (this.reconnectTimer) {
                clearTimeout(this.reconnectTimer);
                this.reconnectTimer = null;
            }

            if (this.ws) {
                this.ws.close();
                this.ws = null;
            }
        }

        scheduleReconnect() {
            if (this.maxReconnectAttempts > 0 && this.reconnectAttempts >= this.maxReconnectAttempts) {
                this.log('Max reconnection attempts reached');
                this.emit('maxReconnectAttemptsReached');
                return;
            }

            this.reconnectAttempts++;
            const interval = Math.min(
                this.reconnectInterval * Math.pow(this.reconnectDecay, this.reconnectAttempts - 1),
                this.maxReconnectInterval
            );

            this.log('Reconnecting in ' + interval + 'ms (attempt ' + this.reconnectAttempts + ')');
            this.emit('reconnecting', { attempt: this.reconnectAttempts, interval });

            this.reconnectTimer = setTimeout(() => {
                this.connect();
            }, interval);
        }

        handleMessage(data) {
            try {
                const message = JSON.parse(data);
                this.log('Received message', message);
                this.emit('message', message);

                if (message.type) {
                    this.emit(message.type, message.payload || message);
                }

                if (message.type === 'pong') {
                    this.emit('pong');
                }

            } catch (error) {
                this.log('Received raw message', data);
                this.emit('message', data);
            }
        }

        send(type, payload) {
            if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
                this.log('Cannot send, not connected');
                return false;
            }

            const message = typeof type === 'object'
                ? JSON.stringify(type)
                : JSON.stringify({ type, payload, timestamp: Date.now() });

            this.log('Sending message', message);
            this.ws.send(message);
            return true;
        }

        ping() {
            return this.send('ping', { time: Date.now() });
        }

        startHeartbeat() {
            if (this.heartbeatTimer) {
                clearInterval(this.heartbeatTimer);
            }

            this.heartbeatTimer = setInterval(() => {
                if (this.status === 'connected') {
                    this.ping();
                }
            }, this.heartbeatInterval);
        }

        stopHeartbeat() {
            if (this.heartbeatTimer) {
                clearInterval(this.heartbeatTimer);
                this.heartbeatTimer = null;
            }
        }

        on(type, handler) {
            if (!this.handlers.has(type)) {
                this.handlers.set(type, new Set());
            }
            this.handlers.get(type).add(handler);

            return () => this.off(type, handler);
        }

        off(type, handler) {
            if (this.handlers.has(type)) {
                this.handlers.get(type).delete(handler);
            }
        }

        emit(type, data) {
            if (this.handlers.has(type)) {
                this.handlers.get(type).forEach(handler => {
                    try {
                        handler(data);
                    } catch (error) {
                        console.error('WS handler error:', error);
                    }
                });
            }
        }

        subscribe(channel) {
            return this.send('subscribe', { channel });
        }

        unsubscribe(channel) {
            return this.send('unsubscribe', { channel });
        }

        isConnected() {
            return this.status === 'connected' && this.ws && this.ws.readyState === WebSocket.OPEN;
        }

        getStatus() {
            return this.status;
        }

        log(...args) {
            if (this.debug) {
                console.log('[WS]', ...args);
            }
        }
    }

    class ConnectionStatus {
        constructor(options = {}) {
            this.client = options.client;
            this.container = options.container || this.createDefaultContainer();
            this.showLabel = options.showLabel !== false;

            if (this.client) {
                this.bindToClient(this.client);
            }
        }

        createDefaultContainer() {
            const el = document.createElement('div');
            el.className = 'connection-status';
            return el;
        }

        bindToClient(client) {
            this.client = client;

            client.on('status', (data) => {
                this.update(data.status);
            });

            client.on('reconnecting', (data) => {
                this.update('reconnecting', data.attempt);
            });

            this.update(client.getStatus());
        }

        update(status, attempt) {
            const statusConfig = {
                connected: { icon: '●', color: '#22c55e', label: 'Connected' },
                connecting: { icon: '◐', color: '#f59e0b', label: 'Connecting...' },
                disconnected: { icon: '○', color: '#ef4444', label: 'Disconnected' },
                reconnecting: { icon: '◐', color: '#f59e0b', label: 'Reconnecting (' + attempt + ')...' }
            };

            const config = statusConfig[status] || statusConfig.disconnected;

            this.container.innerHTML = '';

            const iconSpan = document.createElement('span');
            iconSpan.className = 'status-icon';
            iconSpan.style.color = config.color;
            iconSpan.textContent = config.icon;
            this.container.appendChild(iconSpan);

            if (this.showLabel) {
                const labelSpan = document.createElement('span');
                labelSpan.className = 'status-label';
                labelSpan.textContent = config.label;
                this.container.appendChild(labelSpan);
            }

            this.container.className = 'connection-status status-' + status;
        }

        mount(selector) {
            const target = typeof selector === 'string'
                ? document.querySelector(selector)
                : selector;

            if (target) {
                target.appendChild(this.container);
            }

            return this;
        }
    }

    global.WSClient = WSClient;
    global.ConnectionStatus = ConnectionStatus;

})(window);

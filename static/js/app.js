/**
 * app.js - 主控制器模块
 * 负责全局状态管理、WebSocket连接、消息路由、模块协调
 *
 * 多客户端设计：
 * - 每个浏览器标签页是独立的App实例
 * - 新标签页默认创建新会话，通过sidebar可切换到已有会话
 * - 重连时携带currentSessionId，复用已有会话而非创建新的
 * - 会话不存在时才创建新会话
 */
const App = {
    ws: null,                   // WebSocket连接实例
    token: null,                // 访问令牌
    currentSessionId: null,     // 当前活跃会话ID
    currentSessionName: '',     // 当前活跃会话名称
    reconnectAttempts: 0,       // 重连尝试次数
    maxReconnectAttempts: 3,    // 最大重连次数
    reconnectTimer: null,       // 重连定时器
    manualClose: false,         // 是否主动关闭（不触发自动重连）
    pingInterval: null,         // 心跳定时器
    _initialized: false,        // UI是否已初始化
    _tokenSubmitBound: false,   // Token提交事件是否已绑定
    _connecting: false,         // 是否正在连接中（防止并发连接）

    /**
     * 初始化应用
     */
    init() {
        const savedToken = localStorage.getItem('terminal_token');
        if (savedToken) {
            this.token = savedToken;
            this.startApp();
        } else {
            this.showTokenModal();
        }
    },

    /**
     * 显示Token输入弹窗
     */
    showTokenModal() {
        document.getElementById('token-modal').style.display = 'flex';
        document.getElementById('token-input').value = '';
        document.getElementById('token-error').style.display = 'none';

        if (!this._tokenSubmitBound) {
            this._tokenSubmitBound = true;

            document.getElementById('token-submit').addEventListener('click', () => {
                this._submitToken();
            });
            document.getElementById('token-input').addEventListener('keydown', (e) => {
                if (e.key === 'Enter') {
                    this._submitToken();
                }
            });
        }

        setTimeout(() => {
            document.getElementById('token-input').focus();
        }, 100);
    },

    /**
     * 提交Token
     */
    _submitToken() {
        const token = document.getElementById('token-input').value.trim();
        if (!token) {
            document.getElementById('token-error').textContent = 'Token不能为空';
            document.getElementById('token-error').style.display = 'block';
            return;
        }
        if (token.length < 8) {
            document.getElementById('token-error').textContent = 'Token长度不能少于8个字符';
            document.getElementById('token-error').style.display = 'block';
            return;
        }
        this.token = token;
        localStorage.setItem('terminal_token', token);
        document.getElementById('token-modal').style.display = 'none';
        this.startApp();
    },

    /**
     * 启动应用主界面
     */
    async startApp() {
        // 首次初始化UI（只执行一次）
        if (!this._initialized) {
            this._initialized = true;

            document.getElementById('app').style.display = 'flex';

            const container = document.getElementById('xterm-terminal');
            TermMgr.init(container);

            Keyboard.init();
            Sidebar.init();

            document.getElementById('btn-export').addEventListener('click', () => {
                Export.exportOutput(TermMgr.term, this.currentSessionName);
            });

            document.getElementById('btn-keyboard-toggle').addEventListener('click', () => {
                Keyboard.toggle();
            });

            document.getElementById('btn-reconnect').addEventListener('click', () => {
                this.reconnectAttempts = 0;
                this.connect(this.currentSessionId);
            });
        }

        // 验证Token有效性并连接
        try {
            await this.apiRequest('GET', '/api/sessions');
            // Token有效，建立WebSocket连接（新标签页不传sessionId，让服务端创建新会话）
            this.connect();
        } catch (e) {
            localStorage.removeItem('terminal_token');
            this.token = null;
            this.showToast('Token验证失败，请重新输入', 'error');
            this.showTokenModal();
        }
    },

    /**
     * 建立WebSocket连接
     * @param {string} sessionId - 会话ID（可选）
     *   - 不传：让服务端创建新会话（新标签页首次连接）
     *   - 传入：加入/重连已有会话（切换会话、断线重连）
     */
    connect(sessionId) {
        // 防止并发连接
        if (this._connecting) return;
        this._connecting = true;

        // 关闭旧连接
        if (this.ws) {
            this.manualClose = true;
            this.ws.close();
            this.ws = null;
        }

        this.manualClose = false;
        this.updateConnectionStatus('connecting');

        // 构建WebSocket URL
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const host = window.location.host;
        let url = `${protocol}//${host}/ws?token=${encodeURIComponent(this.token)}`;
        if (sessionId) {
            url += `&session_id=${encodeURIComponent(sessionId)}`;
        }

        try {
            this.ws = new WebSocket(url);
        } catch (e) {
            console.error('[App] WebSocket creation failed:', e);
            this._connecting = false;
            this.updateConnectionStatus('disconnected');
            this.scheduleReconnect();
            return;
        }

        this.ws.onopen = () => {
            this._connecting = false;
            console.log('[App] WebSocket connected, sessionId=' + (sessionId || 'new'));
            this.reconnectAttempts = 0;
            this.updateConnectionStatus('connected');

            // 发送当前终端尺寸
            this.sendResize(TermMgr.getRows(), TermMgr.getCols());

            // 启动心跳
            this.startPing();

            // 刷新会话列表
            Sidebar.refreshSessions();
        };

        this.ws.onmessage = (event) => {
            try {
                const msg = JSON.parse(event.data);
                this.handleMessage(msg);
            } catch (e) {
                console.error('[App] failed to parse message:', e);
            }
        };

        this.ws.onclose = (event) => {
            this._connecting = false;
            console.log('[App] WebSocket closed:', event.code, event.reason);
            this.stopPing();
            this.updateConnectionStatus('disconnected');

            if (!this.manualClose) {
                this.scheduleReconnect();
            }
        };

        this.ws.onerror = (event) => {
            this._connecting = false;
            console.error('[App] WebSocket error:', event);
            this.updateConnectionStatus('disconnected');
        };
    },

    /**
     * 处理接收到的WebSocket消息
     */
    handleMessage(msg) {
        switch (msg.type) {
            case 'output':
                this.handleOutput(msg);
                break;
            case 'session_info':
                this.handleSessionInfo(msg);
                break;
            case 'error':
                this.handleError(msg);
                break;
            case 'pty_resize':
                this.handlePtyResize(msg);
                break;
            case 'pong':
                break;
            default:
                console.log('[App] unknown message type:', msg.type);
        }
    },

    /**
     * 处理终端输出消息
     * Base64解码后转为Uint8Array传给xterm.js，
     * xterm.js内部有完善的UTF-8解析器，能正确处理多字节字符（如中文）
     */
    handleOutput(msg) {
        if (msg.data) {
            try {
                const binaryStr = atob(msg.data);
                const bytes = new Uint8Array(binaryStr.length);
                for (let i = 0; i < binaryStr.length; i++) {
                    bytes[i] = binaryStr.charCodeAt(i);
                }
                TermMgr.write(bytes);
            } catch (e) {
                console.error('[App] failed to decode output data:', e);
            }
        }
    },

    /**
     * 处理会话信息消息
     * 收到session_info表示服务端已成功建立/找到会话
     */
    handleSessionInfo(msg) {
        if (msg.id) {
            this.currentSessionId = msg.id;
            this.currentSessionName = msg.name || '';
            this.updateTitle(this.currentSessionName);
            Sidebar.refreshSessions();
        }
    },

    /**
     * 处理PTY尺寸变更通知
     */
    handlePtyResize(msg) {
        if (msg.rows > 0 && msg.cols > 0) {
            TermMgr.resizeToPtySize(msg.rows, msg.cols);
        }
    },

    /**
     * 处理错误消息
     * 关键：SESSION_NOT_FOUND/SESSION_EXPIRED时重连到同session而非创建新的
     */
    handleError(msg) {
        console.error('[App] server error:', msg.code, msg.message);

        if (msg.code === 'SESSION_EXITED') {
            this.showToast('Shell进程已退出', 'error');
            Sidebar.refreshSessions();
        } else if (msg.code === 'SESSION_NOT_FOUND' || msg.code === 'SESSION_EXPIRED') {
            // 会话已不存在，必须清空sessionId后创建新会话
            this.showToast('会话不存在或已过期，将创建新会话', 'error');
            this.currentSessionId = null;
            // 关闭当前连接，让scheduleReconnect创建新会话
            if (this.ws) {
                this.manualClose = true;
                this.ws.close();
                this.ws = null;
            }
            // 不带sessionId重连，服务端会创建新会话
            this.reconnectAttempts = 0;
            setTimeout(() => this.connect(), 1000);
        } else if (msg.code === 'SESSION_CREATE_FAILED') {
            this.showToast('会话创建失败: ' + (msg.message || ''), 'error');
        } else {
            this.showToast('错误: ' + (msg.message || msg.code || '未知错误'), 'error');
        }
    },

    /**
     * 发送终端输入数据
     */
    sendInput(data) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;

        try {
            const encoded = btoa(unescape(encodeURIComponent(data)));
            this.ws.send(JSON.stringify({ type: 'input', data: encoded }));
        } catch (e) {
            console.error('[App] failed to send input:', e);
        }
    },

    /**
     * 发送终端resize消息
     */
    sendResize(rows, cols) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
        if (rows <= 0 || cols <= 0) return;

        this.ws.send(JSON.stringify({ type: 'resize', rows: rows, cols: cols }));
    },

    /**
     * 自动重连（指数退避）
     * 重连时携带currentSessionId，复用已有会话
     */
    scheduleReconnect() {
        if (this.reconnectAttempts >= this.maxReconnectAttempts) {
            this.showToast('重连失败次数过多，请手动刷新页面', 'error');
            this.updateConnectionStatus('failed');
            return;
        }

        this.reconnectAttempts++;
        const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts - 1), 10000);

        this.updateConnectionStatus('connecting');
        this.showToast(`正在重连... (${this.reconnectAttempts}/${this.maxReconnectAttempts})`, 'error');

        // 携带currentSessionId重连，复用已有会话而非创建新的
        this.reconnectTimer = setTimeout(() => {
            this.connect(this.currentSessionId);
        }, delay);
    },

    /**
     * 启动心跳
     */
    startPing() {
        this.stopPing();
        this.pingInterval = setInterval(() => {
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                this.ws.send(JSON.stringify({ type: 'ping' }));
            }
        }, 30000);
    },

    /**
     * 停止心跳
     */
    stopPing() {
        if (this.pingInterval) {
            clearInterval(this.pingInterval);
            this.pingInterval = null;
        }
    },

    /**
     * 更新连接状态UI
     */
    updateConnectionStatus(status) {
        const el = document.getElementById('connection-status');
        const dot = el?.querySelector('.status-dot');
        const text = el?.querySelector('.status-text');

        if (!el) return;

        if (status === 'connected') {
            el.style.display = 'none';
        } else {
            el.style.display = 'flex';
            dot.className = 'status-dot ' + status;

            const labels = {
                'connecting': '连接中...',
                'disconnected': '已断开',
                'failed': '连接失败'
            };
            text.textContent = labels[status] || '未知状态';
        }
    },

    /**
     * 更新标题栏
     */
    updateTitle(name) {
        this.currentSessionName = name || '';
        const titleEl = document.getElementById('toolbar-title');
        if (titleEl) {
            titleEl.textContent = name ? `终端 - ${name}` : 'Web Terminal';
        }
    },

    /**
     * HTTP API请求封装
     */
    async apiRequest(method, path, body) {
        const headers = {
            'Authorization': `Bearer ${this.token}`,
            'Content-Type': 'application/json'
        };

        const options = { method, headers };
        if (body) {
            options.body = JSON.stringify(body);
        }

        const response = await fetch(path, options);
        const data = await response.json();

        if (data.code !== 0) {
            if (data.code === 40100) {
                localStorage.removeItem('terminal_token');
                this.token = null;
                this.showTokenModal();
            }
            throw new Error(data.message || 'API请求失败');
        }

        return data;
    },

    /**
     * 显示Toast通知
     */
    showToast(message, type) {
        document.querySelectorAll('.toast').forEach(t => t.remove());

        const toast = document.createElement('div');
        toast.className = 'toast' + (type ? ' ' + type : '');
        toast.textContent = message;
        document.body.appendChild(toast);

        setTimeout(() => {
            if (toast.parentNode) {
                toast.remove();
            }
        }, 3000);
    }
};

// ==================== 启动应用 ====================
document.addEventListener('DOMContentLoaded', () => {
    App.init();
});

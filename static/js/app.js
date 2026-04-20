/**
 * app.js - 主控制器模块
 * 负责全局状态管理、WebSocket连接、消息路由、模块协调
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

    /**
     * 初始化应用
     */
    init() {
        // 检查Token
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

        const submitHandler = () => {
            const token = document.getElementById('token-input').value.trim();
            if (!token) {
                document.getElementById('token-error').textContent = 'Token不能为空';
                document.getElementById('token-error').style.display = 'block';
                return;
            }
            this.token = token;
            localStorage.setItem('terminal_token', token);
            document.getElementById('token-modal').style.display = 'none';
            this.startApp();
        };

        document.getElementById('token-submit').onclick = submitHandler;
        document.getElementById('token-input').onkeydown = (e) => {
            if (e.key === 'Enter') submitHandler();
        };

        document.getElementById('token-input').focus();
    },

    /**
     * 启动应用主界面
     */
    async startApp() {
        // 显示主界面
        document.getElementById('app').style.display = 'flex';

        // 初始化终端
        const container = document.getElementById('xterm-terminal');
        TermMgr.init(container);

        // 初始化虚拟键盘
        Keyboard.init();

        // 初始化侧边栏
        Sidebar.init();

        // 绑定导出按钮
        document.getElementById('btn-export').addEventListener('click', () => {
            Export.exportOutput(TermMgr.term, this.currentSessionName);
        });

        // 绑定虚拟键盘切换按钮
        document.getElementById('btn-keyboard-toggle').addEventListener('click', () => {
            Keyboard.toggle();
        });

        // 绑定重连按钮
        document.getElementById('btn-reconnect').addEventListener('click', () => {
            this.reconnectAttempts = 0;
            this.connect(this.currentSessionId);
        });

        // 验证Token有效性并连接
        try {
            await this.apiRequest('GET', '/api/sessions');
            // Token有效，建立WebSocket连接
            this.connect();
        } catch (e) {
            // Token无效
            localStorage.removeItem('terminal_token');
            this.token = null;
            this.showToast('Token验证失败，请重新输入', 'error');
            this.showTokenModal();
        }
    },

    /**
     * 建立WebSocket连接
     * @param {string} sessionId - 会话ID（可选，用于重连）
     */
    connect(sessionId) {
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
            this.updateConnectionStatus('disconnected');
            this.scheduleReconnect();
            return;
        }

        this.ws.onopen = () => {
            console.log('[App] WebSocket connected');
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
            console.log('[App] WebSocket closed:', event.code, event.reason);
            this.stopPing();
            this.updateConnectionStatus('disconnected');

            if (!this.manualClose) {
                this.scheduleReconnect();
            }
        };

        this.ws.onerror = (event) => {
            console.error('[App] WebSocket error:', event);
            this.updateConnectionStatus('disconnected');
        };
    },

    /**
     * 处理接收到的WebSocket消息
     * @param {object} msg - 解析后的消息对象
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

            case 'pong':
                // 心跳响应，无需处理
                break;

            default:
                console.log('[App] unknown message type:', msg.type);
        }
    },

    /**
     * 处理终端输出消息
     */
    handleOutput(msg) {
        if (msg.data) {
            try {
                const decoded = atob(msg.data);
                TermMgr.write(decoded);
            } catch (e) {
                console.error('[App] failed to decode output data:', e);
            }
        }
    },

    /**
     * 处理会话信息消息
     */
    handleSessionInfo(msg) {
        if (msg.id) {
            this.currentSessionId = msg.id;
            this.currentSessionName = msg.name || '';
            this.updateTitle(this.currentSessionName);

            // 更新侧边栏高亮
            Sidebar.refreshSessions();
        }
    },

    /**
     * 处理错误消息
     */
    handleError(msg) {
        console.error('[App] server error:', msg.code, msg.message);

        if (msg.code === 'SESSION_EXITED') {
            this.showToast('Shell进程已退出', 'error');
            // 刷新会话列表
            Sidebar.refreshSessions();
        } else if (msg.code === 'SESSION_NOT_FOUND' || msg.code === 'SESSION_EXPIRED') {
            this.showToast('会话不存在或已过期，将创建新会话', 'error');
            this.currentSessionId = null;
            setTimeout(() => this.connect(), 1000);
        } else if (msg.code === 'SESSION_BUSY') {
            this.showToast('会话已有其他连接', 'error');
        } else if (msg.code === 'SESSION_CREATE_FAILED') {
            this.showToast('会话创建失败: ' + (msg.message || ''), 'error');
        } else {
            this.showToast('错误: ' + (msg.message || msg.code || '未知错误'), 'error');
        }
    },

    /**
     * 发送终端输入数据
     * @param {string} data - 输入数据（原始字符串）
     */
    sendInput(data) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;

        try {
            const encoded = btoa(unescape(encodeURIComponent(data)));
            const msg = {
                type: 'input',
                data: encoded
            };
            this.ws.send(JSON.stringify(msg));
        } catch (e) {
            console.error('[App] failed to send input:', e);
        }
    },

    /**
     * 发送终端resize消息
     * @param {number} rows - 行数
     * @param {number} cols - 列数
     */
    sendResize(rows, cols) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
        if (rows <= 0 || cols <= 0) return;

        const msg = {
            type: 'resize',
            rows: rows,
            cols: cols
        };
        this.ws.send(JSON.stringify(msg));
    },

    /**
     * 自动重连（指数退避）
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
        }, 30000); // 每30秒发送一次心跳
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
     * @param {string} status - 'connected' | 'connecting' | 'disconnected' | 'failed'
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
     * @param {string} name - 会话名称
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
     * @param {string} method - HTTP方法
     * @param {string} path - API路径
     * @param {object} body - 请求体（可选）
     * @returns {Promise<object>} 响应数据
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
                // Token无效，要求重新输入
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
     * @param {string} message - 通知消息
     * @param {string} type - 'success' | 'error' | ''
     */
    showToast(message, type) {
        // 移除已有toast
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

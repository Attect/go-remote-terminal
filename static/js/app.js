/**
 * app.js - 主控制器模块
 * 负责全局状态管理、WebSocket连接、消息路由、模块协调
 *
 * 协议: v1 混合协议
 * - Binary Frame: input/output
 * - JSON Text Frame: auth/resize/ping/pong/session_info/conn_list/focus_change/error
 */
const App = {
    ws: null,
    token: null,
    currentSessionId: null,
    currentSessionName: '',
    reconnectAttempts: 0,
    maxReconnectAttempts: 3,
    reconnectTimer: null,
    manualClose: false,
    pingInterval: null,
    _initialized: false,
    _tokenSubmitBound: false,
    _connecting: false,
    _readOnly: false,       // 当前连接是否为只读
    _focused: false,        // 当前连接是否拥有输入焦点
    _bc: null,              // BroadcastChannel
    _quickCmds: [],         // 快捷命令列表

    init() {
        // 初始化 BroadcastChannel（多标签页自感知）
        if (typeof BroadcastChannel !== 'undefined') {
            this._bc = new BroadcastChannel('go-remote-terminal');
            this._bc.onmessage = (ev) => {
                if (ev.data === 'sessions-changed') {
                    Sidebar.refreshSessions();
                }
            };
        }

        window.addEventListener('beforeunload', () => {
            this.manualClose = true;
            if (this.ws) {
                this.ws.close();
            }
        });

        // 加载快捷命令
        this._loadQuickCmds();

        const savedToken = localStorage.getItem('terminal_token');
        if (savedToken) {
            this.token = savedToken;
            document.getElementById('token-modal').style.display = 'none';
            this.validateTokenAndConnect();
        } else {
            this.showTokenModal();
        }
    },

    broadcastSessionsChanged() {
        if (this._bc) {
            this._bc.postMessage('sessions-changed');
        }
    },

    async validateTokenAndConnect() {
        try {
            const data = await this.apiRequest('GET', '/api/sessions');
            document.getElementById('app').style.display = 'flex';
            document.getElementById('token-modal').style.display = 'none';

            if (!this._initialized) {
                this._initialized = true;
                TermMgr.init(document.getElementById('xterm-terminal'));
                Keyboard.init();
                Sidebar.init();
                this.bindToolbarEvents();
                this.bindFocusEvents();
                this.bindQuickCmdEvents();
            }

            if (data.data && data.data.length > 0) {
                const session = data.data[0];
                this.currentSessionId = session.id;
                this.currentSessionName = session.name;
                this.connect(session.id);
            } else {
                this.connect();
            }
        } catch (e) {
            console.error('[App] token validation failed:', e);
            localStorage.removeItem('terminal_token');
            this.token = null;
            document.getElementById('app').style.display = 'none';
            this.showTokenModal();
        }
    },

    bindToolbarEvents() {
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
        document.getElementById('btn-focus').addEventListener('click', () => {
            if (this._focused) {
                this.releaseFocus();
            } else {
                this.takeFocus();
            }
        });
        document.getElementById('btn-quick-cmd').addEventListener('click', () => {
            this.toggleQuickCmdPanel();
        });

        // 字体大小调整
        const btnFontDec = document.getElementById('btn-font-dec');
        const btnFontInc = document.getElementById('btn-font-inc');
        if (btnFontDec) {
            btnFontDec.addEventListener('click', () => {
                TermMgr.adjustFontSize(-1);
            });
        }
        if (btnFontInc) {
            btnFontInc.addEventListener('click', () => {
                TermMgr.adjustFontSize(1);
            });
        }

        // 滚动到底部
        const btnScrollBottom = document.getElementById('btn-scroll-bottom');
        if (btnScrollBottom) {
            btnScrollBottom.addEventListener('click', () => {
                TermMgr.scrollToBottom();
            });
        }

        // 功能键面板
        const btnFuncKeys = document.getElementById('btn-func-keys');
        if (btnFuncKeys) {
            btnFuncKeys.addEventListener('click', () => {
                Keyboard.toggleFuncKeys();
            });
        }

        // 鼠标模式
        const btnMouseMode = document.getElementById('btn-mouse-mode');
        if (btnMouseMode) {
            btnMouseMode.addEventListener('click', () => {
                Keyboard.toggleMouseMode();
            });
        }
    },

    bindFocusEvents() {
        document.getElementById('btn-take-focus').addEventListener('click', () => {
            this.takeFocus();
        });
    },

    showTokenModal() {
        const modal = document.getElementById('token-modal');
        modal.style.display = 'flex';
        modal.style.visibility = 'visible';
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
        document.getElementById('token-input').focus();
    },

    _submitToken() {
        const tokenInput = document.getElementById('token-input');
        const token = tokenInput ? tokenInput.value.trim() : '';
        if (!token) {
            const errEl = document.getElementById('token-error');
            if (errEl) { errEl.textContent = 'Token不能为空'; errEl.style.display = 'block'; }
            return;
        }
        if (token.length < 8) {
            const errEl = document.getElementById('token-error');
            if (errEl) { errEl.textContent = 'Token长度不能少于8个字符'; errEl.style.display = 'block'; }
            return;
        }
        this.token = token;
        localStorage.setItem('terminal_token', token);
        const tokenModal = document.getElementById('token-modal');
        if (tokenModal) {
            tokenModal.style.visibility = 'hidden';
            tokenModal.style.display = 'none';
        }
        this.validateTokenAndConnect();
    },

    connect(sessionId) {
        if (this._connecting) {
            console.log('[App] already connecting, skip');
            return;
        }
        if (this.ws) {
            try {
                this.manualClose = true;
                this.ws.onclose = null;
                this.ws.onerror = null;
                this.ws.close();
            } catch (e) {}
            this.ws = null;
        }

        this._connecting = true;
        this.manualClose = false;
        this.updateConnectionStatus('connecting');

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const host = window.location.host;
        const url = `${protocol}//${host}/ws`;

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
            // 确保终端尺寸与容器匹配后再发送认证
            TermMgr.fit();
            const rows = TermMgr.getRows();
            const cols = TermMgr.getCols();
            // 发送认证消息（附带终端尺寸，服务端以此作为初始PTY尺寸）
            const authMsg = {
                v: 1,
                type: 'auth',
                payload: {
                    token: this.token,
                    session_id: sessionId || '',
                    rows: rows,
                    cols: cols
                }
            };
            this.ws.send(JSON.stringify(authMsg));
        };

        this.ws.onmessage = (event) => {
            if (event.data instanceof Blob) {
                event.data.arrayBuffer().then(buffer => {
                    this.handleBinary(new Uint8Array(buffer));
                });
                return;
            }
            if (event.data instanceof ArrayBuffer) {
                this.handleBinary(new Uint8Array(event.data));
                return;
            }
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
            this._updateFocusUI();
            this._updateSharingBanner([]);

            if (!this.manualClose && this.reconnectAttempts < this.maxReconnectAttempts) {
                this.scheduleReconnect();
            } else if (this.reconnectAttempts >= this.maxReconnectAttempts) {
                this.showToast('连接已断开，请刷新页面重新连接', 'error');
            }
        };

        this.ws.onerror = (event) => {
            this._connecting = false;
            console.error('[App] WebSocket error:', event);
            this.updateConnectionStatus('disconnected');
        };
    },

    handleBinary(data) {
        if (data.length < 1) return;
        const msgType = data[0];
        const payload = data.slice(1);
        if (msgType === 0x02) {
            TermMgr.write(payload);
        }
    },

    handleMessage(msg) {
        if (!msg || !msg.type) return;
        switch (msg.type) {
            case 'output':
                this.handleOutput(msg);
                break;
            case 'session_info':
                this.handleSessionInfo(msg);
                break;
            case 'conn_list':
                this.handleConnList(msg);
                break;
            case 'focus_change':
                this.handleFocusChange(msg);
                break;
            case 'pty_resize':
                this.handlePtyResize(msg);
                break;
            case 'error':
                this.handleError(msg);
                break;
            case 'pong':
                break;
            default:
                console.log('[App] unknown message type:', msg.type);
        }
    },

    handleOutput(msg) {
        if (msg.payload && msg.payload.data) {
            try {
                const binaryStr = atob(msg.payload.data);
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

    handleSessionInfo(msg) {
        const p = msg.payload || {};
        if (p.id) {
            this.currentSessionId = p.id;
            this.currentSessionName = p.name || '';
            this._readOnly = p.read_only || false;
            this._focused = p.focused || false;
            this.updateTitle(this.currentSessionName);
            Sidebar.refreshSessions();
            this._updateFocusUI();
            if (p.conns) {
                this._updateSharingBanner(p.conns);
            }
        }
        // 服务端AddConn时已使用客户端初始尺寸，这里只需fit到容器实际大小
        // 并同步给服务端，由服务端统一计算最小PTY尺寸后广播
        TermMgr.fit();
        this.sendResize(TermMgr.getRows(), TermMgr.getCols());

        this._connecting = false;
        this.reconnectAttempts = 0;
        this.updateConnectionStatus('connected');
        this.startPing();
    },

    handleConnList(msg) {
        const p = msg.payload || {};
        if (p.conns) {
            this._updateSharingBanner(p.conns);
        }
    },

    handleFocusChange(msg) {
        const p = msg.payload || {};
        // 焦点变更后刷新 session_info 以更新自己的 focused 状态
        // 服务端会在焦点变更后广播新的 session_info
    },

    handlePtyResize(msg) {
        const p = msg.payload || {};
        if (p.rows > 0 && p.cols > 0) {
            TermMgr.resizeToPtySize(p.rows, p.cols);
        }
    },

    handleError(msg) {
        const p = msg.payload || {};
        console.error('[App] server error:', p.code, p.message);

        if (p.code === 'SESSION_EXITED') {
            this.showToast('Shell进程已退出', 'error');
            Sidebar.refreshSessions();
        } else if (p.code === 'SESSION_CLOSED') {
            this.showToast('会话已被其他用户关闭', 'error');
            this.currentSessionId = null;
            this._closeWS();
            this.reconnectAttempts = 0;
            setTimeout(() => this.validateTokenAndConnect(), 800);
        } else if (p.code === 'SESSION_NOT_FOUND' || p.code === 'SESSION_EXPIRED') {
            this.showToast('会话不存在或已过期，将创建新会话', 'error');
            this.currentSessionId = null;
            this._closeWS();
            this.reconnectAttempts = 0;
            setTimeout(() => this.connect(), 1000);
        } else if (p.code === 'SESSION_CREATE_FAILED') {
            this.showToast('会话创建失败: ' + (p.message || ''), 'error');
        } else if (p.code === 'READ_ONLY') {
            this.showToast('只读连接: ' + (p.message || ''), 'error');
        } else if (p.code === 'NO_FOCUS') {
            this.showToast('无输入焦点: ' + (p.message || ''), 'error');
        } else if (p.code === 'RATE_LIMITED') {
            this.showToast('速率超限: ' + (p.message || ''), 'error');
        } else {
            this.showToast('错误: ' + (p.message || p.code || '未知错误'), 'error');
        }
    },

    _closeWS() {
        if (this.ws) {
            this.manualClose = true;
            this.ws.onclose = null;
            this.ws.onerror = null;
            try { this.ws.close(); } catch (e) {}
            this.ws = null;
        }
    },

    /**
     * 复制终端选中内容到剪贴板
     */
    async copySelection() {
        try {
            const text = TermMgr.getSelection();
            if (!text) {
                this.showToast('没有选中的内容', 'error');
                return;
            }
            await navigator.clipboard.writeText(text);
            this.showToast('已复制到剪贴板', 'success');
        } catch (e) {
            console.error('[App] copy failed:', e);
            this.showToast('复制失败', 'error');
        }
    },

    /**
     * 从剪贴板粘贴到终端
     */
    async pasteFromClipboard() {
        try {
            const text = await navigator.clipboard.readText();
            if (text) {
                this.sendInput(text);
            }
        } catch (e) {
            console.error('[App] paste failed:', e);
            this.showToast('无法读取剪贴板', 'error');
        }
    },

    sendInput(data) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
        if (this._readOnly) return;

        // 如果没有焦点，但发送的是紧急信号（Ctrl+C, Ctrl+Z, Ctrl+D, Ctrl+\），
        // 自动申请焦点后继续发送。WebSocket 消息按顺序处理，take_focus 会先被执行。
        if (!this._focused) {
            const urgentSignals = ['\x03', '\x1a', '\x04', '\x1c'];
            if (urgentSignals.includes(data)) {
                this.takeFocus();
                // 继续发送输入，后端会按顺序处理 take_focus 然后 input
            } else {
                return;
            }
        }

        try {
            const encoder = new TextEncoder();
            const bytes = encoder.encode(data);
            const frame = new Uint8Array(1 + bytes.length);
            frame[0] = 0x01;
            frame.set(bytes, 1);
            this.ws.send(frame);
        } catch (e) {
            console.error('[App] failed to send input:', e);
        }
    },

    sendResize(rows, cols) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
        if (rows <= 0 || cols <= 0) return;
        this.ws.send(JSON.stringify({ v: 1, type: 'resize', payload: { rows, cols } }));
    },

    takeFocus() {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
        this.ws.send(JSON.stringify({ v: 1, type: 'take_focus' }));
    },

    releaseFocus() {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
        this.ws.send(JSON.stringify({ v: 1, type: 'release_focus' }));
    },

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

    startPing() {
        this.stopPing();
        this.pingInterval = setInterval(() => {
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                this.ws.send(JSON.stringify({ v: 1, type: 'ping' }));
            }
        }, 30000);
    },

    stopPing() {
        if (this.pingInterval) {
            clearInterval(this.pingInterval);
            this.pingInterval = null;
        }
    },

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

    updateTitle(name) {
        this.currentSessionName = name || '';
        const titleEl = document.getElementById('toolbar-title');
        if (titleEl) {
            titleEl.textContent = name ? `终端 - ${name}` : 'Web Terminal';
        }
    },

    _updateFocusUI() {
        const focusBtn = document.getElementById('btn-focus');
        const focusHint = document.getElementById('focus-hint');
        if (!focusBtn || !focusHint) return;

        if (this._readOnly) {
            focusBtn.style.display = 'none';
            focusHint.style.display = 'none';
            return;
        }

        if (this._focused) {
            focusBtn.style.display = 'flex';
            focusBtn.title = '释放焦点';
            focusBtn.style.color = 'var(--success)';
            focusHint.style.display = 'none';
        } else {
            focusBtn.style.display = 'flex';
            focusBtn.title = '申请焦点';
            focusBtn.style.color = 'var(--text-secondary)';
            focusHint.style.display = 'flex';
        }
    },

    _updateSharingBanner(conns) {
        const banner = document.getElementById('sharing-banner');
        const textEl = document.getElementById('sharing-text');
        const namesEl = document.getElementById('sharing-names');
        if (!banner || !textEl || !namesEl) return;

        if (!conns || conns.length === 0) {
            banner.style.display = 'none';
            return;
        }

        banner.style.display = 'flex';
        textEl.textContent = `${conns.length} 人正在共享`;
        namesEl.innerHTML = conns.map(c =>
            `<span class="sharing-name" style="color:${c.color}">${c.name}${c.focus ? ' ●' : ''}</span>`
        ).join('');
    },

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
    },

    // ==================== 快捷命令 ====================

    _loadQuickCmds() {
        try {
            const saved = localStorage.getItem('terminal_quick_cmds');
            this._quickCmds = saved ? JSON.parse(saved) : ['ls -la', 'pwd', 'clear'];
        } catch (e) {
            this._quickCmds = ['ls -la', 'pwd', 'clear'];
        }
    },

    _saveQuickCmds() {
        localStorage.setItem('terminal_quick_cmds', JSON.stringify(this._quickCmds));
    },

    toggleQuickCmdPanel() {
        const panel = document.getElementById('quick-cmd-panel');
        if (!panel) return;
        if (panel.style.display === 'none') {
            panel.style.display = 'flex';
            this._renderQuickCmds();
            document.getElementById('quick-cmd-input').focus();
        } else {
            panel.style.display = 'none';
        }
    },

    bindQuickCmdEvents() {
        document.getElementById('btn-quick-cmd-close').addEventListener('click', () => {
            document.getElementById('quick-cmd-panel').style.display = 'none';
        });
        document.getElementById('btn-quick-cmd-add').addEventListener('click', () => {
            this._addQuickCmd();
        });
        document.getElementById('quick-cmd-input').addEventListener('keydown', (e) => {
            if (e.key === 'Enter') {
                this._addQuickCmd();
            }
        });
    },

    _renderQuickCmds() {
        const list = document.getElementById('quick-cmd-list');
        if (!list) return;
        list.innerHTML = '';
        this._quickCmds.forEach((cmd, idx) => {
            const item = document.createElement('div');
            item.className = 'quick-cmd-item';
            item.innerHTML = `<span class="quick-cmd-text">${this._escapeHtml(cmd)}</span>`;
            const actions = document.createElement('div');
            actions.className = 'quick-cmd-actions';

            const sendBtn = document.createElement('button');
            sendBtn.className = 'quick-cmd-btn';
            sendBtn.textContent = '发送';
            sendBtn.addEventListener('click', () => {
                this.sendInput(cmd + '\r');
            });
            actions.appendChild(sendBtn);

            const delBtn = document.createElement('button');
            delBtn.className = 'quick-cmd-btn del';
            delBtn.textContent = '删除';
            delBtn.addEventListener('click', () => {
                this._quickCmds.splice(idx, 1);
                this._saveQuickCmds();
                this._renderQuickCmds();
            });
            actions.appendChild(delBtn);

            item.appendChild(actions);
            list.appendChild(item);
        });
    },

    _addQuickCmd() {
        const input = document.getElementById('quick-cmd-input');
        const cmd = input.value.trim();
        if (!cmd) return;
        this._quickCmds.push(cmd);
        this._saveQuickCmds();
        input.value = '';
        this._renderQuickCmds();
    },

    _escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
};

document.addEventListener('DOMContentLoaded', () => {
    App.init();
});

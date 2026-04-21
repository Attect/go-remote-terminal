/**
 * sidebar.js - 会话侧边栏模块
 * 负责会话列表渲染、CRUD操作、事件绑定
 */
const Sidebar = {
    sessions: [],           // 会话列表数据
    renamingSessionId: null, // 正在重命名的会话ID
    closingSessionId: null,  // 待关闭的会话ID
    collapsed: false,

    /**
     * 初始化侧边栏
     */
    init() {
        // 移动端默认折叠侧边栏
        if (Keyboard.isMobileDevice) {
            this.collapsed = true;
            document.getElementById('sidebar').classList.add('collapsed');
        }

        // 绑定新建会话按钮
        document.getElementById('btn-new-session').addEventListener('click', () => {
            this.createNewSession();
        });

        // 绑定侧边栏切换
        document.getElementById('btn-sidebar-toggle').addEventListener('click', () => {
            this.toggle();
        });

        // 移动端遮罩点击关闭侧边栏
        document.getElementById('sidebar-overlay').addEventListener('click', () => {
            if (!this.collapsed) {
                this.toggle();
            }
        });

        // 绑定重命名弹窗事件
        document.getElementById('rename-cancel').addEventListener('click', () => {
            this.hideRenameModal();
        });

        document.getElementById('rename-confirm').addEventListener('click', () => {
            this.confirmRename();
        });

        document.getElementById('rename-input').addEventListener('keydown', (e) => {
            if (e.key === 'Enter') {
                this.confirmRename();
            } else if (e.key === 'Escape') {
                this.hideRenameModal();
            }
        });

        // 绑定确认关闭弹窗事件
        document.getElementById('confirm-cancel').addEventListener('click', () => {
            this.hideConfirmModal();
        });

        document.getElementById('confirm-ok').addEventListener('click', () => {
            this.confirmClose();
        });

        // 加载会话列表
        this.refreshSessions();
    },

    /**
     * 切换侧边栏折叠/展开
     */
    toggle() {
        this.collapsed = !this.collapsed;
        const sidebar = document.getElementById('sidebar');
        const overlay = document.getElementById('sidebar-overlay');

        if (this.collapsed) {
            sidebar.classList.add('collapsed');
            overlay.classList.remove('visible');
        } else {
            sidebar.classList.remove('collapsed');
            if (Keyboard.isMobileDevice) {
                overlay.classList.add('visible');
            }
        }

        // 终端需要重新适配尺寸
        setTimeout(() => {
            TermMgr.fit();
        }, 250);
    },

    /**
     * 从API刷新会话列表
     */
    async refreshSessions() {
        try {
            const data = await App.apiRequest('GET', '/api/sessions');
            if (data && data.data) {
                this.sessions = data.data;
                this.render();
            }
        } catch (e) {
            console.error('[Sidebar] refresh sessions failed:', e);
        }
    },

    /**
     * 渲染会话列表
     */
    render() {
        const listEl = document.getElementById('session-list');
        listEl.innerHTML = '';

        if (this.sessions.length === 0) {
            const emptyItem = document.createElement('li');
            emptyItem.className = 'session-item';
            emptyItem.innerHTML = '<span style="color:var(--text-muted);font-size:13px;">暂无活跃会话</span>';
            listEl.appendChild(emptyItem);
            return;
        }

        this.sessions.forEach(session => {
            const item = document.createElement('li');
            item.className = 'session-item' + (session.id === App.currentSessionId ? ' active' : '');
            item.dataset.id = session.id;

            // 状态指示点
            const dot = document.createElement('span');
            dot.className = 'session-status-dot ' + (session.status === 'active' ? 'active' : 'exited');
            item.appendChild(dot);

            // 会话名称（双击可重命名）
            const nameSpan = document.createElement('span');
            nameSpan.className = 'session-name';
            nameSpan.textContent = session.name;
            nameSpan.addEventListener('dblclick', (e) => {
                e.stopPropagation();
                this.showRenameModal(session.id, session.name);
            });
            item.appendChild(nameSpan);

            // 操作按钮区
            const actions = document.createElement('div');
            actions.className = 'session-actions';

            // 重命名按钮
            const renameBtn = document.createElement('button');
            renameBtn.className = 'session-action-btn';
            renameBtn.title = '重命名';
            renameBtn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>';
            renameBtn.addEventListener('click', (e) => {
                e.stopPropagation();
                this.showRenameModal(session.id, session.name);
            });
            actions.appendChild(renameBtn);

            // 关闭按钮
            const closeBtn = document.createElement('button');
            closeBtn.className = 'session-action-btn close-btn';
            closeBtn.title = '关闭会话';
            closeBtn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>';
            closeBtn.addEventListener('click', (e) => {
                e.stopPropagation();
                this.showConfirmModal(session.id, session.name);
            });
            actions.appendChild(closeBtn);

            item.appendChild(actions);

            // 点击切换会话
            item.addEventListener('click', () => {
                this.switchSession(session.id);
            });

            listEl.appendChild(item);
        });
    },

    /**
     * 切换到指定会话
     * @param {string} sessionId - 目标会话ID
     */
    switchSession(sessionId) {
        if (sessionId === App.currentSessionId) return;

        // 清空终端内容，准备加载新会话的输出
        TermMgr.clear();

        // 断开当前WebSocket连接（不关闭会话，仅解除绑定）
        if (App.ws) {
            App.manualClose = true;
            App.ws.close();
            App.ws = null;
        }

        // 连接到目标会话（带sessionId，服务端会加入已有会话并回显缓冲区）
        App.currentSessionId = sessionId;
        App.connect(sessionId);

        // 移动端自动收起侧边栏
        if (Keyboard.isMobileDevice && !this.collapsed) {
            this.toggle();
        }
    },

    /**
     * 创建新会话
     */
    async createNewSession() {
        try {
            const data = await App.apiRequest('POST', '/api/sessions', {});
            if (data && data.data) {
                // 清空终端，准备加载新会话
                TermMgr.clear();

                // 断开当前连接
                if (App.ws) {
                    App.manualClose = true;
                    App.ws.close();
                    App.ws = null;
                }
                // 连接到新会话（带sessionId，服务端会加入该会话并回显）
                App.currentSessionId = data.data.id;
                App.connect(data.data.id);
                App.showToast('新会话已创建', 'success');
            }
        } catch (e) {
            App.showToast('创建会话失败: ' + e.message, 'error');
        }
    },

    /**
     * 显示重命名弹窗
     */
    showRenameModal(sessionId, currentName) {
        this.renamingSessionId = sessionId;
        const input = document.getElementById('rename-input');
        input.value = currentName;
        document.getElementById('rename-modal').style.display = 'flex';
        setTimeout(() => input.select(), 50);
    },

    /**
     * 隐藏重命名弹窗
     */
    hideRenameModal() {
        this.renamingSessionId = null;
        document.getElementById('rename-modal').style.display = 'none';
    },

    /**
     * 确认重命名
     */
    async confirmRename() {
        const newName = document.getElementById('rename-input').value.trim();
        if (!newName) {
            App.showToast('会话名称不能为空', 'error');
            return;
        }

        try {
            await App.apiRequest('PUT', `/api/sessions/${this.renamingSessionId}/rename`, { name: newName });
            this.hideRenameModal();
            this.refreshSessions();
            App.showToast('重命名成功', 'success');

            // 更新标题栏
            if (this.renamingSessionId === App.currentSessionId) {
                App.updateTitle(newName);
            }
        } catch (e) {
            App.showToast('重命名失败: ' + e.message, 'error');
        }
    },

    /**
     * 显示确认关闭弹窗
     */
    showConfirmModal(sessionId, sessionName) {
        this.closingSessionId = sessionId;
        document.getElementById('confirm-message').textContent =
            `确定要关闭会话「${sessionName}」吗？关闭后进程将被终止。`;
        document.getElementById('confirm-modal').style.display = 'flex';
    },

    /**
     * 隐藏确认关闭弹窗
     */
    hideConfirmModal() {
        this.closingSessionId = null;
        document.getElementById('confirm-modal').style.display = 'none';
    },

    /**
     * 确认关闭会话
     */
    async confirmClose() {
        const sessionId = this.closingSessionId;

        try {
            await App.apiRequest('DELETE', `/api/sessions/${sessionId}`);

            // 如果关闭的是当前会话
            if (sessionId === App.currentSessionId) {
                if (App.ws) {
                    App.manualClose = true;
                    App.ws.close();
                    App.ws = null;
                }
                App.currentSessionId = null;
                TermMgr.clear();

                // 尝试切换到其他活跃会话
                const remaining = this.sessions.filter(s => s.id !== sessionId);
                if (remaining.length > 0) {
                    App.currentSessionId = remaining[0].id;
                    App.connect(remaining[0].id);
                } else {
                    // 无活跃会话，自动创建新的
                    this.createNewSession();
                }
            }

            this.hideConfirmModal();
            this.refreshSessions();
            App.showToast('会话已关闭', 'success');
        } catch (e) {
            App.showToast('关闭会话失败: ' + e.message, 'error');
        }
    }
};

/**
 * terminal.js - xterm.js终端封装模块
 * 负责终端初始化、数据绑定、resize处理
 *
 * 注意: xterm.js CDN加载后 window.Terminal 是xterm的Terminal类
 * 我们将模块命名为 TermMgr 以避免命名冲突
 */

// 保存xterm.js Terminal类的引用（在CDN加载后、模块定义前）
const XtermTerminal = window.Terminal;

const TermMgr = {
    term: null,        // xterm.js实例
    fitAddon: null,    // fit插件实例

    // resize相关状态
    _lastResizeKey: '',       // 上次resize的尺寸key，避免重复发送
    _resizeTimer: null,       // debounce定时器
    _serverResizeInProgress: false, // 服务端resize进行中标记，防止反馈循环
    _resizeHandler: null,     // window resize事件处理器
    _resizeObserver: null,    // ResizeObserver实例
    searchAddon: null,        // 搜索插件实例

    // 字体大小
    _fontSize: 14,
    _minFontSize: 8,
    _maxFontSize: 20,
    _fontSizeKey: 'terminal_font_size',

    /**
     * 初始化终端
     * @param {HTMLElement} container - 终端容器DOM元素
     */
    init(container) {
        if (this.term) {
            this.dispose();
        }

        // 加载保存的字体大小（移动端默认更小）
        this._loadFontSize();

        // 创建xterm.js实例
        this.term = new XtermTerminal({
            cursorBlink: true,
            cursorStyle: 'bar',
            fontSize: this._fontSize,
            fontFamily: '"Cascadia Code", "Fira Code", "JetBrains Mono", "Source Code Pro", Consolas, monospace',
            theme: {
                background: '#1a1a2e',
                foreground: '#e0e0e0',
                cursor: '#00d4ff',
                cursorAccent: '#1a1a2e',
                selectionBackground: 'rgba(0, 212, 255, 0.3)',
                black: '#1a1a2e',
                red: '#e94560',
                green: '#4caf50',
                yellow: '#ff9800',
                blue: '#2196f3',
                magenta: '#e040fb',
                cyan: '#00d4ff',
                white: '#e0e0e0',
                brightBlack: '#606080',
                brightRed: '#ff6b6b',
                brightGreen: '#69f0ae',
                brightYellow: '#ffd740',
                brightBlue: '#448aff',
                brightMagenta: '#ea80fc',
                brightCyan: '#18ffff',
                brightWhite: '#ffffff'
            },
            allowTransparency: false,
            scrollback: 5000,
            convertEol: false,
            lineHeight: 1.15,
        });

        // 创建fit插件
        this.fitAddon = new FitAddon.FitAddon();
        this.term.loadAddon(this.fitAddon);

        // 创建搜索插件
        if (typeof SearchAddon !== 'undefined') {
            this.searchAddon = new SearchAddon.SearchAddon();
            this.term.loadAddon(this.searchAddon);
        }

        // 绑定自定义快捷键
        this.term.attachCustomKeyEventHandler((e) => {
            // Ctrl+Shift+F 搜索
            if (e.ctrlKey && e.shiftKey && e.code === 'KeyF') {
                e.preventDefault();
                if (this.searchAddon) {
                    this.searchAddon.findNext('');
                }
                return false;
            }
            // Ctrl+Shift+C 复制选中内容
            if (e.ctrlKey && e.shiftKey && e.code === 'KeyC') {
                e.preventDefault();
                App.copySelection();
                return false;
            }
            // Ctrl+Shift+M 重置鼠标跟踪
            if (e.ctrlKey && e.shiftKey && e.code === 'KeyM') {
                e.preventDefault();
                this.resetMouseTracking();
                App.showToast('鼠标跟踪已重置', 'success');
                return false;
            }
            // Ctrl+C 始终发送 SIGINT (\x03)，不依赖 xterm.js 的默认行为
            // 默认行为在有选中文本时会执行浏览器复制而非发送 \x03
            if (e.ctrlKey && !e.shiftKey && e.code === 'KeyC') {
                e.preventDefault();
                App.sendInput('\x03');
                return false;
            }
            // 允许 xterm.js 默认的选中复制和右键菜单
            return true;
        });

        // 鼠标滚轮支持（xterm.js默认处理，额外确保viewport可滚动）
        container.addEventListener('wheel', (e) => {
            if (this.term) {
                // xterm.js内部已处理滚动，这里不做拦截
                e.stopPropagation();
            }
        }, { passive: true });

        // 拖拽文件/目录支持：拖入时在当前光标位置插入带引号的完整路径
        this._dragCounter = 0; // 防止子元素触发导致闪烁
        container.addEventListener('dragenter', (e) => {
            e.preventDefault();
            this._dragCounter++;
            container.classList.add('drag-over');
        });
        container.addEventListener('dragleave', (e) => {
            e.preventDefault();
            this._dragCounter--;
            if (this._dragCounter <= 0) {
                this._dragCounter = 0;
                container.classList.remove('drag-over');
            }
        });
        container.addEventListener('dragover', (e) => {
            e.preventDefault(); // 必须阻止默认行为才能触发 drop
            e.dataTransfer.dropEffect = 'copy';
        });
        container.addEventListener('drop', (e) => {
            e.preventDefault();
            this._dragCounter = 0;
            container.classList.remove('drag-over');
            this._handleFileDrop(e);
        });

        // 打开终端
        this.term.open(container);

        // 初始适配（延迟确保容器已有尺寸）
        requestAnimationFrame(() => {
            this.fit();
        });

        // 绑定输入事件
        this.term.onData((data) => {
            if (App.ws && App.ws.readyState === WebSocket.OPEN) {
                App.sendInput(data);
            }
        });

        // 监听resize事件（仅向服务端发送）
        this._lastResizeKey = '';
        this.term.onResize(({ cols, rows }) => {
            // 如果是服务端触发的resize，不回发给服务端（防止反馈循环）
            if (this._serverResizeInProgress) return;

            const key = `${cols}x${rows}`;
            if (key !== this._lastResizeKey) {
                this._lastResizeKey = key;
                if (App.ws && App.ws.readyState === WebSocket.OPEN) {
                    App.sendResize(rows, cols);
                }
            }
        });

        // 窗口resize时自动适配（带debounce）
        this._resizeHandler = () => {
            this.debouncedFit();
        };
        window.addEventListener('resize', this._resizeHandler);

        // 监听容器尺寸变化(侧边栏折叠等)，带debounce
        if (typeof ResizeObserver !== 'undefined') {
            this._resizeObserver = new ResizeObserver(() => {
                this.debouncedFit();
            });
            // 观察terminal-wrapper（终端的直接父容器）
            const wrapper = container.parentElement;
            if (wrapper) {
                this._resizeObserver.observe(wrapper);
            }
        }

        // 监听页面可见性变化，切回时强制刷新渲染（修复标签页切换后终端破碎）
        this._visibilityHandler = () => {
            if (document.visibilityState === 'visible' && this.term) {
                // 强制刷新整个终端视口
                this.term.refresh(0, this.term.rows - 1);
            }
        };
        document.addEventListener('visibilitychange', this._visibilityHandler);

        return this.term;
    },

    /**
     * 带debounce的fit调用
     * 避免频繁resize导致过多计算和通信
     */
    debouncedFit() {
        if (this._resizeTimer) {
            clearTimeout(this._resizeTimer);
        }
        this._resizeTimer = setTimeout(() => {
            this.fit();
        }, 100);
    },

    /**
     * 适配终端尺寸
     */
    fit() {
        if (!this.term || !this.fitAddon) return;

        try {
            this.fitAddon.fit();
        } catch (e) {
            // ignore fit errors during transitions
        }
    },

    /**
     * 服务端通知PTY尺寸变更，调整xterm.js匹配
     * 多连接场景下，PTY尺寸取所有连接的最小值
     * @param {number} rows - PTY行数
     * @param {number} cols - PTY列数
     */
    resizeToPtySize(rows, cols) {
        if (!this.term) return;

        // 设置标记，防止onResize回发给服务端
        this._serverResizeInProgress = true;
        try {
            this.term.resize(cols, rows);
            this._lastResizeKey = `${cols}x${rows}`;
        } catch (e) {
            // ignore resize errors
        }
        this._serverResizeInProgress = false;
    },

    /**
     * 写入输出数据到终端
     * 支持string和Uint8Array两种类型：
     * - Uint8Array: 来自PTY的原始UTF-8字节，由xterm.js内部UTF-8解析器正确解码
     * - string: 直接写入（如重连回显等场景）
     * @param {string|Uint8Array} data - 输出数据
     */
    write(data) {
        if (this.term) {
            this.term.write(data);
        }
    },

    /**
     * 聚焦终端
     */
    focus() {
        if (this.term) {
            this.term.focus();
        }
    },

    /**
     * 清空终端
     */
    clear() {
        if (this.term) {
            this.term.clear();
        }
    },

    /**
     * 获取终端列数
     */
    getCols() {
        return this.term ? this.term.cols : 80;
    },

    /**
     * 获取终端行数
     */
    getRows() {
        return this.term ? this.term.rows : 24;
    },

    /**
     * 获取当前选中的文本内容
     */
    getSelection() {
        return this.term ? this.term.getSelection() : '';
    },

    /**
     * 销毁终端实例
     */
    dispose() {
        if (this._resizeTimer) {
            clearTimeout(this._resizeTimer);
            this._resizeTimer = null;
        }
        if (this._resizeHandler) {
            window.removeEventListener('resize', this._resizeHandler);
            this._resizeHandler = null;
        }
        if (this._resizeObserver) {
            this._resizeObserver.disconnect();
            this._resizeObserver = null;
        }
        if (this._visibilityHandler) {
            document.removeEventListener('visibilitychange', this._visibilityHandler);
            this._visibilityHandler = null;
        }
        if (this.term) {
            this.term.dispose();
            this.term = null;
            this.fitAddon = null;
            this.searchAddon = null;
        }
    },

    /**
     * 从localStorage加载字体大小
     */
    _loadFontSize() {
        try {
            const saved = localStorage.getItem(this._fontSizeKey);
            if (saved) {
                const size = parseInt(saved, 10);
                if (size >= this._minFontSize && size <= this._maxFontSize) {
                    this._fontSize = size;
                    return;
                }
            }
        } catch (e) {
            // ignore
        }
        // 移动端默认更小字体
        if (Keyboard.isMobileDevice) {
            this._fontSize = 11;
        } else {
            this._fontSize = 14;
        }
    },

    /**
     * 保存字体大小到localStorage
     */
    _saveFontSize() {
        try {
            localStorage.setItem(this._fontSizeKey, String(this._fontSize));
        } catch (e) {
            // ignore
        }
    },

    /**
     * 设置字体大小
     * @param {number} size - 字体大小
     */
    setFontSize(size) {
        size = Math.max(this._minFontSize, Math.min(this._maxFontSize, size));
        this._fontSize = size;
        if (this.term) {
            this.term.options.fontSize = size;
            this.fit();
        }
        this._saveFontSize();
    },

    /**
     * 调整字体大小
     * @param {number} delta - 变化量（可为负）
     */
    adjustFontSize(delta) {
        this.setFontSize(this._fontSize + delta);
    },

    // 鼠标跟踪状态（前端记录，用于手动切换）
    _mouseTrackingEnabled: false,

    /**
     * 开启鼠标跟踪模式
     */
    enableMouseTracking() {
        if (this.term) {
            // 开启所有事件跟踪(1003) + SGR扩展格式(1006)
            this.term.write('\x1b[?1003h\x1b[?1006h');
            this._mouseTrackingEnabled = true;
        }
    },

    /**
     * 重置/关闭鼠标跟踪模式
     * 当远程程序异常退出导致鼠标模式未关闭时，发送ANSI序列强制关闭
     */
    resetMouseTracking() {
        if (this.term) {
            // 关闭所有xterm鼠标跟踪模式：
            // 1000=普通鼠标报告, 1002=按钮事件跟踪, 1003=所有事件跟踪(含移动)
            // 1006=SGR扩展格式, 1015=URXVT格式
            this.term.write('\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1015l');
            this._mouseTrackingEnabled = false;
        }
    },

    /**
     * 切换鼠标跟踪模式
     */
    toggleMouseTracking() {
        if (this._mouseTrackingEnabled) {
            this.resetMouseTracking();
        } else {
            this.enableMouseTracking();
        }
        return this._mouseTrackingEnabled;
    },

    /**
     * 滚动到终端底部（最新消息）
     */
    scrollToBottom() {
        if (this.term) {
            this.term.scrollToBottom();
        }
    },

    /**
     * 处理文件/目录拖放事件
     * 尝试从多种数据源获取路径，并发送到终端
     */
    _handleFileDrop(e) {
        if (!this.term) return;

        let path = '';

        // 优先级1: text/uri-list（某些浏览器/操作系统会提供 file:// URI）
        const uriList = e.dataTransfer.getData('text/uri-list');
        if (uriList) {
            const uri = uriList.split('\n')[0].trim();
            if (uri.startsWith('file://')) {
                // file:///C:/Users/Attect/file.txt → C:/Users/Attect/file.txt
                // file:///home/attect/file.txt → /home/attect/file.txt
                path = decodeURIComponent(uri.slice(7));
                if (path.startsWith('/')) {
                    path = path.slice(1);
                }
                // Windows 路径将正斜杠统一为反斜杠更自然
                if (path.length > 1 && path[1] === ':') {
                    path = path.replace(/\//g, '\\');
                }
            }
        }

        // 优先级2: text/plain（浏览器扩展或某些文件管理器可能提供）
        if (!path) {
            path = e.dataTransfer.getData('text/plain').trim();
        }

        // 优先级3: files[0].name（回退到文件名）
        if (!path && e.dataTransfer.files.length > 0) {
            path = e.dataTransfer.files[0].name;
        }

        if (path) {
            // 如果路径包含空格或特殊字符，用引号包裹
            const needQuotes = /[\s'"\\$;&|<>(){}[\]*?~`]/.test(path);
            const escaped = path.replace(/"/g, '\\"');
            const quoted = needQuotes ? `"${escaped}"` : escaped;
            App.sendInput(quoted);
        }
    }
};

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

    /**
     * 初始化终端
     * @param {HTMLElement} container - 终端容器DOM元素
     */
    init(container) {
        if (this.term) {
            this.dispose();
        }

        // 创建xterm.js实例
        this.term = new XtermTerminal({
            cursorBlink: true,
            cursorStyle: 'bar',
            fontSize: 14,
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
            convertEol: true,
            lineHeight: 1.15,
        });

        // 创建fit插件
        this.fitAddon = new FitAddon.FitAddon();
        this.term.loadAddon(this.fitAddon);

        // 打开终端
        this.term.open(container);

        // 初始适配
        requestAnimationFrame(() => {
            this.fit();
        });

        // 绑定输入事件
        this.term.onData((data) => {
            if (App.ws && App.ws.readyState === WebSocket.OPEN) {
                App.sendInput(data);
            }
        });

        // 监听resize事件（仅向服务端发送，不在前端触发resize以避免循环）
        this._lastResizeKey = '';
        this.term.onResize(({ cols, rows }) => {
            const key = `${cols}x${rows}`;
            if (key !== this._lastResizeKey) {
                this._lastResizeKey = key;
                if (App.ws && App.ws.readyState === WebSocket.OPEN) {
                    App.sendResize(rows, cols);
                }
            }
        });

        // 窗口resize时自动适配
        this._resizeHandler = () => {
            this.fit();
        };
        window.addEventListener('resize', this._resizeHandler);

        // 监听容器尺寸变化(侧边栏折叠等)
        if (typeof ResizeObserver !== 'undefined') {
            this._resizeObserver = new ResizeObserver(() => {
                this.fit();
            });
            this._resizeObserver.observe(container.parentElement || container);
        }

        return this.term;
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
     * 写入输出数据到终端
     * @param {string} data - 输出数据（Base64解码后的原始字符串）
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
     * 销毁终端实例
     */
    dispose() {
        if (this._resizeHandler) {
            window.removeEventListener('resize', this._resizeHandler);
            this._resizeHandler = null;
        }
        if (this._resizeObserver) {
            this._resizeObserver.disconnect();
            this._resizeObserver = null;
        }
        if (this.term) {
            this.term.dispose();
            this.term = null;
            this.fitAddon = null;
        }
    }
};

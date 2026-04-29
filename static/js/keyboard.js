/**
 * keyboard.js - 移动端虚拟键盘模块
 * 负责移动端检测、虚拟键盘渲染、组合键状态管理
 */
const Keyboard = {
    visible: false,
    isMobileDevice: /(android|bb\d+|meego).+mobile|avantgo|bada\/|blackberry|blazer|compal|elaine|fennec|hiptop|iemobile|ip(hone|od)|iris|kindle|lge |maemo|midp|mmp|mobile.+firefox|netfront|opera m(ob|in)i|palm( os)?|phone|p(ixi|re)\/|plucker|pocket|psp|series(4|6)0|symbian|treo|up\.(browser|link)|vodafone|wap|windows ce|xda|xiino|android|ipad|playbook|silk/i.test(navigator.userAgent || navigator.vendor || window.opera),

    // 修饰键状态: 'Ctrl' | 'Alt' | 'Shift'
    activeModifiers: new Set(),

    // 长按锁定状态
    lockedModifiers: new Set(),

    // 长按检测定时器
    _longPressTimer: null,
    _longPressThreshold: 500, // 毫秒

    // 修饰键对应的xterm转义序列前缀
    modifierMap: {
        'Ctrl': '\x1b',  // Ctrl组合键通过字符编码计算
        'Alt': '\x1b',   // Alt组合键发送ESC前缀
    },

    // 普通按键对应的转义序列
    keySequenceMap: {
        'Esc': '\x1b',
        'Tab': '\t',
        'ArrowUp': '\x1b[A',
        'ArrowDown': '\x1b[B',
        'ArrowRight': '\x1b[C',
        'ArrowLeft': '\x1b[D',
        'Home': '\x1b[H',
        'End': '\x1b[F',
        'PageUp': '\x1b[5~',
        'PageDown': '\x1b[6~',
    },

    // 功能键面板状态
    funcKeysVisible: false,

    // 鼠标模式状态
    mouseMode: false,
    _mouseTouchStart: null,
    _mouseListeners: [],

    /**
     * 初始化虚拟键盘
     */
    init() {
        // 绑定虚拟按键事件
        this._bindKeys(document.querySelectorAll('.vk-key'));

        // 绑定功能键面板事件
        this._bindKeys(document.querySelectorAll('.fk-key'));

        // 移动端默认不显示虚拟键盘，由用户切换
    },

    /**
     * 绑定按键事件（支持长按锁定修饰键）
     * @param {NodeList} buttons - 按键按钮集合
     */
    _bindKeys(buttons) {
        buttons.forEach(btn => {
            const key = btn.dataset.key;
            if (!key) return;

            // 长按检测
            const startLongPress = (e) => {
                if (this._longPressTimer) clearTimeout(this._longPressTimer);
                this._longPressTimer = setTimeout(() => {
                    this.handleLongPress(key, btn);
                }, this._longPressThreshold);
            };

            const cancelLongPress = () => {
                if (this._longPressTimer) {
                    clearTimeout(this._longPressTimer);
                    this._longPressTimer = null;
                }
            };

            // 使用touchstart/click处理，防止iOS延迟
            btn.addEventListener('touchstart', (e) => {
                e.preventDefault();
                startLongPress(e);
                this.handleKey(key, btn);
            }, { passive: false });
            btn.addEventListener('touchend', cancelLongPress);
            btn.addEventListener('touchcancel', cancelLongPress);

            btn.addEventListener('mousedown', (e) => {
                e.preventDefault();
                startLongPress(e);
                this.handleKey(key, btn);
            });
            btn.addEventListener('mouseup', cancelLongPress);
            btn.addEventListener('mouseleave', cancelLongPress);
        });
    },

    /**
     * 处理长按事件（锁定修饰键）
     */
    handleLongPress(key, btn) {
        if (key === 'Ctrl' || key === 'Alt' || key === 'Shift') {
            if (this.lockedModifiers.has(key)) {
                this.lockedModifiers.delete(key);
                this.activeModifiers.delete(key);
            } else {
                this.lockedModifiers.add(key);
                this.activeModifiers.add(key);
            }
            this._longPressTimer = null;
            this.updateModifierUI();
        }
    },

    /**
     * 检测是否为移动端设备
     * @returns {boolean}
     */
    isMobile() {
        const ua = navigator.userAgent || navigator.vendor || window.opera;
        // 检测手机/平板
        return /(android|bb\d+|meego).+mobile|avantgo|bada\/|blackberry|blazer|compal|elaine|fennec|hiptop|iemobile|ip(hone|od)|iris|kindle|lge |maemo|midp|mmp|mobile.+firefox|netfront|opera m(ob|in)i|palm( os)?|phone|p(ixi|re)\/|plucker|pocket|psp|series(4|6)0|symbian|treo|up\.(browser|link)|vodafone|wap|windows ce|xda|xiino|android|ipad|playbook|silk/i.test(ua);
    },

    /**
     * 切换虚拟键盘显示/隐藏
     */
    toggle() {
        this.visible = !this.visible;
        const el = document.getElementById('virtual-keyboard');
        if (el) {
            el.style.display = this.visible ? 'flex' : 'none';
        }

        // 释放所有修饰键状态
        this.activeModifiers.clear();
        this.updateModifierUI();

        // 通知终端重新适配尺寸
        setTimeout(() => {
            TermMgr.fit();
        }, 50);
    },

    /**
     * 处理按键点击
     * @param {string} key - 按键标识
     * @param {HTMLElement} btn - 按钮DOM元素
     */
    handleKey(key, btn) {
        // 修饰键特殊处理
        if (key === 'Ctrl' || key === 'Alt' || key === 'Shift') {
            this.handleModifier(key);
            return;
        }

        // 粘贴按钮
        if (key === 'Paste') {
            App.pasteFromClipboard();
            return;
        }

        // 切换鼠标跟踪模式
        if (key === 'ResetMouse') {
            const enabled = TermMgr.toggleMouseTracking();
            App.showToast(enabled ? '鼠标跟踪已开启' : '鼠标跟踪已关闭', 'success');
            // 更新按钮视觉状态
            const btn = document.querySelector('.fk-key[data-key="ResetMouse"]');
            if (btn) {
                btn.classList.toggle('mouse-active', enabled);
            }
            return;
        }

        // 如果有激活的修饰键，发送组合键
        if (this.activeModifiers.size > 0) {
            this.sendCombination(key);
        } else {
            // 发送普通按键序列
            const sequence = this.keySequenceMap[key];
            if (sequence) {
                App.sendInput(sequence);
            }
        }
    },

    /**
     * 处理修饰键切换（点击切换，长按锁定）
     * @param {string} key - 'Ctrl' | 'Alt' | 'Shift'
     */
    handleModifier(key) {
        // 如果已锁定，点击解锁
        if (this.lockedModifiers.has(key)) {
            this.lockedModifiers.delete(key);
            this.activeModifiers.delete(key);
            this.updateModifierUI();
            return;
        }
        // 普通点击切换
        if (this.activeModifiers.has(key)) {
            this.activeModifiers.delete(key);
        } else {
            this.activeModifiers.add(key);
        }
        this.updateModifierUI();
    },

    /**
     * 检查是否有激活的修饰键（含锁定）
     */
    hasModifier(key) {
        return this.activeModifiers.has(key) || this.lockedModifiers.has(key);
    },

    /**
     * 发送组合键
     * @param {string} key - 被修饰的按键
     */
    sendCombination(key) {
        const hasCtrl = this.hasModifier('Ctrl');
        const hasAlt = this.hasModifier('Alt');
        const hasShift = this.hasModifier('Shift');

        let sequence = '';

        // 处理方向键等特殊键的组合
        if (this.keySequenceMap[key]) {
            const baseSeq = this.keySequenceMap[key];
            // 方向键的序列格式: ESC [ A
            // Ctrl+方向键: ESC [ 1;5 A
            // Alt+方向键: ESC [ 1;3 A
            if (baseSeq.startsWith('\x1b[')) {
                let modifierCode = 1;
                if (hasCtrl && hasAlt) {
                    modifierCode = 7; // Ctrl+Alt
                } else if (hasCtrl) {
                    modifierCode = 5; // Ctrl
                } else if (hasAlt) {
                    modifierCode = 3; // Alt
                }
                // PageUp/PageDown 格式: ESC [ 5 ~ -> ESC [ 5 ; modifier ~
                if (baseSeq === '\x1b[5~') {
                    sequence = `\x1b[5;${modifierCode}~`;
                } else if (baseSeq === '\x1b[6~') {
                    sequence = `\x1b[6;${modifierCode}~`;
                } else {
                    const dirChar = baseSeq.slice(-1); // A, B, C, D, H, F
                    sequence = `\x1b[1;${modifierCode}${dirChar}`;
                }
            } else {
                // Tab/Esc
                if (hasAlt) {
                    sequence = '\x1b' + baseSeq;
                }
            }
        } else {
            // 单字符键
            const charCode = key.charCodeAt(0);

            if (hasCtrl) {
                // Ctrl+A=1, Ctrl+B=2, ... Ctrl+Z=26
                // Ctrl+[=27, Ctrl+\=28, Ctrl+]=29
                if (charCode >= 65 && charCode <= 90) {
                    // 大写字母 A-Z: Ctrl+Key = key - 64
                    sequence = String.fromCharCode(charCode - 64);
                } else if (charCode >= 97 && charCode <= 122) {
                    // 小写字母 a-z
                    sequence = String.fromCharCode(charCode - 96);
                } else if (key === '[' || key === '{') {
                    sequence = '\x1b'; // ESC (27)
                } else if (key === ']' || key === '}') {
                    sequence = '\x1d'; // GS (29)
                } else if (key === '\\' || key === '|') {
                    sequence = '\x1c'; // FS (28)
                } else if (key === '2' || key === '@') {
                    sequence = '\x00'; // NUL
                } else {
                    sequence = key;
                }
            }

            if (hasAlt && sequence) {
                // Alt组合: ESC前缀
                sequence = '\x1b' + sequence;
            } else if (hasAlt && !hasCtrl) {
                sequence = '\x1b' + key;
            }

            // Shift处理：对于字母键，转换为大写
            if (hasShift && !hasCtrl && !hasAlt && key.length === 1) {
                sequence = key.toUpperCase();
            }
        }

        if (sequence) {
            App.sendInput(sequence);
        }

        // 发送组合键后释放非锁定的修饰键状态
        this.activeModifiers.forEach(k => {
            if (!this.lockedModifiers.has(k)) {
                this.activeModifiers.delete(k);
            }
        });
        this.updateModifierUI();
    },

    /**
     * 切换功能键面板显示/隐藏
     */
    toggleFuncKeys() {
        this.funcKeysVisible = !this.funcKeysVisible;
        const el = document.getElementById('func-keys-panel');
        if (el) {
            el.style.display = this.funcKeysVisible ? 'flex' : 'none';
        }
        // 同步按钮高亮状态
        const btn = document.getElementById('btn-func-keys');
        if (btn) {
            btn.classList.toggle('toolbar-btn-active', this.funcKeysVisible);
        }
    },

    /**
     * 切换鼠标模式
     */
    toggleMouseMode() {
        this.mouseMode = !this.mouseMode;
        const container = document.getElementById('terminal-container');
        if (container) {
            container.classList.toggle('mouse-mode-active', this.mouseMode);
        }
        const btn = document.getElementById('btn-mouse-mode');
        if (btn) {
            btn.classList.toggle('toolbar-btn-active', this.mouseMode);
        }

        if (this.mouseMode) {
            this._enableMouseMode();
            App.showToast('鼠标模式已开启：触摸终端发送鼠标事件', 'success');
        } else {
            this._disableMouseMode();
        }
    },

    /**
     * 启用鼠标模式：将touch事件转换为mouse事件发送给xterm.js
     */
    _enableMouseMode() {
        const container = document.getElementById('xterm-terminal');
        if (!container || !TermMgr.term) return;

        // 阻止默认触摸行为（防止滚动）
        const preventDefault = (e) => {
            if (this.mouseMode) e.preventDefault();
        };

        // touchstart -> mousedown
        const onTouchStart = (e) => {
            if (!this.mouseMode) return;
            if (e.touches.length > 1) return; // 允许多指手势（如双指滚动）
            e.preventDefault();
            const touch = e.touches[0];
            this._mouseTouchStart = { x: touch.clientX, y: touch.clientY, time: Date.now() };
            this._dispatchMouseEvent('mousedown', touch);
        };

        // touchmove -> mousemove
        const onTouchMove = (e) => {
            if (!this.mouseMode) return;
            if (e.touches.length > 1) return; // 允许多指手势
            e.preventDefault();
            const touch = e.touches[0];
            this._dispatchMouseEvent('mousemove', touch);
        };

        // touchend -> mouseup + click（如果是短按）
        const onTouchEnd = (e) => {
            if (!this.mouseMode) return;
            const touch = e.changedTouches[0];
            this._dispatchMouseEvent('mouseup', touch);

            // 判断是否为短按（非滑动），发送click
            if (this._mouseTouchStart) {
                const dx = touch.clientX - this._mouseTouchStart.x;
                const dy = touch.clientY - this._mouseTouchStart.y;
                const dt = Date.now() - this._mouseTouchStart.time;
                if (Math.abs(dx) < 10 && Math.abs(dy) < 10 && dt < 300) {
                    this._dispatchMouseEvent('click', touch);
                }
            }
            this._mouseTouchStart = null;
        };

        // 滚轮：双指缩放/滑动 -> wheel事件
        const onWheel = (e) => {
            if (!this.mouseMode) return;
            // 在鼠标模式下允许wheel事件自然传播给xterm.js
        };

        container.addEventListener('touchstart', onTouchStart, { passive: false });
        container.addEventListener('touchmove', onTouchMove, { passive: false });
        container.addEventListener('touchend', onTouchEnd, { passive: false });
        container.addEventListener('touchcancel', onTouchEnd, { passive: false });

        this._mouseListeners = [
            { el: container, type: 'touchstart', fn: onTouchStart },
            { el: container, type: 'touchmove', fn: onTouchMove },
            { el: container, type: 'touchend', fn: onTouchEnd },
            { el: container, type: 'touchcancel', fn: onTouchEnd },
        ];
    },

    /**
     * 禁用鼠标模式
     */
    _disableMouseMode() {
        this._mouseListeners.forEach(item => {
            item.el.removeEventListener(item.type, item.fn);
        });
        this._mouseListeners = [];
    },

    /**
     * 将触摸坐标转换为鼠标事件并分发给xterm.js
     */
    _dispatchMouseEvent(type, touch) {
        const container = document.getElementById('xterm-terminal');
        if (!container) return;

        const rect = container.getBoundingClientRect();
        const x = touch.clientX - rect.left;
        const y = touch.clientY - rect.top;

        // 创建并分发合成鼠标事件
        const evt = new MouseEvent(type, {
            bubbles: true,
            cancelable: true,
            view: window,
            clientX: touch.clientX,
            clientY: touch.clientY,
            screenX: touch.screenX,
            screenY: touch.screenY,
            button: 0, // 左键
            buttons: type === 'mouseup' ? 0 : 1,
        });

        // 分发到xterm.js的底层元素（.xterm-screen或容器本身）
        const screenEl = container.querySelector('.xterm-screen') || container;
        screenEl.dispatchEvent(evt);
    },

    /**
     * 更新修饰键按钮的视觉状态
     */
    updateModifierUI() {
        const allModifierBtns = document.querySelectorAll('.vk-modifier, .fk-modifier');
        allModifierBtns.forEach(btn => {
            const key = btn.dataset.key;
            btn.classList.remove('modifier-active', 'modifier-locked');
            if (this.lockedModifiers.has(key)) {
                btn.classList.add('modifier-locked');
            } else if (this.activeModifiers.has(key)) {
                btn.classList.add('modifier-active');
            }
        });
    }
};

/**
 * keyboard.js - 移动端虚拟键盘模块
 * 负责移动端检测、虚拟键盘渲染、组合键状态管理
 */
const Keyboard = {
    visible: false,
    isMobileDevice: false,

    // 修饰键状态: 'ctrl' | 'alt'
    activeModifiers: new Set(),

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
    },

    /**
     * 初始化虚拟键盘
     */
    init() {
        this.isMobileDevice = this.isMobile();

        // 绑定虚拟按键事件
        const vkKeys = document.querySelectorAll('.vk-key');
        vkKeys.forEach(btn => {
            const key = btn.dataset.key;

            // 使用touchstart/click处理，防止iOS延迟
            btn.addEventListener('touchstart', (e) => {
                e.preventDefault();
                this.handleKey(key, btn);
            });

            btn.addEventListener('mousedown', (e) => {
                e.preventDefault();
                this.handleKey(key, btn);
            });
        });

        // 移动端默认不显示虚拟键盘，由用户切换
        // 但如果是移动设备，显示键盘切换按钮
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
        if (key === 'Ctrl' || key === 'Alt') {
            this.handleModifier(key);
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
     * 处理修饰键切换
     * @param {string} key - 'Ctrl' 或 'Alt'
     */
    handleModifier(key) {
        if (this.activeModifiers.has(key)) {
            this.activeModifiers.delete(key);
        } else {
            this.activeModifiers.add(key);
        }
        this.updateModifierUI();
    },

    /**
     * 发送组合键
     * @param {string} key - 被修饰的按键
     */
    sendCombination(key) {
        const hasCtrl = this.activeModifiers.has('Ctrl');
        const hasAlt = this.activeModifiers.has('Alt');

        let sequence = '';

        // 处理方向键等特殊键的组合
        if (this.keySequenceMap[key]) {
            const baseSeq = this.keySequenceMap[key];
            // 方向键的序列格式: ESC [ A
            // Ctrl+方向键: ESC [ 1;5 A
            // Alt+方向键: ESC [ 1;3 A
            if (baseSeq.startsWith('\x1b[')) {
                const dirChar = baseSeq.slice(-1); // A, B, C, D
                let modifierCode = 1;
                if (hasCtrl && hasAlt) {
                    modifierCode = 7; // Ctrl+Alt
                } else if (hasCtrl) {
                    modifierCode = 5; // Ctrl
                } else if (hasAlt) {
                    modifierCode = 3; // Alt
                }
                sequence = `\x1b[1;${modifierCode}${dirChar}`;
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
        }

        if (sequence) {
            App.sendInput(sequence);
        }

        // 发送组合键后释放修饰键状态
        this.activeModifiers.clear();
        this.updateModifierUI();
    },

    /**
     * 更新修饰键按钮的视觉状态
     */
    updateModifierUI() {
        const modifierBtns = document.querySelectorAll('.vk-modifier');
        modifierBtns.forEach(btn => {
            const key = btn.dataset.key;
            if (this.activeModifiers.has(key)) {
                btn.classList.add('modifier-active');
            } else {
                btn.classList.remove('modifier-active');
            }
        });
    }
};

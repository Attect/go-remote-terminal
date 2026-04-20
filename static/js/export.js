/**
 * export.js - 终端输出导出模块
 * 负责将终端输出导出为文本文件，清理ANSI转义码
 */
const Export = {
    /**
     * 导出当前终端输出为文本文件
     * @param {object} term - xterm.js Terminal实例
     * @param {string} sessionName - 当前会话名称
     */
    exportOutput(term, sessionName) {
        if (!term) {
            App.showToast('终端未初始化', 'error');
            return;
        }

        try {
            const buffer = term.buffer.active;
            const lines = [];
            const length = buffer.length;

            for (let i = 0; i < length; i++) {
                const line = buffer.getLine(i);
                if (line) {
                    lines.push(line.translateToString(true));
                }
            }

            // 移除末尾空行
            while (lines.length > 0 && lines[lines.length - 1].trim() === '') {
                lines.pop();
            }

            let text = lines.join('\n');

            // 清理ANSI转义码
            text = this.stripAnsi(text);

            if (!text.trim()) {
                App.showToast('终端无输出内容', 'error');
                return;
            }

            // 生成文件名
            const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
            const safeName = (sessionName || 'terminal').replace(/[^a-zA-Z0-9\u4e00-\u9fa5_-]/g, '_');
            const filename = `terminal_output_${safeName}_${timestamp}.txt`;

            // 下载文件
            this.downloadFile(text, filename);
            App.showToast('导出成功', 'success');
        } catch (e) {
            console.error('[Export] failed:', e);
            App.showToast('导出失败: ' + e.message, 'error');
        }
    },

    /**
     * 清理ANSI转义码
     * @param {string} text - 含ANSI转义码的文本
     * @returns {string} 纯文本
     */
    stripAnsi(text) {
        // 匹配ANSI转义序列: ESC [ ... m 等
        // 也匹配 OSC 序列和其他控制序列
        return text
            .replace(/\x1b\][0-9;]*[^\x1b]*\x1b\\/g, '')  // OSC序列
            .replace(/\x1b\[[0-9;]*[A-Za-z]/g, '')          // CSI序列
            .replace(/\x1b\][^\x07]*\x07/g, '')             // OSC序列(BEL终止)
            .replace(/\x1b[\(\)][B0UK]/g, '')               // 字符集选择
            .replace(/\x1b[()][A-Za-z0-9]/g, '')            // 字符集选择
            .replace(/\x1b[\[\]0-9;]*[A-Za-z@]/g, '')      // 通用CSI
            .replace(/\x1b[^[\]()]/g, '')                    // 其他ESC序列
            .replace(/\x07/g, '')                             // BEL
            .replace(/\x00/g, '');                            // NUL
    },

    /**
     * 下载文本文件
     * @param {string} content - 文件内容
     * @param {string} filename - 文件名
     */
    downloadFile(content, filename) {
        const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        a.style.display = 'none';
        document.body.appendChild(a);
        a.click();
        setTimeout(() => {
            document.body.removeChild(a);
            URL.revokeObjectURL(url);
        }, 100);
    }
};

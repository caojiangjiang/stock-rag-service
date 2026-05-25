/**
 * Markdown 渲染：规范化 LLM 输出 + marked 解析 + DOMPurify 消毒
 */
(function (global) {
  let configured = false;

  function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text == null ? '' : String(text);
    return div.innerHTML;
  }

  function configureMarked() {
    if (configured || typeof marked === 'undefined') return;
    marked.use({
      breaks: true,
      gfm: true,
      headerIds: false,
      mangle: false,
    });
    configured = true;
  }

  /** 将模型常输出的「挤在一行」文本整理成标准 Markdown */
  function normalizeMarkdownText(text) {
    if (!text) return '';

    let s = String(text).replace(/\r\n/g, '\n').trim();

    // 清理孤立符号行
    s = s.replace(/^\s*[•·]\s*$/gm, '');

    // 分隔线：--- / --（模型常 inline 输出）
    s = s.replace(/\s*---+\s*/g, '\n\n---\n\n');
    s = s.replace(/([^\n-])\s+--\s+([^\n-])/g, '$1\n\n---\n\n$2');

    // 小节标题：核心原因如下： / 我能提供的服务：
    s = s.replace(/([\u4e00-\u9fff]{2,14})如下：/g, '\n\n### $1\n\n');
    s = s.replace(
      /(^|\n\n)([\u4e00-\u9fff]{2,14}：)(?=\s*[-\d•·]|\s*$)/gm,
      '$1### $2\n\n'
    );
    s = s.replace(/### ([\u4e00-\u9fff]{2,14})：/g, '### $1\n');

    // 强调标签
    s = s.replace(/(重要提醒|温馨提示|合规提示|特别说明|免责声明)：/g, '**$1**\n\n');

    // ###标题 / ###1.
    s = s.replace(/([^\n#])(#{1,6})([^\s#\n])/g, '$1\n\n$2 $3');
    s = s.replace(/^(#{1,6})([^\s#\n])/gm, '$1 $2');

    // 无序列表：-风险 / ：- 科普 / 行内 " -市场"
    s = s.replace(/([：。！？；\n]|^)-([\u4e00-\u9fff])/g, '$1\n- $2');
    s = s.replace(/([^\n-])-([\u4e00-\u9fff*])/g, '$1\n- $2');
    s = s.replace(/([^\n])\s+-\s*([\u4e00-\u9fff*])/g, '$1\n- $2');
    s = s.replace(/•\s*/g, '- ');

    // 有序列表
    s = s.replace(
      /([。！？；:])(\s*)(\d+\.\s*(?:[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}]|\*\*|【|[\u4e00-\u9fff]))/gu,
      '$1\n\n$3'
    );
    s = s.replace(
      /([^\n\d。！？；:])(\d+\.\s*(?:[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}]|[\u4e00-\u9fff]))/gu,
      '$1\n\n$2'
    );

    // **加粗**
    s = s.replace(/([^\n*])(\*\*[^*\n]{2,}\*\*)/g, '$1\n\n$2');

    return s.replace(/\n{3,}/g, '\n\n').trim();
  }

  function render(text) {
    const source = normalizeMarkdownText(text);
    if (!source) return '';

    if (typeof marked !== 'undefined') {
      configureMarked();
      const html = marked.parse(source);
      if (typeof DOMPurify !== 'undefined') {
        return DOMPurify.sanitize(html, {
          USE_PROFILES: { html: true },
          ADD_ATTR: ['target', 'rel'],
        });
      }
      return html;
    }

    return escapeHtml(source).replace(/\n/g, '<br>');
  }

  global.Markdown = { render, normalize: normalizeMarkdownText };
})(window);

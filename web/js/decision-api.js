/**
 * Decision API
 */
(function (global) {
  async function getDailyBrief(options = {}) {
    const query = new URLSearchParams();
    if (options.refresh) query.set('refresh', '1');
    const url = `/api/decision/daily-brief${query.toString() ? '?' + query.toString() : ''}`;
    const res = await Auth.authFetch(url);
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || `请求失败: ${res.status}`);
    }
    return res.json();
  }

  function exportHistoryUrl() {
    return '/api/decision/history/export';
  }

  function openChatWithPrompt(text, title) {
    sessionStorage.setItem('chat_prefill_prompt', text);
    if (title) {
      sessionStorage.setItem('chat_prefill_title', title);
    }
    sessionStorage.setItem('chat_prefill_autosend', '1');
    sessionStorage.setItem('chat_prefill_mode', 'chat');
    window.location.href = 'chat.html';
  }

  global.DecisionAPI = { getDailyBrief, exportHistoryUrl, openChatWithPrompt };
})(window);

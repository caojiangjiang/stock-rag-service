/**
 * Theme API 封装
 */
(function (global) {
  async function getSnapshot(params = {}) {
    const query = new URLSearchParams();
    if (params.theme) query.set('theme', params.theme);
    if (params.market) query.set('market', params.market);
    if (params.refresh) query.set('refresh', '1');
    const url = `/api/themes/snapshot${query.toString() ? '?' + query.toString() : ''}`;
    const res = await Auth.authFetch(url);
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || `请求失败: ${res.status}`);
    }
    return res.json();
  }

  global.ThemeAPI = { getSnapshot };
})(window);

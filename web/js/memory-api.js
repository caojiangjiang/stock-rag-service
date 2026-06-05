/**
 * Memory API — 用户画像与会话记忆
 */
(function (global) {
  async function getProfile() {
    const res = await Auth.authFetch('/api/memory/profile');
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || `请求失败: ${res.status}`);
    }
    return res.json();
  }

  async function updatePreferences(prefs) {
    const res = await Auth.authFetch('/api/memory/preferences', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(prefs),
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || `保存失败: ${res.status}`);
    }
    return res.json();
  }

  async function deleteInsight(insightID) {
    const res = await Auth.authFetch(
      `/api/memory/insights?insight_id=${encodeURIComponent(insightID)}`,
      { method: 'DELETE' }
    );
    if (!res.ok && res.status !== 204) {
      const text = await res.text();
      throw new Error(text || `删除失败: ${res.status}`);
    }
  }

  async function getSessionMemory(conversationID) {
    const res = await Auth.authFetch(
      `/api/memory/session?conversation_id=${encodeURIComponent(conversationID)}`
    );
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || `请求失败: ${res.status}`);
    }
    return res.json();
  }

  global.MemoryAPI = { getProfile, updatePreferences, deleteInsight, getSessionMemory };
})(window);

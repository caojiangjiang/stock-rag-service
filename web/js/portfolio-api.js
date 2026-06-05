/**
 * Portfolio API 封装
 */
(function (global) {
  async function parseError(res) {
    const text = await res.text();
    try {
      const json = JSON.parse(text);
      return json.error || json.message || text;
    } catch {
      return text || `请求失败: ${res.status}`;
    }
  }

  async function listPositions() {
    const res = await Auth.authFetch('/api/portfolio/positions');
    if (!res.ok) throw new Error(await parseError(res));
    return res.json();
  }

  async function getSummary() {
    const res = await Auth.authFetch('/api/portfolio/summary');
    if (!res.ok) throw new Error(await parseError(res));
    return res.json();
  }

  async function upsertPosition(payload) {
    const res = await Auth.authFetch('/api/portfolio/positions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(await parseError(res));
    return res.json();
  }

  async function deletePosition(id) {
    const res = await Auth.authFetch(`/api/portfolio/positions?id=${encodeURIComponent(id)}`, {
      method: 'DELETE',
    });
    if (!res.ok) throw new Error(await parseError(res));
    return res.json();
  }

  async function previewFundImport(payload) {
    const res = await Auth.authFetch('/api/portfolio/import/fund?preview=true', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(await parseError(res));
    return res.json();
  }

  async function importFund(payload) {
    const res = await Auth.authFetch('/api/portfolio/import/fund', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(await parseError(res));
    return res.json();
  }

  async function fetchFundNav(code) {
    const res = await Auth.authFetch(`/api/market/fund/nav?code=${encodeURIComponent(code)}`);
    if (!res.ok) throw new Error(await parseError(res));
    return res.json();
  }

  global.PortfolioAPI = {
    listPositions,
    getSummary,
    upsertPosition,
    deletePosition,
    previewFundImport,
    importFund,
    fetchFundNav,
  };
})(window);

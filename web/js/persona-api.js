/**
 * Persona API 封装
 */
(function (global) {
  async function listPersonas(params = {}) {
    const query = new URLSearchParams();
    if (params.market) query.set('market', params.market);
    if (params.styleTag) query.set('style_tag', params.styleTag);
    if (params.status) query.set('status', params.status);
    if (params.limit) query.set('limit', params.limit);

    const url = `/api/personas${query.toString() ? '?' + query.toString() : ''}`;
    const res = await Auth.authFetch(url);
    if (!res.ok) {
      throw new Error(`Failed to list personas: ${res.status}`);
    }
    return res.json();
  }

  async function getPersona(id) {
    const res = await Auth.authFetch(`/api/personas/${id}`);
    if (!res.ok) {
      if (res.status === 404) throw new Error('Persona not found');
      throw new Error(`Failed to get persona: ${res.status}`);
    }
    return res.json();
  }

  async function chatWithPersona(payload) {
    const res = await Auth.authFetch('/api/personas/chat', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      if (res.status === 404) throw new Error('Persona not found');
      throw new Error(`Chat failed: ${res.status}`);
    }
    return res.json();
  }

  async function runRoundtable(payload) {
    const res = await Auth.authFetch('/api/personas/roundtable', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      throw new Error(`Roundtable failed: ${res.status}`);
    }
    return res.json();
  }

  global.PersonaAPI = {
    listPersonas,
    getPersona,
    chatWithPersona,
    runRoundtable,
  };
})(window);

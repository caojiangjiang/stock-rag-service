/**
 * 认证：access/refresh token、刷新、过期跳转登录页
 */
(function (global) {
  const TOKEN_KEY = 'token';
  const REFRESH_KEY = 'refresh_token';
  const USER_KEY = 'user';

  function getAccessToken() {
    return localStorage.getItem(TOKEN_KEY) || '';
  }

  function getRefreshToken() {
    return localStorage.getItem(REFRESH_KEY) || '';
  }

  function getUser() {
    const raw = localStorage.getItem(USER_KEY);
    if (!raw) return null;
    try {
      return JSON.parse(raw);
    } catch {
      return null;
    }
  }

  function saveSession(data) {
    const access = data.access_token || data.token || '';
    if (access) localStorage.setItem(TOKEN_KEY, access);
    if (data.refresh_token) localStorage.setItem(REFRESH_KEY, data.refresh_token);
    if (data.user) localStorage.setItem(USER_KEY, JSON.stringify(data.user));
  }

  function clearSession() {
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(REFRESH_KEY);
    localStorage.removeItem(USER_KEY);
  }

  function redirectToLogin() {
    const next = encodeURIComponent(global.location.pathname + global.location.search);
    const target = next && next !== '%2Findex.html' ? `login.html?next=${next}` : 'login.html';
    global.location.replace(target);
  }

  async function refreshAccessToken() {
    const refreshToken = getRefreshToken();
    if (!refreshToken) return false;

    try {
      const res = await fetch('/api/auth/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refreshToken }),
      });
      if (!res.ok) return false;
      const data = await res.json();
      saveSession(data);
      return true;
    } catch {
      return false;
    }
  }

  async function fetchMe() {
    const token = getAccessToken();
    if (!token) return null;
    const res = await fetch('/api/auth/me', {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!res.ok) return null;
    const data = await res.json();
    if (data.user) localStorage.setItem(USER_KEY, JSON.stringify(data.user));
    return data.user;
  }

  /** 受保护页面入口：无效/过期 token 则跳转登录 */
  async function ensureAuthenticated(options = {}) {
    const { redirect = true } = options;
    let user = await fetchMe();
    if (user) return user;

    const refreshed = await refreshAccessToken();
    if (refreshed) {
      user = await fetchMe();
      if (user) return user;
    }

    clearSession();
    if (redirect) redirectToLogin();
    return null;
  }

  /** 已登录用户访问 login/register 时跳回首页 */
  async function redirectIfAuthenticated() {
    const user = await fetchMe();
    if (user) {
      const params = new URLSearchParams(global.location.search);
      global.location.replace(params.get('next') || 'index.html');
      return true;
    }
    if (getAccessToken() && (await refreshAccessToken())) {
      const again = await fetchMe();
      if (again) {
        global.location.replace('index.html');
        return true;
      }
    }
    return false;
  }

  /**
   * 带鉴权的 fetch；401 时尝试 refresh 一次，仍失败则清 session 并跳转登录
   */
  async function authFetch(url, options = {}) {
    const headers = new Headers(options.headers || {});
    const token = getAccessToken();
    if (token) headers.set('Authorization', `Bearer ${token}`);
    if (options.body && !headers.has('Content-Type')) {
      headers.set('Content-Type', 'application/json');
    }

    let res = await fetch(url, { ...options, headers });

    if (res.status !== 401) return res;

    const refreshed = await refreshAccessToken();
    if (!refreshed) {
      clearSession();
      redirectToLogin();
      throw new Error('session expired');
    }

    headers.set('Authorization', `Bearer ${getAccessToken()}`);
    res = await fetch(url, { ...options, headers });
    if (res.status === 401) {
      clearSession();
      redirectToLogin();
      throw new Error('session expired');
    }
    return res;
  }

  async function logout() {
    const token = getAccessToken();
    const refreshToken = getRefreshToken();
    try {
      if (token) {
        await fetch('/api/auth/logout', {
          method: 'POST',
          headers: {
            Authorization: `Bearer ${token}`,
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ refresh_token: refreshToken }),
        });
      }
    } catch {
      /* ignore */
    }
    clearSession();
    redirectToLogin();
  }

  global.Auth = {
    getAccessToken,
    getRefreshToken,
    getUser,
    saveSession,
    clearSession,
    redirectToLogin,
    ensureAuthenticated,
    redirectIfAuthenticated,
    authFetch,
    logout,
  };
})(window);

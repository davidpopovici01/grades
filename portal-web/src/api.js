const API_BASE = '/api';

async function apiFetch(path, options = {}) {
  const res = await fetch(`${API_BASE}${path}`, {
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
    ...options,
  });

  if (res.status === 401) {
    window.dispatchEvent(new CustomEvent('auth:unauthorized'));
  }

  const data = await res.json().catch(() => null);

  if (!res.ok) {
    const error = new Error(data?.error || `HTTP ${res.status}`);
    error.status = res.status;
    error.data = data;
    throw error;
  }

  return data;
}

export const login = (username, password) =>
  apiFetch('/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });

export const logout = () =>
  apiFetch('/logout', { method: 'POST' });

export const getMe = () =>
  apiFetch('/me');

export const getGrades = () =>
  apiFetch('/grades');

export const getIndex = () =>
  apiFetch('/index');

import { useState, useEffect, useCallback } from 'react';
import { getMe, login as apiLogin, logout as apiLogout } from '../api';

export function useAuth() {
  const [user, setUser] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const checkAuth = useCallback(async () => {
    try {
      const me = await getMe();
      setUser(me);
      setError(null);
    } catch (err) {
      if (err.status === 401) {
        setUser(null);
      } else {
        setError(err.message);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    // Deferred to a microtask so the auth state updates happen outside the
    // effect body (react-hooks/set-state-in-effect).
    Promise.resolve().then(checkAuth);

    const handleUnauthorized = () => {
      setUser(null);
      setLoading(false);
    };

    window.addEventListener('auth:unauthorized', handleUnauthorized);
    return () => window.removeEventListener('auth:unauthorized', handleUnauthorized);
  }, [checkAuth]);

  const login = async (username, password) => {
    setError(null);
    try {
      const result = await apiLogin(username, password);
      setUser(result);
      return result;
    } catch (err) {
      setError(err.data?.error || err.message);
      throw err;
    }
  };

  const logout = async () => {
    try {
      await apiLogout();
    } finally {
      setUser(null);
    }
  };

  return { user, loading, error, login, logout, checkAuth };
}

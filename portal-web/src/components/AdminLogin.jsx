import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { adminListCourses } from '../api';

export function AdminLogin() {
  const [token, setToken] = useState('');
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!token.trim()) return;
    setError(null);
    setLoading(true);
    sessionStorage.setItem('adminToken', token.trim());
    try {
      const data = await adminListCourses();
      if (!data || !Array.isArray(data.courses)) {
        throw new Error('unexpected response');
      }
      navigate('/admin');
    } catch (err) {
      sessionStorage.removeItem('adminToken');
      if (err.status === 401) {
        setError('Invalid token');
      } else if (err.status === 404) {
        setError('Admin is not available here — the local preview only serves the student view.');
      } else {
        setError(err.message);
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-sm bg-white rounded-xl shadow-sm border border-gray-200 p-8">
        <h1 className="text-2xl font-semibold text-gray-900 text-center mb-1">Admin Portal</h1>
        <p className="text-sm text-gray-500 text-center mb-6">Enter your teacher token</p>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Token</label>
            <input
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent text-sm"
              placeholder="••••••••"
              autoComplete="off"
              required
            />
          </div>

          {error && (
            <div className="text-sm text-red-600 bg-red-50 px-3 py-2 rounded-lg">
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={loading}
            className="w-full bg-blue-600 hover:bg-blue-700 text-white font-medium py-2.5 rounded-lg transition text-sm disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {loading ? 'Signing in...' : 'Sign In'}
          </button>
        </form>
      </div>
    </div>
  );
}

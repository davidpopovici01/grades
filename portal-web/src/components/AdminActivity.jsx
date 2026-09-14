import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { adminActivity } from '../api';

const ONLINE_WINDOW_MS = 5 * 60 * 1000;
const REFRESH_MS = 30 * 1000;

const KIND_STYLES = {
  login: 'bg-green-50 text-green-700 border-green-200',
  login_failed: 'bg-red-50 text-red-700 border-red-200',
  submit: 'bg-blue-50 text-blue-700 border-blue-200',
  upload: 'bg-indigo-50 text-indigo-700 border-indigo-200',
  download: 'bg-gray-100 text-gray-600 border-gray-200',
  password_change: 'bg-amber-50 text-amber-700 border-amber-200',
};

const KIND_LABELS = {
  login: 'logged in',
  login_failed: 'failed login',
  submit: 'submitted',
  upload: 'staged file',
  download: 'downloaded',
  password_change: 'changed password',
};

function formatTime(iso) {
  const date = new Date(iso);
  const now = Date.now();
  const diff = now - date.getTime();
  if (diff < 60 * 1000) return 'just now';
  if (diff < 60 * 60 * 1000) return `${Math.floor(diff / 60000)} min ago`;
  if (diff < 24 * 60 * 60 * 1000) return date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  return date.toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
}

function lastSeenLabel(acc) {
  if (!acc.lastSeenAt) return 'never';
  return formatTime(acc.lastSeenAt);
}

export function AdminActivity() {
  const [data, setData] = useState(null);
  const [now, setNow] = useState(() => Date.now());
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const navigate = useNavigate();

  const load = useCallback(() => {
    return adminActivity(100)
      .then((d) => {
        setData(d);
        setNow(Date.now());
      })
      .catch((err) => {
        if (err.status === 401) {
          sessionStorage.removeItem('adminToken');
          navigate('/admin/login');
          return;
        }
        setError(err.message);
      })
      .finally(() => setLoading(false));
  }, [navigate]);

  useEffect(() => {
    if (!sessionStorage.getItem('adminToken')) {
      navigate('/admin/login');
      return;
    }
    load();
    const timer = setInterval(load, REFRESH_MS);
    return () => clearInterval(timer);
  }, [navigate, load]);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
        <div className="text-center">
          <div className="text-red-600 mb-2">Failed to load activity</div>
          <div className="text-sm text-gray-500">{error}</div>
        </div>
      </div>
    );
  }

  const events = data?.events || [];
  const accounts = data?.accounts || [];
  const online = accounts.filter(
    (a) => a.lastSeenAt && now - new Date(a.lastSeenAt).getTime() < ONLINE_WINDOW_MS,
  );
  const byLeastRecent = [...accounts].sort((a, b) => {
    if (!a.lastSeenAt && !b.lastSeenAt) return a.username.localeCompare(b.username);
    if (!a.lastSeenAt) return -1;
    if (!b.lastSeenAt) return 1;
    return new Date(a.lastSeenAt) - new Date(b.lastSeenAt);
  });

  return (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white shadow-sm border-b border-gray-200">
        <div className="max-w-5xl mx-auto px-4 py-3 flex items-center justify-between">
          <div className="flex items-center gap-6">
            <Link to="/admin" className="font-semibold text-gray-800 hover:text-gray-900">
              Grades Admin
            </Link>
            <Link to="/admin/materials" className="text-sm text-gray-600 hover:text-gray-900">
              Materials
            </Link>
            <Link to="/admin/submissions" className="text-sm text-gray-600 hover:text-gray-900">
              Submissions
            </Link>
            <span className="text-sm font-medium text-gray-900">Activity</span>
          </div>
          <button
            onClick={() => {
              sessionStorage.removeItem('adminToken');
              navigate('/admin/login');
            }}
            className="text-red-600 hover:text-red-700 font-medium text-sm"
          >
            Sign Out
          </button>
        </div>
      </nav>
      <main className="max-w-5xl mx-auto px-4 py-6 space-y-6">
        {/* Online now */}
        <div className="bg-white rounded-xl border border-gray-200 shadow-sm p-6">
          <div className="flex items-center justify-between mb-3">
            <h2 className="font-semibold text-gray-800">Online now</h2>
            <button
              onClick={() => load()}
              className="text-sm text-blue-600 hover:text-blue-700 font-medium"
            >
              Refresh
            </button>
          </div>
          {online.length === 0 ? (
            <div className="text-sm text-gray-500">No students active in the last 5 minutes.</div>
          ) : (
            <div className="flex flex-wrap gap-2">
              {online.map((a) => (
                <span
                  key={a.username}
                  className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-sm bg-green-50 text-green-800 border border-green-200"
                >
                  <span className="w-2 h-2 rounded-full bg-green-500"></span>
                  {a.username}
                </span>
              ))}
            </div>
          )}
        </div>

        {/* Recent activity feed */}
        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-gray-100">
            <h2 className="font-semibold text-gray-800">Recent activity</h2>
          </div>
          {events.length === 0 ? (
            <div className="px-6 py-12 text-center text-gray-500">No activity recorded yet.</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <tbody className="divide-y divide-gray-100">
                  {events.map((ev) => (
                    <tr key={ev.id}>
                      <td className="px-6 py-2.5 whitespace-nowrap text-gray-400 w-40">
                        {formatTime(ev.createdAt)}
                      </td>
                      <td className="px-6 py-2.5 font-medium text-gray-900">{ev.username || '—'}</td>
                      <td className="px-6 py-2.5">
                        <span
                          className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border ${KIND_STYLES[ev.kind] || 'bg-gray-100 text-gray-700 border-gray-200'}`}
                        >
                          {KIND_LABELS[ev.kind] || ev.kind}
                        </span>
                      </td>
                      <td className="px-6 py-2.5 text-gray-600">{ev.detail}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {/* Last seen per student */}
        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-gray-100">
            <h2 className="font-semibold text-gray-800">Last seen</h2>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <tbody className="divide-y divide-gray-100">
                {byLeastRecent.map((a) => (
                  <tr key={a.username}>
                    <td className="px-6 py-2.5 font-medium text-gray-900">{a.username}</td>
                    <td className="px-6 py-2.5 text-right">
                      {a.lastSeenAt ? (
                        <span className="text-gray-500">{lastSeenLabel(a)}</span>
                      ) : (
                        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-red-50 text-red-700 border border-red-200">
                          never
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </main>
    </div>
  );
}

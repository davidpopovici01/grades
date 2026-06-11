import { Link, useLocation } from 'react-router-dom';

export function Layout({ user, onLogout, children }) {
  const location = useLocation();

  return (
    <div className="min-h-screen bg-gray-50">
      {user && (
        <nav className="bg-white shadow-sm border-b border-gray-200">
          <div className="max-w-5xl mx-auto px-4 py-3 flex items-center justify-between">
            <div className="flex items-center gap-6">
              <span className="font-semibold text-gray-800">Grades Portal</span>
              <div className="flex gap-4 text-sm">
                <Link
                  to="/"
                  className={`px-3 py-1 rounded-md transition ${
                    location.pathname === '/' ? 'bg-blue-50 text-blue-700 font-medium' : 'text-gray-600 hover:text-gray-900'
                  }`}
                >
                  Grades
                </Link>
                <Link
                  to="/what-if"
                  className={`px-3 py-1 rounded-md transition ${
                    location.pathname === '/what-if' ? 'bg-blue-50 text-blue-700 font-medium' : 'text-gray-600 hover:text-gray-900'
                  }`}
                >
                  What-If
                </Link>
              </div>
            </div>
            <div className="flex items-center gap-3 text-sm">
              <Link
                to="/change-password"
                className="text-gray-600 hover:text-gray-900"
              >
                Change Password
              </Link>
              <span className="text-gray-500">{user.username}</span>
              <button
                onClick={onLogout}
                className="text-red-600 hover:text-red-700 font-medium"
              >
                Sign Out
              </button>
            </div>
          </div>
        </nav>
      )}
      <main className="max-w-5xl mx-auto px-4 py-6">
        {children}
      </main>
    </div>
  );
}

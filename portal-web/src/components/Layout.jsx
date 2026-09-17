import { Link, useLocation } from 'react-router-dom';
import { CourseSelect } from './CourseSelect';

// portalHosts returns the canonical URLs of the two subdomains, or null on
// unrecognized hosts (local dev, custom domains) so links stay same-origin.
function portalHosts() {
  const host = window.location.hostname;
  if (host.startsWith('grades.')) {
    return { grades: `https://${host}`, materials: `https://materials.${host.slice(7)}` };
  }
  if (host.startsWith('materials.')) {
    return { grades: `https://grades.${host.slice(10)}`, materials: `https://${host}` };
  }
  return null;
}

export function Layout({ user, onLogout, courses, selectedCourseKey, onSelectCourse, children }) {
  const location = useLocation();
  const hosts = portalHosts();
  const onMaterialsHost = window.location.hostname.startsWith('materials.');

  const navClass = (active) =>
    `px-3 py-1 rounded-md transition ${
      active ? 'bg-blue-50 text-blue-700 font-medium' : 'text-gray-600 hover:text-gray-900'
    }`;

  return (
    <div className="min-h-screen bg-gray-50">
      {user && (
        <nav className="bg-white shadow-sm border-b border-gray-200">
          <div className="max-w-5xl mx-auto px-4 py-3 flex items-center justify-between">
            <div className="flex items-center gap-6">
              <span className="font-semibold text-gray-800">Grades Portal</span>
              <div className="flex gap-4 text-sm">
                {onMaterialsHost && hosts ? (
                  <a href={hosts.grades} className={navClass(false)}>
                    Grades
                  </a>
                ) : (
                  <Link to="/" className={navClass(location.pathname === '/')}>
                    Grades
                  </Link>
                )}
                <Link to="/what-if" className={navClass(location.pathname === '/what-if')}>
                  What-If
                </Link>
                <Link to="/submissions" className={navClass(location.pathname === '/submissions')}>
                  Submissions
                </Link>
                {!onMaterialsHost && hosts ? (
                  <a href={`${hosts.materials}/materials`} className={navClass(false)}>
                    Materials
                  </a>
                ) : (
                  <Link to="/materials" className={navClass(location.pathname === '/materials')}>
                    Materials
                  </Link>
                )}
              </div>
            </div>
            <div className="flex items-center gap-3 text-sm">
              <CourseSelect courses={courses} value={selectedCourseKey} onChange={onSelectCourse} />
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

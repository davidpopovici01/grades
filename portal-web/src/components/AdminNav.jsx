import { Link, useLocation, useNavigate } from 'react-router-dom';
import { CourseSelect } from './CourseSelect';

const NAV_LINKS = [
  { to: '/admin', label: 'Overview', exact: true },
  { to: '/admin/materials', label: 'Materials' },
  { to: '/admin/submissions', label: 'Submissions' },
  { to: '/admin/activity', label: 'Activity' },
];

// AdminNav is the shared admin header: brand + the same nav links on every
// page (with the active one highlighted), an optional breadcrumb, the
// portal-wide course selector, and Sign Out on the right.
export function AdminNav({ courses, selectedKey, onSelectCourse, wide = false, crumb = null }) {
  const navigate = useNavigate();
  const location = useLocation();

  const navClass = (active) =>
    `px-3 py-1 rounded-md transition text-sm ${
      active ? 'bg-blue-50 text-blue-700 font-medium' : 'text-gray-600 hover:text-gray-900'
    }`;

  return (
    <nav className="bg-white shadow-sm border-b border-gray-200">
      <div
        className={`${wide ? 'max-w-6xl' : 'max-w-5xl'} mx-auto px-4 py-3 flex items-center justify-between gap-4`}
      >
        <div className="flex items-center gap-4 min-w-0">
          <span className="font-semibold text-gray-800">Grades Admin</span>
          <div className="flex gap-1">
            {NAV_LINKS.map((link) => {
              const active = link.exact
                ? location.pathname === link.to
                : location.pathname.startsWith(link.to);
              return (
                <Link key={link.to} to={link.to} className={navClass(active)}>
                  {link.label}
                </Link>
              );
            })}
          </div>
          {crumb && (
            <>
              <span className="text-gray-400">/</span>
              <span className="text-gray-600 text-sm truncate">{crumb}</span>
            </>
          )}
        </div>
        <div className="flex items-center gap-3 shrink-0">
          <CourseSelect courses={courses} value={selectedKey} onChange={onSelectCourse} />
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
      </div>
    </nav>
  );
}

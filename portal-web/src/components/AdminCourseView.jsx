import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { adminListStudents, adminResetPassword, adminUnpublishCourse } from '../api';

function formatPercent(value) {
  if (value === null || value === undefined || isNaN(value)) return '—';
  return `${value.toFixed(1)}%`;
}

export function AdminCourseView() {
  const { courseYearId, termId } = useParams();
  const [students, setStudents] = useState(null);
  const [courseInfo, setCourseInfo] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [resetResult, setResetResult] = useState(null);
  const [unpublishing, setUnpublishing] = useState(false);
  const navigate = useNavigate();

  const courseYearIdNum = parseInt(courseYearId, 10);
  const termIdNum = parseInt(termId, 10);

  useEffect(() => {
    const token = sessionStorage.getItem('adminToken');
    if (!token) {
      navigate('/admin/login');
      return;
    }

    adminListStudents(courseYearIdNum, termIdNum)
      .then((data) => {
        setStudents(data.students || []);
        setCourseInfo({ courseName: data.courseName, termName: data.termName });
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
  }, [courseYearIdNum, termIdNum, navigate]);

  const handleResetPassword = async (student) => {
    if (!window.confirm(`Reset password for ${student.firstName} ${student.lastName}?`)) return;
    try {
      const result = await adminResetPassword(student.studentId);
      setResetResult(result);
    } catch (err) {
      setError(err.message);
    }
  };

  const handleUnpublish = async () => {
    if (!window.confirm('Unpublish this course? This removes it and all student snapshots from the portal.')) return;
    setUnpublishing(true);
    try {
      await adminUnpublishCourse(courseYearIdNum, termIdNum);
      navigate('/admin');
    } catch (err) {
      setError(err.message);
    } finally {
      setUnpublishing(false);
    }
  };

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
          <div className="text-red-600 mb-2">Failed to load students</div>
          <div className="text-sm text-gray-500">{error}</div>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white shadow-sm border-b border-gray-200">
        <div className="max-w-5xl mx-auto px-4 py-3 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <Link to="/admin" className="font-semibold text-gray-800 hover:text-gray-900">
              Grades Admin
            </Link>
            <span className="text-gray-400">/</span>
            <span className="text-gray-600 text-sm">
              {courseInfo?.courseName
                ? `${courseInfo.courseName} · ${courseInfo.termName}`
                : `Course ${courseYearId} · Term ${termId}`}
            </span>
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
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-gray-900">Students</h2>
          <button
            onClick={handleUnpublish}
            disabled={unpublishing}
            className="text-red-600 hover:text-red-700 font-medium text-sm disabled:opacity-50"
          >
            {unpublishing ? 'Unpublishing...' : 'Unpublish Course'}
          </button>
        </div>

        {resetResult && (
          <div className="bg-green-50 border border-green-200 rounded-lg p-4 text-sm">
            <div className="font-medium text-green-900">Password reset for {resetResult.username}</div>
            <div className="text-green-700 mt-1">
              Temporary password: <code className="bg-white px-2 py-0.5 rounded border border-green-200">{resetResult.temporaryPassword}</code>
            </div>
            <button
              onClick={() => setResetResult(null)}
              className="mt-2 text-xs text-green-600 hover:text-green-800"
            >
              Dismiss
            </button>
          </div>
        )}

        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          {students.length === 0 ? (
            <div className="px-6 py-12 text-center text-gray-500">No students published in this course.</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-gray-50 text-gray-600">
                  <tr>
                    <th className="text-left px-6 py-3 font-medium">Student</th>
                    <th className="text-left px-6 py-3 font-medium">Username</th>
                    <th className="text-right px-6 py-3 font-medium">Grade</th>
                    <th className="text-right px-6 py-3 font-medium"></th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {students.map((student) => (
                    <tr key={student.studentId}>
                      <td className="px-6 py-3">
                        <div className="font-medium text-gray-900">
                          {student.firstName} {student.lastName}
                        </div>
                        {student.chineseName && (
                          <div className="text-xs text-gray-400">{student.chineseName}</div>
                        )}
                      </td>
                      <td className="px-6 py-3 text-gray-600">{student.username || '—'}</td>
                      <td className="px-6 py-3 text-right">
                        <span className="font-medium text-gray-900">{formatPercent(student.weightedTotal)}</span>
                        {student.letterGrade && (
                          <span className="ml-2 text-sm text-gray-500">({student.letterGrade})</span>
                        )}
                      </td>
                      <td className="px-6 py-3 text-right">
                        <button
                          onClick={() => handleResetPassword(student)}
                          className="text-blue-600 hover:text-blue-700 text-sm font-medium"
                        >
                          Reset Password
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

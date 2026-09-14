import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { adminListStudents, adminResetPassword, adminUnpublishCourse } from '../api';

function formatPercent(value) {
  if (value === null || value === undefined || isNaN(value)) return '—';
  return `${value.toFixed(1)}%`;
}

const FLAG_STYLE = {
  missing: 'bg-red-50 text-red-700',
  cheat: 'bg-red-100 text-red-800',
  late: 'bg-amber-50 text-amber-700',
  redo: 'bg-orange-50 text-orange-700',
  pass: 'bg-green-50 text-green-700',
};

function FlagChip({ flag }) {
  return (
    <span className={`px-1 py-0.5 rounded text-[10px] font-medium ${FLAG_STYLE[flag] || 'bg-gray-100 text-gray-600'}`}>
      {flag}
    </span>
  );
}

function GradeCell({ cell }) {
  if (!cell) {
    return <span className="text-gray-300">—</span>;
  }
  // Follow the student overview: missing/redo mean "needs action" only while
  // pending (no score, or below the pass threshold) — computed server-side.
  const flags = (cell.flags || []).filter((f) => {
    if (f !== 'missing' && f !== 'redo') return true;
    return cell.pending?.[f] === true;
  });
  return (
    <div className="flex flex-col items-center gap-0.5">
      <span className="text-xs text-gray-700">{cell.label || '—'}</span>
      {flags.length > 0 && (
        <span className="flex flex-wrap justify-center gap-0.5">
          {flags.map((f) => <FlagChip key={f} flag={f} />)}
        </span>
      )}
    </div>
  );
}

export function AdminCourseView() {
  const { courseYearId, termId } = useParams();
  const [students, setStudents] = useState(null);
  const [assignments, setAssignments] = useState([]);
  const [courseInfo, setCourseInfo] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [resetResult, setResetResult] = useState(null);
  const [unpublishing, setUnpublishing] = useState(false);
  const navigate = useNavigate();

  const courseYearIdNum = parseInt(courseYearId, 10);
  const termIdNum = parseInt(termId, 10);

  const load = useCallback(() => {
    const token = sessionStorage.getItem('adminToken');
    if (!token) {
      navigate('/admin/login');
      return Promise.resolve();
    }
    return adminListStudents(courseYearIdNum, termIdNum)
      .then((data) => {
        setStudents(data.students || []);
        setAssignments(data.assignments || []);
        setCourseInfo({
          courseName: data.courseName,
          courseYearName: data.courseYearName,
          termName: data.termName,
          publishedAt: data.publishedAt,
        });
      })
      .catch((err) => {
        if (err.status === 401) {
          sessionStorage.removeItem('adminToken');
          navigate('/admin/login');
          return;
        }
        setError(err.message);
      });
  }, [courseYearIdNum, termIdNum, navigate]);

  useEffect(() => {
    load().finally(() => setLoading(false));
  }, [load]);

  const handleRefresh = () => {
    setRefreshing(true);
    load().finally(() => setRefreshing(false));
  };

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
        <div className="max-w-6xl mx-auto px-4 py-3 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <Link to="/admin" className="font-semibold text-gray-800 hover:text-gray-900">
              Grades Admin
            </Link>
            <span className="text-gray-400">/</span>
            <span className="text-gray-600 text-sm">
              {courseInfo?.courseName
                ? `${courseInfo.courseName}${courseInfo.courseYearName ? ` · ${courseInfo.courseYearName}` : ''} · ${courseInfo.termName}`
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
      <main className="max-w-6xl mx-auto px-4 py-6 space-y-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="text-sm text-gray-500">
            {courseInfo?.publishedAt
              ? `Grades published ${new Date(courseInfo.publishedAt).toLocaleString()} — if this looks old, run grades publish on your laptop.`
              : ''}
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={handleRefresh}
              disabled={refreshing}
              className="px-3 py-1.5 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 disabled:opacity-50 transition"
            >
              {refreshing ? 'Refreshing...' : 'Refresh'}
            </button>
            <button
              onClick={handleUnpublish}
              disabled={unpublishing}
              className="text-red-600 hover:text-red-700 font-medium text-sm disabled:opacity-50"
            >
              {unpublishing ? 'Unpublishing...' : 'Unpublish Course'}
            </button>
          </div>
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
          <div className="px-6 py-4 border-b border-gray-100">
            <h2 className="font-semibold text-gray-800">Grades Overview</h2>
          </div>
          {students.length === 0 ? (
            <div className="px-6 py-12 text-center text-gray-500">No students published in this course.</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="text-sm border-collapse">
                <thead className="bg-gray-50 text-gray-600">
                  <tr>
                    <th className="text-left px-4 py-3 font-medium sticky left-0 bg-gray-50 min-w-40">Student</th>
                    <th className="text-center px-3 py-3 font-medium">Total</th>
                    {assignments.map((a) => (
                      <th key={a.id} className="text-center px-3 py-3 font-medium min-w-24" title={`${a.title} (${a.categoryName}, ${a.maxPoints} pts)`}>
                        <div className="max-w-28 truncate mx-auto">{a.title}</div>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {students.map((student) => (
                    <tr key={student.studentId}>
                      <td className="px-4 py-2 sticky left-0 bg-white">
                        <div className="font-medium text-gray-900 whitespace-nowrap">
                          {student.firstName} {student.lastName}
                        </div>
                        <div className="flex flex-wrap items-center gap-1 mt-0.5">
                          {student.missing > 0 && (
                            <span className="px-1 py-0.5 rounded bg-red-50 text-red-700 text-[10px] font-medium">
                              {student.missing} missing
                            </span>
                          )}
                          {student.redo > 0 && (
                            <span className="px-1 py-0.5 rounded bg-orange-50 text-orange-700 text-[10px] font-medium">
                              {student.redo} redo
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-3 py-2 text-center whitespace-nowrap">
                        <span className="font-medium text-gray-900">{formatPercent(student.weightedTotal)}</span>
                        {student.letterGrade && (
                          <span className="ml-1 text-xs text-gray-500">({student.letterGrade})</span>
                        )}
                      </td>
                      {assignments.map((a) => (
                        <td key={a.id} className="px-3 py-2 text-center">
                          <GradeCell cell={student.grades?.[a.id]} />
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-gray-100">
            <h2 className="font-semibold text-gray-800">Accounts</h2>
          </div>
          {students.length === 0 ? (
            <div className="px-6 py-12 text-center text-gray-500">No students published in this course.</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-gray-50 text-gray-600">
                  <tr>
                    <th className="text-left px-6 py-3 font-medium">Student</th>
                    <th className="text-left px-6 py-3 font-medium">Username</th>
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

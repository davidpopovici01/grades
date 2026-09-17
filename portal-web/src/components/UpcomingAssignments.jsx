import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getSubmissions } from '../api';
import { resolveCourse } from '../hooks/useCourseSelection';

const startOfDay = (d) => new Date(d.getFullYear(), d.getMonth(), d.getDate());

function dueInfo(dueAt) {
  if (!dueAt) return { text: 'No due date', overdue: false };
  const due = new Date(dueAt);
  if (isNaN(due)) return { text: 'No due date', overdue: false };
  const formatted = due.toLocaleString();
  if (due.getTime() < Date.now()) return { text: `Was due ${formatted}`, overdue: true };
  const dayDiff = Math.round((startOfDay(due) - startOfDay(new Date())) / 86400000);
  const rel = dayDiff === 0 ? 'today' : dayDiff === 1 ? 'tomorrow' : `in ${dayDiff} days`;
  return { text: `Due ${formatted} · ${rel}`, overdue: false };
}

function StatusBadge({ assignment, overdue }) {
  const sub = assignment.latestSubmission;
  const warn = overdue ? 'bg-red-50 text-red-700' : 'bg-amber-50 text-amber-700';

  if ((assignment.publicTestCount || 0) > 0) {
    const passed = assignment.publicPassed || 0;
    const failed = assignment.publicFailed || 0;
    if (passed + failed > 0) {
      return (
        <span
          className={`px-2 py-0.5 rounded-full text-xs font-medium ${
            failed === 0 ? 'bg-green-50 text-green-700' : 'bg-amber-50 text-amber-700'
          }`}
        >
          {passed} / {passed + failed} tests passed
        </span>
      );
    }
    if (sub) {
      return (
        <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-500">
          submitted{sub.isLate ? ' · late' : ''} · not tested
        </span>
      );
    }
    return (
      <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${warn}`}>
        not submitted
      </span>
    );
  }

  if (sub) {
    return (
      <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-green-50 text-green-700">
        submitted{sub.isLate ? ' · late' : ''}
      </span>
    );
  }
  if ((assignment.draftFiles || []).length > 0) {
    return (
      <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${warn}`}>
        files staged — not submitted
      </span>
    );
  }
  return (
    <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${warn}`}>
      not submitted
    </span>
  );
}

// UpcomingAssignments lists the selected course's open and testable
// assignments (soonest due first) with submission/test status, so students
// see what still needs doing and how their tests are doing in one place.
export function UpcomingAssignments({ selectedCourseKey }) {
  const [courses, setCourses] = useState(null);

  useEffect(() => {
    getSubmissions()
      .then((data) => setCourses(data?.courses || []))
      .catch(() => setCourses([]));
  }, []);

  const course = resolveCourse(courses, selectedCourseKey);
  const assignments = (course?.assignments || [])
    .filter((a) => a.isOpen || (a.publicTestCount || 0) > 0)
    .sort((a, b) => {
      const ta = a.dueAt ? Date.parse(a.dueAt) : Infinity;
      const tb = b.dueAt ? Date.parse(b.dueAt) : Infinity;
      return ta - tb;
    });
  if (assignments.length === 0) return null;

  const pending = assignments.filter((a) => a.isOpen && !a.latestSubmission).length;

  return (
    <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
      <div className="px-6 py-4 border-b border-gray-100 flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <h2 className="font-semibold text-gray-800">Upcoming Assignments</h2>
          {pending > 0 && (
            <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-amber-50 text-amber-700">
              {pending} to submit
            </span>
          )}
        </div>
        <Link to="/submissions" className="text-sm text-blue-600 hover:text-blue-700 font-medium">
          Open Submissions
        </Link>
      </div>
      <div className="divide-y divide-gray-100">
        {assignments.map((a) => {
          const due = dueInfo(a.dueAt);
          return (
            <div key={a.id} className="px-6 py-3 flex flex-wrap items-center justify-between gap-2">
              <div className="min-w-0">
                <div className="text-sm font-medium text-gray-900">{a.title}</div>
                <div className={`text-xs ${due.overdue ? 'text-red-600' : 'text-gray-400'}`}>
                  {due.text}
                  {!a.isOpen && ' · closed'}
                </div>
              </div>
              <StatusBadge assignment={a} overdue={due.overdue} />
            </div>
          );
        })}
      </div>
    </div>
  );
}

import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  adminDownloadSubmissionFile,
  adminGetSubAssignment,
  adminGetSubmission,
  adminGetSubmissionFileText,
  adminListSubAssignmentSubmissions,
} from '../api';
import { formatSize, isViewable } from '../format';

// fetchDetail loads a submission's file list plus the text of every viewable
// file, cached per submission id so flipping back and forth is instant.
function fetchDetail(cache, submission) {
  if (!cache.has(submission.id)) {
    const promise = adminGetSubmission(submission.id)
      .then(async (data) => {
        const sub = data?.submission || {};
        const files = data?.files || sub.files || [];
        const withText = await Promise.all(
          files.map(async (f) => {
            if (!isViewable(f.name)) return { ...f, text: null };
            const text = await adminGetSubmissionFileText(submission.id, f.name).catch(() => null);
            return { ...f, text };
          })
        );
        return { submission: sub, files: withText };
      })
      .catch((err) => {
        cache.delete(submission.id);
        throw err;
      });
    cache.set(submission.id, promise);
  }
  return cache.get(submission.id);
}

// AdminSubmissionScan is a quick-scan view: one student's latest submission at
// a time, all viewable files already open, with prev/next navigation for
// flipping through the whole roster.
export function AdminSubmissionScan() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [assignment, setAssignment] = useState(null);
  const [students, setStudents] = useState(null);
  const [index, setIndex] = useState(0);
  const [detail, setDetail] = useState(null);
  const [error, setError] = useState(null);
  const cacheRef = useRef(new Map());

  useEffect(() => {
    if (!sessionStorage.getItem('adminToken')) {
      navigate('/admin/login');
      return;
    }
    Promise.all([adminGetSubAssignment(id), adminListSubAssignmentSubmissions(id)])
      .then(([a, s]) => {
        setAssignment(a?.assignment || null);
        setStudents((s?.students || []).filter((st) => st.latestSubmission));
      })
      .catch((err) => {
        if (err.status === 401) {
          sessionStorage.removeItem('adminToken');
          navigate('/admin/login');
          return;
        }
        setError(err.message);
      });
  }, [id, navigate]);

  const student = students?.[index] || null;
  const submissionId = student?.latestSubmission?.id || null;

  // Clear the shown detail when the current submission changes. This is a
  // state adjustment during render, kept out of the effect below.
  const [prevSubmissionId, setPrevSubmissionId] = useState(null);
  if (submissionId !== prevSubmissionId) {
    setPrevSubmissionId(submissionId);
    setDetail(null);
  }

  // Load the current student's submission, prefetch the neighbours, and reset
  // the scroll position so each student starts at the top of their code.
  useEffect(() => {
    if (!students || !student) return;
    let cancelled = false;
    fetchDetail(cacheRef.current, student.latestSubmission)
      .then((d) => {
        if (!cancelled) setDetail(d);
      })
      .catch((err) => {
        if (!cancelled) setError(err.message);
      });
    for (const n of [index + 1, index - 1]) {
      const neighbour = students[n];
      if (neighbour) fetchDetail(cacheRef.current, neighbour.latestSubmission).catch(() => {});
    }
    window.scrollTo(0, 0);
    return () => {
      cancelled = true;
    };
  }, [students, student, index]);

  // Arrow keys (and vim-style j/k) flip between students.
  useEffect(() => {
    if (!students || students.length === 0) return undefined;
    const onKey = (e) => {
      const tag = e.target?.tagName;
      if (tag === 'SELECT' || tag === 'INPUT' || tag === 'TEXTAREA') return;
      if (e.key === 'ArrowRight' || e.key === 'j') {
        setIndex((i) => Math.min(i + 1, students.length - 1));
      } else if (e.key === 'ArrowLeft' || e.key === 'k') {
        setIndex((i) => Math.max(i - 1, 0));
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [students]);

  if (error && !assignment) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
        <div className="text-center">
          <div className="text-red-600 mb-2">Failed to load assignment</div>
          <div className="text-sm text-gray-500">{error}</div>
        </div>
      </div>
    );
  }

  if (!assignment || !students) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  const navButtonClass =
    'px-3 py-1.5 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 disabled:opacity-40 transition';

  return (
    <div className="min-h-screen bg-gray-50">
      <div className="sticky top-0 z-10 bg-white border-b border-gray-200 shadow-sm">
        <div className="max-w-6xl mx-auto px-4 py-3 space-y-2">
          <div className="flex flex-wrap items-center gap-3">
            <Link
              to={`/admin/submissions/${id}`}
              className="text-sm text-blue-600 hover:text-blue-700 font-medium"
            >
              &larr; Back
            </Link>
            <span className="text-sm font-medium text-gray-700 truncate">{assignment.title}</span>
            <div className="flex items-center gap-2 ml-auto">
              <select
                value={index}
                onChange={(e) => setIndex(Number(e.target.value))}
                className="px-2 py-1.5 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 max-w-48"
              >
                {students.map((s, i) => (
                  <option key={s.studentId} value={i}>
                    {s.firstName} {s.lastName} ({s.username})
                  </option>
                ))}
              </select>
              <button onClick={() => setIndex((i) => i - 1)} disabled={index === 0} className={navButtonClass}>
                &lsaquo; Prev
              </button>
              <span className="text-xs text-gray-400 whitespace-nowrap">
                {index + 1} / {students.length}
              </span>
              <button
                onClick={() => setIndex((i) => i + 1)}
                disabled={index >= students.length - 1}
                className={navButtonClass}
              >
                Next &rsaquo;
              </button>
              <span className="text-xs text-gray-400 hidden lg:inline">&larr; &rarr; to flip</span>
            </div>
          </div>
          {student && (
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-semibold text-gray-900">
                {student.firstName} {student.lastName}
              </span>
              <span className="text-xs text-gray-400">{student.username}</span>
              <span className="text-xs text-gray-500">
                {new Date(student.latestSubmission.submittedAt).toLocaleString()}
              </span>
              {student.latestSubmission.isLate && (
                <span className="px-1.5 py-0.5 rounded bg-amber-50 text-amber-700 text-xs font-medium">
                  late · cap {student.latestSubmission.capPercent}%
                </span>
              )}
              <span
                className={`text-xs font-medium ${student.publicFailed > 0 ? 'text-amber-700' : 'text-green-700'}`}
              >
                public {student.publicPassed}/{student.publicPassed + student.publicFailed}
              </span>
              <span
                className={`text-xs font-medium ${student.secretFailed > 0 ? 'text-amber-700' : 'text-green-700'}`}
              >
                secret {student.secretPassed}/{student.secretPassed + student.secretFailed}
              </span>
            </div>
          )}
        </div>
      </div>

      <main className="max-w-6xl mx-auto px-4 py-6 space-y-4">
        {error && (
          <div className="text-sm text-red-700 bg-red-50 border border-red-200 px-3 py-2 rounded-lg">
            {error}
          </div>
        )}
        {students.length === 0 ? (
          <div className="bg-white rounded-xl border border-gray-200 shadow-sm px-6 py-12 text-center text-gray-500">
            No submissions yet.
          </div>
        ) : !detail ? (
          <div className="text-center text-gray-400 py-12">Loading...</div>
        ) : detail.files.length === 0 ? (
          <div className="bg-white rounded-xl border border-gray-200 shadow-sm px-6 py-12 text-center text-gray-500">
            This submission has no files.
          </div>
        ) : (
          detail.files.map((f) => (
            <div key={f.name} className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
              <div className="px-4 py-2 border-b border-gray-100 flex items-center gap-2">
                <code className="text-sm text-gray-800 break-all">{f.name}</code>
                <span className="text-xs text-gray-400">{formatSize(f.size)}</span>
                <button
                  onClick={() =>
                    adminDownloadSubmissionFile(detail.submission.id, f.name).catch((err) =>
                      setError(err.message)
                    )
                  }
                  className="text-xs text-blue-600 hover:text-blue-700 font-medium ml-auto shrink-0"
                >
                  Download
                </button>
              </div>
              {f.text !== null ? (
                <pre className="overflow-x-auto text-xs p-4 whitespace-pre-wrap break-all font-mono text-gray-800">
                  {f.text}
                </pre>
              ) : (
                <div className="px-4 py-3 text-sm text-gray-400">
                  {isViewable(f.name)
                    ? 'Could not load this file inline — use Download.'
                    : 'This file type cannot be viewed inline — use Download.'}
                </div>
              )}
            </div>
          ))
        )}
      </main>
    </div>
  );
}

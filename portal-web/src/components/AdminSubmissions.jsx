import { useCallback, useEffect, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import {
  adminListCourses,
  adminListSubAssignments,
  adminCreateSubAssignment,
  adminUpdateSubAssignment,
  adminDeleteSubAssignment,
} from '../api';
import { formatSize } from '../format';

const EMPTY_FORM = {
  title: '',
  language: 'java',
  dueAt: '',
  expectedFilenames: '',
  maxFileBytes: '262144',
  maxTotalBytes: '1048576',
  lateCapPercent: '90',
  isOpen: true,
  instructions: '',
};

// toLocalInput converts an RFC3339 timestamp to the value a datetime-local
// input expects, in the browser's local timezone.
function toLocalInput(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d)) return '';
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// filenamesToText accepts either a string array or a newline-joined string.
function filenamesToText(value) {
  if (Array.isArray(value)) return value.join('\n');
  return value || '';
}

function toInt(value, fallback) {
  const n = parseInt(value, 10);
  return isNaN(n) ? fallback : n;
}

function formFromAssignment(assignment) {
  return {
    title: assignment.title || '',
    language: assignment.language || 'java',
    dueAt: toLocalInput(assignment.dueAt),
    expectedFilenames: filenamesToText(assignment.expectedFilenames),
    maxFileBytes: String(assignment.maxFileBytes ?? 262144),
    maxTotalBytes: String(assignment.maxTotalBytes ?? 1048576),
    lateCapPercent: String(assignment.lateCapPercent ?? 90),
    isOpen: assignment.isOpen !== false,
    instructions: assignment.instructions || '',
  };
}

export function AdminSubmissions() {
  const [courses, setCourses] = useState(null);
  const [selectedKey, setSelectedKey] = useState('');
  const [assignments, setAssignments] = useState([]);
  const [error, setError] = useState(null);
  const [notice, setNotice] = useState(null);
  const [form, setForm] = useState(EMPTY_FORM);
  const [formOpen, setFormOpen] = useState(false);
  const [editingId, setEditingId] = useState(null);
  const [saving, setSaving] = useState(false);
  const location = useLocation();
  const navigate = useNavigate();
  const [pendingEditId, setPendingEditId] = useState(location.state?.editId || null);

  // Open the edit form when arriving via the detail page's Edit link. This is
  // a state adjustment during render: it runs once, then consumes pendingEditId.
  const editTarget = pendingEditId ? assignments.find((a) => a.id === pendingEditId) : null;
  if (editTarget) {
    setEditingId(editTarget.id);
    setForm(formFromAssignment(editTarget));
    setFormOpen(true);
    setPendingEditId(null);
  }

  const selected = (courses || []).find(
    (c) => `${c.courseYearId}-${c.termId}` === selectedKey
  );

  const refresh = useCallback((course) => {
    return adminListSubAssignments(course.courseYearId, course.termId)
      .then((data) => setAssignments(data?.assignments || []))
      .catch((err) => setError(err.message));
  }, []);

  useEffect(() => {
    if (!sessionStorage.getItem('adminToken')) {
      navigate('/admin/login');
      return;
    }
    adminListCourses()
      .then((data) => {
        const list = data?.courses || [];
        setCourses(list);
        if (list.length > 0) {
          setSelectedKey(`${list[0].courseYearId}-${list[0].termId}`);
        }
      })
      .catch((err) => {
        if (err.status === 401) {
          sessionStorage.removeItem('adminToken');
          navigate('/admin/login');
          return;
        }
        setError(err.message);
      });
  }, [navigate]);

  useEffect(() => {
    if (selected) refresh(selected);
  }, [selected, refresh]);

  const startEdit = (assignment) => {
    setEditingId(assignment.id);
    setForm(formFromAssignment(assignment));
    setFormOpen(true);
    setError(null);
    setNotice(null);
  };

  const startCreate = () => {
    setEditingId(null);
    setForm(EMPTY_FORM);
    setFormOpen(true);
    setError(null);
    setNotice(null);
  };

  const saveForm = () => {
    if (!selected || !form.title.trim()) {
      setError('A title is required.');
      return;
    }
    const body = {
      courseYearId: selected.courseYearId,
      termId: selected.termId,
      title: form.title.trim(),
      language: form.language,
      instructions: form.instructions,
      dueAt: form.dueAt ? new Date(form.dueAt).toISOString() : '',
      expectedFilenames: form.expectedFilenames
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean)
        .join('\n'),
      maxFileBytes: toInt(form.maxFileBytes, 262144),
      maxTotalBytes: toInt(form.maxTotalBytes, 1048576),
      lateCapPercent: toInt(form.lateCapPercent, 90),
      isOpen: form.isOpen,
    };
    setSaving(true);
    setError(null);
    setNotice(null);
    const request = editingId
      ? adminUpdateSubAssignment(editingId, body)
      : adminCreateSubAssignment(body);
    request
      .then(() => {
        setNotice(editingId ? 'Assignment updated.' : 'Assignment created.');
        setFormOpen(false);
        setEditingId(null);
        setForm(EMPTY_FORM);
        return refresh(selected);
      })
      .catch((err) => setError(err.message))
      .finally(() => setSaving(false));
  };

  const deleteAssignment = (assignment) => {
    if (!window.confirm(`Delete assignment "${assignment.title}" and all its submissions?`)) return;
    setError(null);
    setNotice(null);
    adminDeleteSubAssignment(assignment.id)
      .then(() => {
        setNotice('Assignment deleted.');
        return refresh(selected);
      })
      .catch((err) => setError(err.message));
  };

  if (!courses) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  const inputClass =
    'w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500';
  const labelClass = 'block text-sm font-medium text-gray-700 mb-1';

  return (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white shadow-sm border-b border-gray-200">
        <div className="max-w-5xl mx-auto px-4 py-3 flex items-center justify-between">
          <div className="flex items-center gap-6">
            <span className="font-semibold text-gray-800">Grades Admin</span>
            <button
              onClick={() => navigate('/admin')}
              className="text-sm text-gray-600 hover:text-gray-900"
            >
              Courses
            </button>
            <button
              onClick={() => navigate('/admin/materials')}
              className="text-sm text-gray-600 hover:text-gray-900"
            >
              Materials
            </button>
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
      <main className="max-w-5xl mx-auto px-4 py-6 space-y-4">
        <div className="bg-white rounded-xl border border-gray-200 p-4 shadow-sm">
          <label className="block text-sm font-medium text-gray-700 mb-2">Course</label>
          <select
            value={selectedKey}
            onChange={(e) => setSelectedKey(e.target.value)}
            className="w-full sm:w-auto px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            {courses.map((c) => (
              <option key={`${c.courseYearId}-${c.termId}`} value={`${c.courseYearId}-${c.termId}`}>
                {c.courseName}{c.courseYearName ? ` · ${c.courseYearName}` : ''} · {c.termName}
              </option>
            ))}
          </select>
        </div>

        {error && (
          <div className="text-sm text-red-700 bg-red-50 border border-red-200 px-3 py-2 rounded-lg">
            {error}
          </div>
        )}
        {notice && (
          <div className="text-sm text-green-700 bg-green-50 border border-green-200 px-3 py-2 rounded-lg">
            {notice}
          </div>
        )}

        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-gray-100 flex items-center justify-between">
            <h2 className="font-semibold text-gray-800">Code Submission Assignments</h2>
            {!formOpen && (
              <button
                onClick={startCreate}
                disabled={!selected}
                className="px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
              >
                New Assignment
              </button>
            )}
          </div>

          {formOpen && (
            <div className="px-6 py-4 border-b border-gray-100 bg-gray-50 space-y-3">
              <h3 className="text-sm font-semibold text-gray-700">
                {editingId ? 'Edit Assignment' : 'New Assignment'}
              </h3>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <label className={labelClass}>Title</label>
                  <input
                    type="text"
                    value={form.title}
                    onChange={(e) => setForm({ ...form, title: e.target.value })}
                    className={inputClass}
                    placeholder="e.g. Lab 3 — Recursion"
                  />
                </div>
                <div>
                  <label className={labelClass}>Type</label>
                  <select
                    value={form.language}
                    onChange={(e) => {
                      const language = e.target.value;
                      // Generic file submissions (video, Excel, Word) need much
                      // larger limits than code.
                      const bump = language === 'files'
                        ? { maxFileBytes: '104857600', maxTotalBytes: '524288000' }
                        : {};
                      setForm({ ...form, language, ...bump });
                    }}
                    className={inputClass}
                  >
                    <option value="java">Java code</option>
                    <option value="python">Python code</option>
                    <option value="text">Text / essay (.txt, .md)</option>
                    <option value="files">Other files (Excel, Word, video…)</option>
                  </select>
                  {(form.language === 'text' || form.language === 'files') && (
                    <p className="text-xs text-gray-400 mt-1">
                      No automated tests{form.language === 'files' ? ' or plagiarism detection' : ''} for this type.
                    </p>
                  )}
                </div>
                <div>
                  <label className={labelClass}>Due date (server-enforced)</label>
                  <input
                    type="datetime-local"
                    value={form.dueAt}
                    onChange={(e) => setForm({ ...form, dueAt: e.target.value })}
                    className={inputClass}
                  />
                </div>
                <div>
                  <label className={labelClass}>Late cap (% of score)</label>
                  <input
                    type="number"
                    min="0"
                    max="100"
                    value={form.lateCapPercent}
                    onChange={(e) => setForm({ ...form, lateCapPercent: e.target.value })}
                    className={inputClass}
                  />
                  <p className="text-xs text-gray-400 mt-1">90 = late work can earn at most 90% (a 10% penalty).</p>
                </div>
                <div>
                  <label className={labelClass}>Max bytes per file</label>
                  <input
                    type="number"
                    min="1"
                    value={form.maxFileBytes}
                    onChange={(e) => setForm({ ...form, maxFileBytes: e.target.value })}
                    className={inputClass}
                  />
                  <p className="text-xs text-gray-400 mt-1">Default {formatSize(262144)}.</p>
                </div>
                <div>
                  <label className={labelClass}>Max total bytes</label>
                  <input
                    type="number"
                    min="1"
                    value={form.maxTotalBytes}
                    onChange={(e) => setForm({ ...form, maxTotalBytes: e.target.value })}
                    className={inputClass}
                  />
                  <p className="text-xs text-gray-400 mt-1">Default {formatSize(1048576)}.</p>
                </div>
              </div>
              <div>
                <label className={labelClass}>Expected filenames (one per line)</label>
                <textarea
                  value={form.expectedFilenames}
                  onChange={(e) => setForm({ ...form, expectedFilenames: e.target.value })}
                  rows={3}
                  className={`${inputClass} font-mono`}
                  placeholder={'Main.java\nHelper.java'}
                />
                <p className="text-xs text-gray-400 mt-1">
                  Students must submit exactly this set of files (case-insensitive).
                  Use name.ext1/ext2 to allow alternatives, e.g. report.docx/pdf.
                  {form.language === 'java' && ' At least one entry must allow a .java file; other files (report.docx, data.xlsx, demo.mp4…) may be required alongside.'}
                  {form.language === 'python' && ' At least one entry must allow a .py file; other files (report.docx, data.xlsx, demo.mp4…) may be required alongside.'}
                  {form.language === 'text' && ' At least one entry must allow a .txt or .md file; other files may be required alongside.'}
                </p>
              </div>
              <div>
                <label className={labelClass}>Instructions</label>
                <textarea
                  value={form.instructions}
                  onChange={(e) => setForm({ ...form, instructions: e.target.value })}
                  rows={4}
                  className={inputClass}
                />
              </div>
              <label className="flex items-center gap-2 text-sm text-gray-700">
                <input
                  type="checkbox"
                  checked={form.isOpen}
                  onChange={(e) => setForm({ ...form, isOpen: e.target.checked })}
                  className="rounded border-gray-300"
                />
                Open for submissions
              </label>
              <div className="flex gap-2">
                <button
                  onClick={saveForm}
                  disabled={saving}
                  className="px-3 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
                >
                  {saving ? 'Saving...' : editingId ? 'Save Changes' : 'Create Assignment'}
                </button>
                <button
                  onClick={() => { setFormOpen(false); setEditingId(null); setForm(EMPTY_FORM); }}
                  className="px-3 py-2 text-sm font-medium text-gray-600 hover:text-gray-900"
                >
                  Cancel
                </button>
              </div>
            </div>
          )}

          {assignments.length === 0 ? (
            <div className="px-6 py-12 text-center text-gray-500">
              No submission assignments for this course yet.
            </div>
          ) : (
            <div className="divide-y divide-gray-100">
              {assignments.map((assignment) => (
                <div key={assignment.id} className="px-6 py-4 flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium text-gray-900">{assignment.title}</span>
                      <span
                        className={`px-2 py-0.5 rounded-full text-xs font-medium ${
                          assignment.language === 'python'
                            ? 'bg-green-50 text-green-700'
                            : assignment.language === 'java'
                              ? 'bg-blue-50 text-blue-700'
                              : 'bg-gray-100 text-gray-600'
                        }`}
                      >
                        {assignment.language}
                      </span>
                      {!assignment.isOpen && (
                        <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-500">
                          Closed
                        </span>
                      )}
                    </div>
                    <div className="text-sm text-gray-500 mt-0.5">
                      {assignment.dueAt
                        ? `Due ${new Date(assignment.dueAt).toLocaleString()}`
                        : 'No due date'}
                      {' · '}
                      {Array.isArray(assignment.expectedFilenames)
                        ? assignment.expectedFilenames.length
                        : filenamesToText(assignment.expectedFilenames).split('\n').filter(Boolean).length}
                      {' file(s) required'}
                    </div>
                  </div>
                  <div className="flex items-center gap-2 shrink-0">
                    <Link
                      to={`/admin/submissions/${assignment.id}`}
                      className="px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition"
                    >
                      Manage
                    </Link>
                    <button
                      onClick={() => startEdit(assignment)}
                      className="px-3 py-1.5 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition"
                    >
                      Edit
                    </button>
                    <button
                      onClick={() => deleteAssignment(assignment)}
                      className="px-3 py-1.5 text-sm font-medium text-red-600 hover:text-red-700"
                    >
                      Delete
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

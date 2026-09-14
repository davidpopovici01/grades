import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  adminGetSubAssignment,
  adminUploadSubTest,
  adminDeleteSubTest,
  adminListSubAssignmentSubmissions,
  adminRunSubTests,
  adminGetSubmission,
  adminDownloadSubmissionFile,
  adminGetSubmissionFileText,
  adminGetSubTestFile,
  adminUpdateSubTest,
  adminGetSampleFiles,
  adminGetSampleFile,
  adminPutSampleFile,
  adminDeleteSampleFile,
  adminRunSample,
  adminGetBasecodeFiles,
  adminGetBasecodeFile,
  adminPutBasecodeFile,
  adminDeleteBasecodeFile,
  adminRunPlagiarism,
  adminGetPlagiarism,
  adminDownloadPlagReport,
  adminCreateSession,
  adminGetSubQueue,
} from '../api';
import { formatSize } from '../format';

function filenamesList(value) {
  if (Array.isArray(value)) return value;
  return (value || '').split('\n').map((s) => s.trim()).filter(Boolean);
}

// similarity may arrive as 0-1 or 0-100 depending on the backend export.
function similarityPercent(value) {
  if (value === null || value === undefined || isNaN(value)) return 0;
  return value <= 1 ? value * 100 : value;
}

function VisibilityBadge({ visibility }) {
  const secret = visibility === 'secret';
  return (
    <span
      className={`px-2 py-0.5 rounded-full text-xs font-medium ${
        secret ? 'bg-purple-50 text-purple-700' : 'bg-gray-100 text-gray-600'
      }`}
    >
      {secret ? 'secret' : 'public'}
    </span>
  );
}

function RunStatus({ run }) {
  if (run.status !== 'done') {
    return <span className="text-xs text-gray-400">{run.status}</span>;
  }
  return (
    <span className={`text-xs font-medium ${run.failed > 0 ? 'text-amber-700' : 'text-green-700'}`}>
      {run.passed} / {run.passed + run.failed} tests passed
    </span>
  );
}

export function AdminAssignmentDetail() {
  const { id } = useParams();
  const assignmentId = parseInt(id, 10);
  const navigate = useNavigate();

  const [assignment, setAssignment] = useState(null);
  const [tests, setTests] = useState([]);
  const [students, setStudents] = useState(null);
  const [queue, setQueue] = useState(null);
  const [plag, setPlag] = useState(null);
  const [error, setError] = useState(null);
  const [notice, setNotice] = useState(null);
  const [loadFailed, setLoadFailed] = useState(false);

  const [testFile, setTestFile] = useState(null);
  const [testName, setTestName] = useState('');
  const [testVisibility, setTestVisibility] = useState('public');
  const [testView, setTestView] = useState({});
  const [testEdit, setTestEdit] = useState({});
  const [testSaving, setTestSaving] = useState(null);
  const [sampleFiles, setSampleFiles] = useState(null);
  const [sampleEditor, setSampleEditor] = useState(null);
  const [sampleSaving, setSampleSaving] = useState(false);
  const [sampleResults, setSampleResults] = useState(null);
  const [sampleRunning, setSampleRunning] = useState(false);
  const [basecodeFiles, setBasecodeFiles] = useState(null);
  const [basecodeEditor, setBasecodeEditor] = useState(null);
  const [basecodeSaving, setBasecodeSaving] = useState(false);
  const [uploadingTest, setUploadingTest] = useState(false);
  const [runningAll, setRunningAll] = useState(false);
  const [runningStudent, setRunningStudent] = useState(null);
  const [plagStarting, setPlagStarting] = useState(false);
  const [expanded, setExpanded] = useState({});
  const queueWasActive = useRef(false);

  const handleAuthError = useCallback(
    (err) => {
      if (err.status === 401) {
        sessionStorage.removeItem('adminToken');
        navigate('/admin/login');
        return true;
      }
      return false;
    },
    [navigate]
  );

  const loadAssignment = useCallback(
    () =>
      adminGetSubAssignment(assignmentId).then((data) => {
        setAssignment(data?.assignment || null);
        setTests(data?.tests || []);
      }),
    [assignmentId]
  );

  const loadStudents = useCallback(
    () =>
      adminListSubAssignmentSubmissions(assignmentId).then((data) => {
        setStudents(data?.students || []);
      }),
    [assignmentId]
  );

  const loadPlagiarism = useCallback(
    () =>
      adminGetPlagiarism(assignmentId)
        .then((data) => setPlag(data || null))
        .catch(() => {}),
    [assignmentId]
  );

  const loadSample = useCallback(
    () =>
      adminGetSampleFiles(assignmentId)
        .then((data) => setSampleFiles(data?.files || []))
        .catch(() => {}),
    [assignmentId]
  );

  const loadBasecode = useCallback(
    () =>
      adminGetBasecodeFiles(assignmentId)
        .then((data) => setBasecodeFiles(data?.files || []))
        .catch(() => {}),
    [assignmentId]
  );

  useEffect(() => {
    if (!sessionStorage.getItem('adminToken')) {
      navigate('/admin/login');
      return;
    }
    Promise.all([loadAssignment(), loadStudents(), loadPlagiarism(), loadSample(), loadBasecode()]).catch((err) => {
      if (handleAuthError(err)) return;
      setLoadFailed(true);
      setError(err.message);
    });
  }, [navigate, loadAssignment, loadStudents, loadPlagiarism, loadSample, loadBasecode, handleAuthError]);

  // Poll the worker queue; while it is active, refresh the grid so running
  // spinners and finished results show up live.
  useEffect(() => {
    let cancelled = false;
    const poll = () => {
      adminGetSubQueue()
        .then((data) => {
          if (cancelled) return;
          setQueue(data);
          const active = (data?.depth || 0) + (data?.running || 0) > 0;
          if (active) {
            loadStudents();
          }
          if (queueWasActive.current && !active) {
            loadPlagiarism();
          }
          queueWasActive.current = active;
        })
        .catch(() => {});
    };
    poll();
    const timer = setInterval(poll, 5000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [loadStudents, loadPlagiarism]);

  // While a plagiarism run is queued/running, poll its status.
  const plagStatus = plag?.run?.status;
  useEffect(() => {
    if (plagStatus !== 'queued' && plagStatus !== 'running') return undefined;
    const timer = setInterval(() => loadPlagiarism(), 4000);
    return () => clearInterval(timer);
  }, [plagStatus, loadPlagiarism]);

  const uploadTest = () => {
    if (!testFile || !testName.trim()) {
      setError('Choose a harness file and give the test a name.');
      return;
    }
    setUploadingTest(true);
    setError(null);
    setNotice(null);
    adminUploadSubTest(assignmentId, testFile, testName.trim(), testVisibility)
      .then(() => {
        setTestFile(null);
        setTestName('');
        setTestVisibility('public');
        setNotice('Test uploaded.');
        return loadAssignment();
      })
      .catch((err) => setError(err.message))
      .finally(() => setUploadingTest(false));
  };

  const deleteTest = (test) => {
    if (!window.confirm(`Delete test "${test.name}"?`)) return;
    setError(null);
    setNotice(null);
    adminDeleteSubTest(assignmentId, test.id)
      .then(() => loadAssignment())
      .catch((err) => setError(err.message));
  };

  const toggleTestView = (test) => {
    if (testView[test.id]) {
      setTestView((prev) => {
        const next = { ...prev };
        delete next[test.id];
        return next;
      });
      return;
    }
    setTestView((prev) => ({ ...prev, [test.id]: true }));
    adminGetSubTestFile(assignmentId, test.id)
      .then((text) => setTestView((prev) => ({ ...prev, [test.id]: text })))
      .catch((err) => {
        setTestView((prev) => {
          const next = { ...prev };
          delete next[test.id];
          return next;
        });
        setError(err.message);
      });
  };

  const startTestEdit = (test) => {
    const open = (text) => setTestEdit((prev) => ({ ...prev, [test.id]: text }));
    if (typeof testView[test.id] === 'string') {
      open(testView[test.id]);
      return;
    }
    adminGetSubTestFile(assignmentId, test.id)
      .then((text) => {
        setTestView((prev) => ({ ...prev, [test.id]: text }));
        open(text);
      })
      .catch((err) => setError(err.message));
  };

  const cancelTestEdit = (test) => {
    setTestEdit((prev) => {
      const next = { ...prev };
      delete next[test.id];
      return next;
    });
  };

  const saveTestEdit = (test) => {
    const content = testEdit[test.id];
    setTestSaving(test.id);
    setError(null);
    setNotice(null);
    adminUpdateSubTest(assignmentId, test.id, content)
      .then(() => {
        setTestView((prev) => ({ ...prev, [test.id]: content }));
        cancelTestEdit(test);
        setNotice(`Test "${test.name}" saved.`);
        return loadAssignment();
      })
      .catch((err) => setError(err.message))
      .finally(() => setTestSaving(null));
  };

  const editSampleFile = (file) => {
    setError(null);
    adminGetSampleFile(assignmentId, file.name)
      .then((text) => setSampleEditor({ name: file.name, content: text, isNew: false }))
      .catch((err) => setError(err.message));
  };

  const saveSampleFile = () => {
    if (!sampleEditor || !sampleEditor.name.trim()) {
      setError('Give the sample file a name.');
      return;
    }
    setSampleSaving(true);
    setError(null);
    setNotice(null);
    adminPutSampleFile(assignmentId, sampleEditor.name.trim(), sampleEditor.content)
      .then(() => {
        setSampleEditor(null);
        setNotice('Sample file saved.');
        return loadSample();
      })
      .catch((err) => setError(err.message))
      .finally(() => setSampleSaving(false));
  };

  const deleteSampleFile = (file) => {
    if (!window.confirm(`Delete sample file "${file.name}"?`)) return;
    setError(null);
    setNotice(null);
    adminDeleteSampleFile(assignmentId, file.name)
      .then(() => {
        if (sampleEditor?.name === file.name) setSampleEditor(null);
        return loadSample();
      })
      .catch((err) => setError(err.message));
  };

  const runSample = () => {
    setSampleRunning(true);
    setError(null);
    setNotice(null);
    adminRunSample(assignmentId)
      .then((data) => setSampleResults(data?.results || []))
      .catch((err) => setError(err.message))
      .finally(() => setSampleRunning(false));
  };

  const editBasecodeFile = (file) => {
    setError(null);
    adminGetBasecodeFile(assignmentId, file.name)
      .then((text) => setBasecodeEditor({ name: file.name, content: text, isNew: false }))
      .catch((err) => setError(err.message));
  };

  const saveBasecodeFile = () => {
    if (!basecodeEditor || !basecodeEditor.name.trim()) {
      setError('Give the base code file a name.');
      return;
    }
    setBasecodeSaving(true);
    setError(null);
    setNotice(null);
    adminPutBasecodeFile(assignmentId, basecodeEditor.name.trim(), basecodeEditor.content)
      .then(() => {
        setBasecodeEditor(null);
        setNotice('Base code file saved.');
        return loadBasecode();
      })
      .catch((err) => setError(err.message))
      .finally(() => setBasecodeSaving(false));
  };

  const deleteBasecodeFile = (file) => {
    if (!window.confirm(`Delete base code file "${file.name}"?`)) return;
    setError(null);
    setNotice(null);
    adminDeleteBasecodeFile(assignmentId, file.name)
      .then(() => {
        if (basecodeEditor?.name === file.name) setBasecodeEditor(null);
        return loadBasecode();
      })
      .catch((err) => setError(err.message));
  };

  const runAll = () => {
    setRunningAll(true);
    setError(null);
    setNotice(null);
    adminRunSubTests(assignmentId)
      .then((data) => setNotice(`Queued ${data?.queued ?? 0} test run(s). Results appear when the queue drains.`))
      .catch((err) => setError(err.message))
      .finally(() => setRunningAll(false));
  };

  const runForStudent = (studentId) => {
    setRunningStudent(studentId);
    setError(null);
    setNotice(null);
    adminRunSubTests(assignmentId, studentId)
      .then((data) => setNotice(`Queued ${data?.queued ?? 0} test run(s).`))
      .catch((err) => setError(err.message))
      .finally(() => setRunningStudent(null));
  };

  const toggleExpand = (student) => {
    const sid = student.studentId;
    if (expanded[sid]) {
      setExpanded((prev) => {
        const next = { ...prev };
        delete next[sid];
        return next;
      });
      return;
    }
    if (!student.latestSubmission) return;
    setExpanded((prev) => ({ ...prev, [sid]: 'loading' }));
    adminGetSubmission(student.latestSubmission.id)
      .then((data) => setExpanded((prev) => ({ ...prev, [sid]: data })))
      .catch((err) => {
        setError(err.message);
        setExpanded((prev) => {
          const next = { ...prev };
          delete next[sid];
          return next;
        });
      });
  };

  const viewPlagReport = () => {
    setError(null);
    adminCreateSession()
      .then(() => {
        const reportURL = `${window.location.origin}/api/admin/submissions/assignments/${assignmentId}/plagiarism/report`;
        window.open(`${window.location.origin}/overview?file=${encodeURIComponent(reportURL)}`, '_blank');
      })
      .catch((err) => setError(err.message));
  };

  const startPlagiarism = () => {
    setPlagStarting(true);
    setError(null);
    setNotice(null);
    adminRunPlagiarism(assignmentId)
      .then(() => {
        setNotice('Plagiarism check queued.');
        return loadPlagiarism();
      })
      .catch((err) => setError(err.message))
      .finally(() => setPlagStarting(false));
  };

  if (loadFailed) {
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

  const inputClass =
    'px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500';
  const pairs = [...(plag?.pairs || [])].sort(
    (a, b) => similarityPercent(b.similarity) - similarityPercent(a.similarity)
  );

  return (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white shadow-sm border-b border-gray-200">
        <div className="max-w-5xl mx-auto px-4 py-3 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <Link to="/admin/submissions" className="font-semibold text-gray-800 hover:text-gray-900">
              Grades Admin
            </Link>
            <span className="text-gray-400">/</span>
            <span className="text-gray-600 text-sm">{assignment.title}</span>
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

        <div className="bg-white rounded-xl border border-gray-200 p-6 shadow-sm space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-lg font-semibold text-gray-900">{assignment.title}</h2>
              <span
                className={`px-2 py-0.5 rounded-full text-xs font-medium ${
                  assignment.language === 'python'
                    ? 'bg-green-50 text-green-700'
                    : 'bg-blue-50 text-blue-700'
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
            <Link
              to="/admin/submissions"
              state={{ editId: assignmentId }}
              className="px-3 py-1.5 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition"
            >
              Edit
            </Link>
          </div>
          <div className="text-sm text-gray-500">
            {assignment.dueAt ? `Due ${new Date(assignment.dueAt).toLocaleString()}` : 'No due date'}
            {' · '}Late cap {assignment.lateCapPercent}%
            {' · '}Max {formatSize(assignment.maxFileBytes)} per file, {formatSize(assignment.maxTotalBytes)} total
          </div>
          <div className="flex flex-wrap items-center gap-1">
            <span className="text-xs text-gray-400 mr-1">Required files:</span>
            {filenamesList(assignment.expectedFilenames).map((name) => (
              <code key={name} className="px-1.5 py-0.5 bg-gray-50 border border-gray-200 rounded text-xs text-gray-700">
                {name}
              </code>
            ))}
          </div>
          {assignment.instructions && (
            <p className="text-sm text-gray-600 whitespace-pre-wrap">{assignment.instructions}</p>
          )}
        </div>

        {(assignment.language === 'java' || assignment.language === 'python') && (
        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-gray-100">
            <h2 className="font-semibold text-gray-800">Tests</h2>
          </div>
          <div className="px-6 py-4 border-b border-gray-100 bg-gray-50 space-y-3">
            <p className="text-xs text-gray-500">
              Harness contract: the file runs in a folder next to the student's submitted files
              (Java harnesses are compiled together with the student's <code>.java</code> files;
              Python harnesses are run with <code>python3</code>). It must print one line per
              check: <code className="font-mono">PASS: &lt;label&gt;</code> or{' '}
              <code className="font-mono">FAIL: &lt;label&gt;</code>. Public test results are
              visible to students; secret results are admin-only.
            </p>
            <details className="text-xs text-gray-500">
              <summary className="cursor-pointer text-blue-600 hover:text-blue-700 font-medium">
                Show example {assignment.language === 'java' ? 'Java' : 'Python'} harness
              </summary>
              {assignment.language === 'java' ? (
                <div className="mt-2 space-y-2">
                  <pre className="bg-white border border-gray-200 rounded p-2 overflow-x-auto font-mono">
{`// BasicTests.java — the file name must match the public class name.
// It is compiled together with the student's .java files.
public class BasicTests {
    public static void main(String[] args) {
        check("adds two numbers", Calculator.add(2, 3) == 5);
        check("handles negatives", Calculator.add(-1, 1) == 0);
    }

    static void check(String label, boolean condition) {
        System.out.println((condition ? "PASS: " : "FAIL: ") + label);
    }
}`}
                  </pre>
                  <p>Calls the student's code directly (here: a <code>Calculator</code> class with a static <code>add</code>). A compile error in the student's code counts as a failed run with the compiler output attached.</p>
                </div>
              ) : (
                <div className="mt-2 space-y-2">
                  <pre className="bg-white border border-gray-200 rounded p-2 overflow-x-auto font-mono">
{`# BasicTests.py — run with python3, next to the student's files
import calculator  # the student's calculator.py

def check(label, condition):
    print(("PASS: " if condition else "FAIL: ") + label)

check("adds two numbers", calculator.add(2, 3) == 5)
check("handles negatives", calculator.add(-1, 1) == 0)`}
                  </pre>
                  <p>Imports the student's module and calls their functions. Wrap risky calls in try/except if a student crash should count as one FAIL instead of aborting the whole harness.</p>
                </div>
              )}
            </details>
            <div className="flex flex-wrap items-center gap-2">
              <input
                type="file"
                onChange={(e) => setTestFile(e.target.files?.[0] || null)}
                className="text-sm text-gray-600"
              />
              <input
                type="text"
                value={testName}
                onChange={(e) => setTestName(e.target.value)}
                placeholder="Test name"
                className={inputClass}
              />
              <label className="flex items-center gap-1 text-sm text-gray-700">
                <input
                  type="radio"
                  name="visibility"
                  checked={testVisibility === 'public'}
                  onChange={() => setTestVisibility('public')}
                />
                Public
              </label>
              <label className="flex items-center gap-1 text-sm text-gray-700">
                <input
                  type="radio"
                  name="visibility"
                  checked={testVisibility === 'secret'}
                  onChange={() => setTestVisibility('secret')}
                />
                Secret
              </label>
              <button
                onClick={uploadTest}
                disabled={uploadingTest}
                className="px-3 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
              >
                {uploadingTest ? 'Uploading...' : 'Upload Test'}
              </button>
            </div>
          </div>
          {tests.length === 0 ? (
            <div className="px-6 py-4 text-sm text-gray-400">No tests uploaded yet.</div>
          ) : (
            <div className="divide-y divide-gray-100">
              {tests.map((test) => (
                <div key={test.id} className="px-6 py-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="flex items-center gap-2 min-w-0">
                      <span className="text-sm font-medium text-gray-900 break-all">{test.name}</span>
                      <VisibilityBadge visibility={test.visibility} />
                      <span className="text-xs text-gray-400">{formatSize(test.size)}</span>
                    </div>
                    <div className="flex items-center gap-3 shrink-0">
                      <button
                        onClick={() => toggleTestView(test)}
                        className="text-sm text-blue-600 hover:text-blue-700 font-medium"
                      >
                        {testView[test.id] ? 'Hide' : 'View'}
                      </button>
                      <button
                        onClick={() => startTestEdit(test)}
                        className="text-sm text-blue-600 hover:text-blue-700 font-medium"
                      >
                        Edit
                      </button>
                      <button
                        onClick={() => deleteTest(test)}
                        className="text-sm text-red-600 hover:text-red-700 font-medium"
                      >
                        Delete
                      </button>
                    </div>
                  </div>
                  {testEdit[test.id] !== undefined ? (
                    <div className="mt-2 space-y-2">
                      <textarea
                        rows={16}
                        value={testEdit[test.id]}
                        onChange={(e) => setTestEdit((prev) => ({ ...prev, [test.id]: e.target.value }))}
                        className="w-full text-xs bg-white border border-gray-300 rounded-lg p-3 font-mono text-gray-800 focus:outline-none focus:ring-2 focus:ring-blue-500"
                        spellCheck={false}
                      />
                      <div className="flex items-center gap-2">
                        <button
                          onClick={() => saveTestEdit(test)}
                          disabled={testSaving === test.id}
                          className="px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
                        >
                          {testSaving === test.id ? 'Saving...' : 'Save'}
                        </button>
                        <button
                          onClick={() => cancelTestEdit(test)}
                          disabled={testSaving === test.id}
                          className="px-3 py-1.5 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 disabled:opacity-50 transition"
                        >
                          Cancel
                        </button>
                        <span className="text-xs text-gray-400">Saving overwrites the harness file immediately.</span>
                      </div>
                    </div>
                  ) : (
                    testView[test.id] && (
                      <pre className="mt-2 max-h-96 overflow-auto text-xs bg-gray-50 border border-gray-200 rounded-lg p-3 whitespace-pre-wrap break-all font-mono text-gray-800">
                        {testView[test.id] === true ? 'Loading...' : testView[test.id]}
                      </pre>
                    )
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
        )}

        {(assignment.language === 'java' || assignment.language === 'python') && (
        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-gray-100 flex flex-wrap items-center justify-between gap-2">
            <h2 className="font-semibold text-gray-800">Sample Solution</h2>
            <div className="flex items-center gap-2">
              <button
                onClick={() => setSampleEditor({ name: '', content: '', isNew: true })}
                className="px-3 py-1.5 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition"
              >
                Add File
              </button>
              <button
                onClick={runSample}
                disabled={sampleRunning || (sampleFiles || []).length === 0}
                className="px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
              >
                {sampleRunning ? 'Running...' : 'Run Tests on Sample'}
              </button>
            </div>
          </div>
          <div className="px-6 py-3 border-b border-gray-100 bg-gray-50">
            <p className="text-xs text-gray-500">
              Files here act as a reference submission: "Run Tests on Sample" executes every test above
              against them and shows the full output, so you can verify your harnesses before students submit.
            </p>
          </div>
          {sampleEditor && (
            <div className="px-6 py-4 border-b border-gray-100 space-y-2">
              {sampleEditor.isNew ? (
                <input
                  type="text"
                  value={sampleEditor.name}
                  onChange={(e) => setSampleEditor((prev) => ({ ...prev, name: e.target.value }))}
                  placeholder={assignment.language === 'java' ? 'Welcome.java' : 'main.py'}
                  className={inputClass}
                />
              ) : (
                <code className="text-xs text-gray-700">{sampleEditor.name}</code>
              )}
              <textarea
                rows={14}
                value={sampleEditor.content}
                onChange={(e) => setSampleEditor((prev) => ({ ...prev, content: e.target.value }))}
                placeholder={assignment.language === 'java' ? 'public class Welcome { ... }' : '# sample solution'}
                className="w-full text-xs bg-white border border-gray-300 rounded-lg p-3 font-mono text-gray-800 focus:outline-none focus:ring-2 focus:ring-blue-500"
                spellCheck={false}
              />
              <div className="flex items-center gap-2">
                <button
                  onClick={saveSampleFile}
                  disabled={sampleSaving || !sampleEditor.name.trim()}
                  className="px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
                >
                  {sampleSaving ? 'Saving...' : 'Save'}
                </button>
                <button
                  onClick={() => setSampleEditor(null)}
                  disabled={sampleSaving}
                  className="px-3 py-1.5 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 disabled:opacity-50 transition"
                >
                  Cancel
                </button>
              </div>
            </div>
          )}
          {(sampleFiles || []).length === 0 ? (
            <div className="px-6 py-4 text-sm text-gray-400">No sample files yet.</div>
          ) : (
            <div className="divide-y divide-gray-100">
              {sampleFiles.map((file) => (
                <div key={file.name} className="px-6 py-3 flex items-center justify-between gap-3">
                  <div className="flex items-center gap-2 min-w-0">
                    <code className="text-sm text-gray-900 break-all">{file.name}</code>
                    <span className="text-xs text-gray-400">{formatSize(file.size)}</span>
                  </div>
                  <div className="flex items-center gap-3 shrink-0">
                    <button
                      onClick={() => editSampleFile(file)}
                      className="text-sm text-blue-600 hover:text-blue-700 font-medium"
                    >
                      Edit
                    </button>
                    <button
                      onClick={() => deleteSampleFile(file)}
                      className="text-sm text-red-600 hover:text-red-700 font-medium"
                    >
                      Delete
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
          {sampleResults && (
            <div className="px-6 py-4 border-t border-gray-100 space-y-2">
              <div className="text-xs font-semibold uppercase tracking-wide text-gray-500">
                Sample run results
              </div>
              {sampleResults.length === 0 ? (
                <div className="text-sm text-gray-400">No tests to run.</div>
              ) : (
                sampleResults.map((run) => (
                  <div key={run.testId} className="bg-white border border-gray-200 rounded-lg px-3 py-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-sm font-medium text-gray-900">{run.testName}</span>
                      <VisibilityBadge visibility={run.visibility} />
                      <RunStatus run={run} />
                    </div>
                    {run.output && (
                      <pre className="mt-2 max-h-48 overflow-auto text-xs bg-gray-50 border border-gray-100 rounded p-2 whitespace-pre-wrap break-all font-mono text-gray-800">
                        {run.output}
                      </pre>
                    )}
                  </div>
                ))
              )}
            </div>
          )}
        </div>
        )}

        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-gray-100 flex flex-wrap items-center justify-between gap-2">
            <h2 className="font-semibold text-gray-800">Submissions</h2>
            <div className="flex items-center gap-3">
              {queue && (
                <span className="text-xs text-gray-400">
                  Queue: {queue.depth} queued · {queue.running} running
                </span>
              )}
              <button
                onClick={loadStudents}
                className="px-3 py-1.5 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition"
              >
                Refresh
              </button>
              {(assignment.language === 'java' || assignment.language === 'python') && (
                <button
                  onClick={runAll}
                  disabled={runningAll}
                  className="px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
                >
                  {runningAll ? 'Queueing...' : 'Run All Tests'}
                </button>
              )}
            </div>
          </div>
          {students.length === 0 ? (
            <div className="px-6 py-12 text-center text-gray-500">No students in this course.</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-gray-50 text-gray-600">
                  <tr>
                    <th className="text-left px-6 py-3 font-medium">Student</th>
                    <th className="text-left px-6 py-3 font-medium">Latest Submission</th>
                    <th className="text-left px-6 py-3 font-medium">Public</th>
                    <th className="text-left px-6 py-3 font-medium">Secret</th>
                    <th className="text-right px-6 py-3 font-medium"></th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {students.map((student) => {
                    const detail = expanded[student.studentId];
                    return [
                      <tr key={student.studentId} className="align-top">
                        <td className="px-6 py-3">
                          <div className="flex items-center gap-2 font-medium text-gray-900">
                            {student.firstName} {student.lastName}
                            {student.running && (
                              <span
                                title="Tests running"
                                className="inline-block h-3.5 w-3.5 animate-spin rounded-full border-2 border-blue-600 border-t-transparent"
                              />
                            )}
                          </div>
                          <div className="text-xs text-gray-400">{student.username}</div>
                        </td>
                        <td className="px-6 py-3">
                          {student.latestSubmission ? (
                            <div className="flex items-center gap-2">
                              <span className="text-gray-600">
                                {new Date(student.latestSubmission.submittedAt).toLocaleString()}
                              </span>
                              {student.latestSubmission.isLate && (
                                <span className="px-1.5 py-0.5 rounded bg-amber-50 text-amber-700 text-xs font-medium">
                                  late · cap {student.latestSubmission.capPercent}%
                                </span>
                              )}
                            </div>
                          ) : (
                            <span className="text-gray-400">—</span>
                          )}
                        </td>
                        <td className="px-6 py-3">
                          {student.latestSubmission ? (
                            <span
                              title={`${student.publicPassed} passed, ${student.publicFailed} failed`}
                              className={`text-xs font-medium ${student.publicFailed > 0 ? 'text-amber-700' : 'text-green-700'}`}
                            >
                              {student.publicPassed} / {student.publicPassed + student.publicFailed}
                            </span>
                          ) : (
                            <span className="text-gray-400">—</span>
                          )}
                        </td>
                        <td className="px-6 py-3">
                          {student.latestSubmission ? (
                            <span
                              title={`${student.secretPassed} passed, ${student.secretFailed} failed`}
                              className={`text-xs font-medium ${student.secretFailed > 0 ? 'text-amber-700' : 'text-green-700'}`}
                            >
                              {student.secretPassed} / {student.secretPassed + student.secretFailed}
                            </span>
                          ) : (
                            <span className="text-gray-400">—</span>
                          )}
                          {student.untested && student.latestSubmission && (
                            <span className="ml-2 px-1.5 py-0.5 rounded bg-gray-100 text-gray-500 text-xs font-medium">
                              untested
                            </span>
                          )}
                        </td>
                        <td className="px-6 py-3 text-right whitespace-nowrap">
                          <button
                            onClick={() => runForStudent(student.studentId)}
                            disabled={!student.latestSubmission || runningStudent === student.studentId}
                            className="text-blue-600 hover:text-blue-700 text-sm font-medium disabled:opacity-40 mr-3"
                          >
                            {runningStudent === student.studentId ? 'Queueing...' : 'Run'}
                          </button>
                          {student.latestSubmission && (
                            <button
                              onClick={() => toggleExpand(student)}
                              className="text-gray-600 hover:text-gray-900 text-sm font-medium"
                            >
                              {detail ? 'Hide' : 'Details'}
                            </button>
                          )}
                        </td>
                      </tr>,
                      detail ? (
                        <tr key={`${student.studentId}-detail`} className="bg-gray-50">
                          <td colSpan={5} className="px-6 py-4">
                            {detail === 'loading' ? (
                              <div className="text-sm text-gray-400">Loading...</div>
                            ) : (
                              <SubmissionDetail detail={detail} />
                            )}
                          </td>
                        </tr>
                      ) : null,
                    ];
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {assignment.language !== 'files' && (
        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-gray-100 flex flex-wrap items-center justify-between gap-2">
            <h2 className="font-semibold text-gray-800">Plagiarism Check</h2>
            <button
              onClick={startPlagiarism}
              disabled={plagStarting || plagStatus === 'queued' || plagStatus === 'running'}
              className="px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
            >
              {plagStarting
                ? 'Queueing...'
                : plagStatus === 'queued' || plagStatus === 'running'
                  ? `Running (${plagStatus})...`
                  : 'Run Plagiarism Check'}
            </button>
          </div>
          {plag?.run && (
            <div className="px-6 py-3 border-b border-gray-100 text-xs text-gray-500">
              Last run: {plag.run.status} · started {new Date(plag.run.createdAt).toLocaleString()}
              {plag.run.finishedAt ? ` · finished ${new Date(plag.run.finishedAt).toLocaleString()}` : ''}
              {plag.run.message && (
                <div className="mt-1 text-amber-700">{plag.run.message}</div>
              )}
              {plag.run.status === 'done' && plag.run.reportPath && (
                <div className="mt-2 flex flex-wrap items-center gap-2">
                  <button
                    onClick={viewPlagReport}
                    className="px-3 py-1.5 text-xs font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition"
                  >
                    View Full Report
                  </button>
                  <button
                    onClick={() => adminDownloadPlagReport(assignmentId).catch((err) => setError(err.message))}
                    className="px-3 py-1.5 text-xs font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition"
                  >
                    Download
                  </button>
                  <span>Side-by-side code comparisons open in a new tab.</span>
                </div>
              )}
            </div>
          )}
          <div className="px-6 py-4 border-b border-gray-100 bg-gray-50 space-y-2">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="text-xs font-semibold uppercase tracking-wide text-gray-500">
                Base code (optional)
              </div>
              <button
                onClick={() => setBasecodeEditor({ name: '', content: '', isNew: true })}
                className="px-2 py-1 text-xs font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition"
              >
                Add File
              </button>
            </div>
            <p className="text-xs text-gray-500">
              Starter/template code handed out to all students. It is subtracted from every submission
              before comparing, so shared boilerplate does not inflate similarity. Applies to the next run.
            </p>
            {basecodeEditor && (
              <div className="space-y-2">
                {basecodeEditor.isNew ? (
                  <input
                    type="text"
                    value={basecodeEditor.name}
                    onChange={(e) => setBasecodeEditor((prev) => ({ ...prev, name: e.target.value }))}
                    placeholder={assignment.language === 'java' ? 'Template.java' : 'template.py'}
                    className={inputClass}
                  />
                ) : (
                  <code className="text-xs text-gray-700">{basecodeEditor.name}</code>
                )}
                <textarea
                  rows={10}
                  value={basecodeEditor.content}
                  onChange={(e) => setBasecodeEditor((prev) => ({ ...prev, content: e.target.value }))}
                  className="w-full text-xs bg-white border border-gray-300 rounded-lg p-3 font-mono text-gray-800 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  spellCheck={false}
                />
                <div className="flex items-center gap-2">
                  <button
                    onClick={saveBasecodeFile}
                    disabled={basecodeSaving || !basecodeEditor.name.trim()}
                    className="px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
                  >
                    {basecodeSaving ? 'Saving...' : 'Save'}
                  </button>
                  <button
                    onClick={() => setBasecodeEditor(null)}
                    disabled={basecodeSaving}
                    className="px-3 py-1.5 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 disabled:opacity-50 transition"
                  >
                    Cancel
                  </button>
                </div>
              </div>
            )}
            {(basecodeFiles || []).length === 0 && !basecodeEditor ? (
              <div className="text-xs text-gray-400">No base code.</div>
            ) : (
              !basecodeEditor && (
                <div className="space-y-1">
                  {basecodeFiles.map((file) => (
                    <div key={file.name} className="flex items-center justify-between gap-3">
                      <div className="flex items-center gap-2 min-w-0">
                        <code className="text-xs text-gray-700 break-all">{file.name}</code>
                        <span className="text-xs text-gray-400">{formatSize(file.size)}</span>
                      </div>
                      <div className="flex items-center gap-3 shrink-0">
                        <button
                          onClick={() => editBasecodeFile(file)}
                          className="text-xs text-blue-600 hover:text-blue-700 font-medium"
                        >
                          Edit
                        </button>
                        <button
                          onClick={() => deleteBasecodeFile(file)}
                          className="text-xs text-red-600 hover:text-red-700 font-medium"
                        >
                          Delete
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )
            )}
          </div>
          {pairs.length === 0 ? (
            <div className="px-6 py-4 text-sm text-gray-400">
              {plag?.run ? (plag.run.status === 'done' ? 'No similar pairs found.' : '') : 'No plagiarism run yet.'}
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-gray-50 text-gray-600">
                  <tr>
                    <th className="text-left px-6 py-3 font-medium">Student A</th>
                    <th className="text-left px-6 py-3 font-medium">Student B</th>
                    <th className="text-right px-6 py-3 font-medium">Similarity</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {pairs.map((pair, idx) => {
                    const pct = similarityPercent(pair.similarity);
                    const hot = pct > 70;
                    return (
                      <tr
                        key={`${pair.studentA}-${pair.studentB}-${idx}`}
                        className={hot ? 'bg-red-50' : ''}
                      >
                        <td className={`px-6 py-3 ${hot ? 'text-red-700 font-medium' : 'text-gray-900'}`}>
                          {pair.nameA || pair.studentA}
                        </td>
                        <td className={`px-6 py-3 ${hot ? 'text-red-700 font-medium' : 'text-gray-900'}`}>
                          {pair.nameB || pair.studentB}
                        </td>
                        <td className={`px-6 py-3 text-right font-medium ${hot ? 'text-red-700' : 'text-gray-900'}`}>
                          {pct.toFixed(1)}%
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
        )}
      </main>
    </div>
  );
}

// VIEWABLE_EXTENSIONS can be reviewed inline in the browser; everything else
// (pdf, docx, xlsx, images, video…) stays download-only.
const VIEWABLE_EXTENSIONS = new Set([
  '.java', '.py', '.txt', '.md', '.csv', '.json', '.xml', '.html', '.css',
  '.js', '.ts', '.c', '.h', '.cpp', '.hpp', '.cs', '.go', '.rs', '.sql',
  '.yaml', '.yml', '.toml', '.ini', '.sh', '.log', '.tex',
]);

function isViewable(name) {
  const dot = name.lastIndexOf('.');
  return dot >= 0 && VIEWABLE_EXTENSIONS.has(name.slice(dot).toLowerCase());
}

function SubmissionDetail({ detail }) {
  const [downloadError, setDownloadError] = useState(null);
  const [viewing, setViewing] = useState({}); // name -> text content, true while loading
  const submission = detail?.submission || {};
  const files = detail?.files || submission.files || [];
  const runs = detail?.runs || [];

  const download = (name) => {
    setDownloadError(null);
    adminDownloadSubmissionFile(submission.id, name).catch((err) => setDownloadError(err.message));
  };

  const toggleView = (name) => {
    setDownloadError(null);
    if (viewing[name]) {
      setViewing((prev) => {
        const next = { ...prev };
        delete next[name];
        return next;
      });
      return;
    }
    setViewing((prev) => ({ ...prev, [name]: true }));
    adminGetSubmissionFileText(submission.id, name)
      .then((text) => setViewing((prev) => ({ ...prev, [name]: text })))
      .catch((err) => {
        setViewing((prev) => {
          const next = { ...prev };
          delete next[name];
          return next;
        });
        setDownloadError(err.message);
      });
  };

  return (
    <div className="space-y-3">
      <div className="text-xs text-gray-500">
        Submitted {submission.submittedAt ? new Date(submission.submittedAt).toLocaleString() : '—'}
        {submission.isLate && ` · late (cap ${submission.capPercent}%)`}
      </div>

      <div>
        <div className="text-xs font-semibold uppercase tracking-wide text-gray-500 mb-1">Files</div>
        <div className="space-y-2">
          {files.map((f) => (
            <div key={f.name}>
              <div className="flex flex-wrap items-center gap-2">
                <code className="text-xs text-gray-700">{f.name}</code>
                <span className="text-xs text-gray-400">{formatSize(f.size)}</span>
                {isViewable(f.name) && (
                  <button
                    onClick={() => toggleView(f.name)}
                    className="text-xs text-blue-600 hover:text-blue-700 font-medium"
                  >
                    {viewing[f.name] ? 'Hide' : 'View'}
                  </button>
                )}
                <button
                  onClick={() => download(f.name)}
                  className="text-xs text-blue-600 hover:text-blue-700 font-medium"
                >
                  Download
                </button>
              </div>
              {viewing[f.name] && (
                <pre className="mt-1 max-h-96 overflow-auto text-xs bg-gray-50 border border-gray-200 rounded-lg p-3 whitespace-pre-wrap break-all font-mono text-gray-800">
                  {viewing[f.name] === true ? 'Loading...' : viewing[f.name]}
                </pre>
              )}
            </div>
          ))}
        </div>
        {downloadError && <div className="text-xs text-red-600 mt-1">{downloadError}</div>}
      </div>

      <div>
        <div className="text-xs font-semibold uppercase tracking-wide text-gray-500 mb-1">
          Test Runs ({runs.length})
        </div>
        {runs.length === 0 ? (
          <div className="text-sm text-gray-400">No runs for this submission yet.</div>
        ) : (
          <div className="space-y-2">
            {runs.map((run) => (
              <div key={run.id} className="bg-white border border-gray-200 rounded-lg px-3 py-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium text-gray-900">{run.testName}</span>
                  <VisibilityBadge visibility={run.visibility} />
                  <RunStatus run={run} />
                  <span className="text-xs text-gray-400">
                    by {run.triggeredBy}
                    {run.finishedAt ? ` · ${new Date(run.finishedAt).toLocaleString()}` : ''}
                  </span>
                </div>
                {run.output && (
                  <pre className="mt-2 max-h-48 overflow-auto text-xs bg-gray-50 border border-gray-100 rounded p-2 whitespace-pre-wrap">
                    {run.output}
                  </pre>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

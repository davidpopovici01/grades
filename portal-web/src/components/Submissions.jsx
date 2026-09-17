import { useEffect, useRef, useState } from 'react';
import { getSubmissions, getSubmissionDetail, uploadSubmissionSlot, deleteSubmissionSlot, downloadSubmissionSlot, submitSubmission, runMyTests, getSubmissionFileText, downloadSubmissionFile } from '../api';
import { formatSize, isViewable } from '../format';
import { resolveCourse } from '../hooks/useCourseSelection';
import { TestRunStatus } from './TestRunStatus';

const LANG_BADGE = {
  java: 'bg-blue-50 text-blue-700',
  python: 'bg-green-50 text-green-700',
};

function LanguageBadge({ language }) {
  return (
    <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${LANG_BADGE[language] || 'bg-gray-100 text-gray-600'}`}>
      {language}
    </span>
  );
}

function formatDue(dueAt) {
  if (!dueAt) return 'No due date';
  const d = new Date(dueAt);
  if (isNaN(d)) return 'No due date';
  return `Due ${d.toLocaleString()} (server time)`;
}

function SlotStatus({ state, size }) {
  if (state === 'staged') {
    return (
      <span className="px-1.5 py-0.5 rounded bg-blue-50 text-blue-700 text-xs font-medium">
        staged{size != null ? ` · ${formatSize(size)}` : ''}
      </span>
    );
  }
  if (state === 'submitted') {
    return (
      <span className="px-1.5 py-0.5 rounded bg-green-50 text-green-700 text-xs font-medium">
        submitted{size != null ? ` · ${formatSize(size)}` : ''}
      </span>
    );
  }
  return (
    <span className="px-1.5 py-0.5 rounded bg-gray-100 text-gray-500 text-xs font-medium">
      not uploaded
    </span>
  );
}

// slotParts splits a required-file entry into its concrete alternatives:
// "report.docx/pdf" → key "report.docx", names ["report.docx", "report.pdf"].
function slotParts(display) {
  const parts = display.split('/');
  const first = parts[0].trim();
  const dot = first.lastIndexOf('.');
  const stem = dot > 0 ? first.slice(0, dot) : first;
  const names = [first];
  for (const part of parts.slice(1)) {
    const ext = part.trim().replace(/^\./, '');
    if (ext) names.push(`${stem}.${ext}`);
  }
  return { key: first, names, accept: names.map((n) => n.slice(n.lastIndexOf('.'))).join(',') };
}

function AssignmentCard({ assignment }) {
  const [summary, setSummary] = useState(assignment);
  const [slotFiles, setSlotFiles] = useState({});
  const [slotErrors, setSlotErrors] = useState({});
  const [slotBusy, setSlotBusy] = useState(null);
  const [submitting, setSubmitting] = useState(false);
  const [testing, setTesting] = useState(false);
  const [cooldown, setCooldown] = useState(assignment.cooldownRemainingSeconds || 0);
  const [error, setError] = useState(null);
  const [notice, setNotice] = useState(null);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [detail, setDetail] = useState(null);
  const [viewing, setViewing] = useState({}); // "submissionId:name" -> text, true while loading
  const slotInputs = useRef({});
  const pollTimer = useRef(null);

  useEffect(() => () => {
    if (pollTimer.current) clearTimeout(pollTimer.current);
  }, []);

  useEffect(() => {
    if (cooldown <= 0) return undefined;
    const t = setTimeout(() => setCooldown((c) => Math.max(0, c - 1)), 1000);
    return () => clearTimeout(t);
  }, [cooldown]);

  const applyDetail = (data) => {
    if (!data) return;
    setDetail(data);
    setSummary((prev) => {
      const next = { ...prev, ...(data.assignment || {}) };
      if (data.submissions) {
        const latest = data.submissions[0];
        next.latestSubmission = latest
          ? { id: latest.id, submittedAt: latest.submittedAt, isLate: latest.isLate }
          : null;
      }
      if (data.draft) next.draftFiles = data.draft.files;
      if (data.latestPublicRuns) {
        // The detail response has no aggregate counts; derive them so the
        // header badge refreshes while polling.
        let passed = 0;
        let failed = 0;
        for (const run of data.latestPublicRuns) {
          if (run.status !== 'done') continue;
          passed += run.passed || 0;
          failed += run.failed || 0;
        }
        next.publicPassed = passed;
        next.publicFailed = failed;
      }
      return next;
    });
    if (data.assignment) {
      setCooldown((c) => Math.max(c, data.assignment.cooldownRemainingSeconds || 0));
    }
  };

  const refresh = () =>
    getSubmissionDetail(summary.id)
      .then(applyDetail)
      .catch(() => {});

  // Load the detail once so slots show staged/submitted state and sizes.
  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onSlotPick = (name, names, fileList) => {
    const file = fileList?.[0] || null;
    setSlotErrors((prev) => ({ ...prev, [name]: null }));
    setNotice(null);
    setError(null);
    if (!file) return;
    if (!names.some((n) => n === file.name)) {
      setSlotErrors((prev) => ({ ...prev, [name]: `File must be named exactly ${names.join(' or ')} (upper/lowercase matters) — rename it first.` }));
      return;
    }
    if (summary.maxFileBytes > 0 && file.size > summary.maxFileBytes) {
      setSlotErrors((prev) => ({
        ...prev,
        [name]: `${formatSize(file.size)} — max ${formatSize(summary.maxFileBytes)} per file`,
      }));
      return;
    }
    setSlotFiles((prev) => ({ ...prev, [name]: file }));
  };

  const onSlotUpload = (name, key) => {
    const file = slotFiles[name];
    if (!file) return;
    setSlotBusy(name);
    setError(null);
    setNotice(null);
    uploadSubmissionSlot(summary.id, key, file)
      .then((resp) => {
        setSlotFiles((prev) => {
          const next = { ...prev };
          delete next[name];
          return next;
        });
        const missing = resp?.draft?.missing || [];
        setNotice(
          missing.length > 0 && !summary.latestSubmission
            ? `${name} staged — still needed: ${missing.join(', ')}`
            : summary.latestSubmission
              ? `${name} staged — press Submit (or Submit and Test) to record a new version; other files are kept from your previous submission.`
              : `${name} staged — all files ready, press Submit to record your submission.`,
        );
        if (resp?.draft) {
          setSummary((prev) => ({ ...prev, draftFiles: resp.draft.files }));
          setDetail((prev) => (prev ? { ...prev, draft: resp.draft } : prev));
        }
        return refresh();
      })
      .catch((err) => {
        setSlotErrors((prev) => ({ ...prev, [name]: err.message }));
      })
      .finally(() => setSlotBusy(null));
  };

  const onSlotRemove = (name) => {
    setError(null);
    setNotice(null);
    deleteSubmissionSlot(summary.id, name)
      .then((resp) => {
        if (resp?.draft) {
          setSummary((prev) => ({ ...prev, draftFiles: resp.draft.files }));
          setDetail((prev) => (prev ? { ...prev, draft: resp.draft } : prev));
        }
      })
      .catch((err) => setError(err.message));
  };

  const onSlotDownload = (name) => {
    downloadSubmissionSlot(summary.id, name).catch((err) => setError(err.message));
  };

  const onFileDownload = (submissionId, name) => {
    setError(null);
    downloadSubmissionFile(submissionId, name).catch((err) => setError(err.message));
  };

  const toggleFileView = (submissionId, name) => {
    const key = `${submissionId}:${name}`;
    setError(null);
    if (viewing[key]) {
      setViewing((prev) => {
        const next = { ...prev };
        delete next[key];
        return next;
      });
      return;
    }
    setViewing((prev) => ({ ...prev, [key]: true }));
    getSubmissionFileText(submissionId, name)
      .then((text) => setViewing((prev) => ({ ...prev, [key]: text })))
      .catch((err) => {
        setViewing((prev) => {
          const next = { ...prev };
          delete next[key];
          return next;
        });
        setError(err.message);
      });
  };

  const onSubmit = (confirmLate = false) => {
    setSubmitting(true);
    setError(null);
    setNotice(null);
    submitSubmission(summary.id, confirmLate)
      .then((resp) => {
        setNotice(`Submission recorded (attempt ${resp?.submission?.attempt ?? '?'}).`);
        return refresh();
      })
      .catch((err) => {
        if (err.status === 409 && err.data?.late) {
          const penalty = 100 - (summary.lateCapPercent ?? 90);
          const due = err.data.dueAt ? ` (${new Date(err.data.dueAt).toLocaleString()})` : '';
          if (window.confirm(`This submission is past the due date${due} — a ${penalty}% late penalty applies. Submit anyway?`)) {
            return onSubmit(true);
          }
          return undefined;
        }
        if (err.status === 409 && err.data?.missing) {
          setError(`Still missing: ${err.data.missing.join(', ')}`);
          return undefined;
        }
        setError(err.message);
        return undefined;
      })
      .finally(() => setSubmitting(false));
  };

  const pollForResults = () => {
    let attempts = 0;
    const tick = () => {
      attempts += 1;
      getSubmissionDetail(summary.id)
        .then((data) => {
          applyDetail(data);
          const runs = data?.latestPublicRuns || [];
          // The grader runs tests one at a time: keep polling until every
          // queued/running run has reached a terminal state.
          const active = runs.some((r) => r.status === 'queued' || r.status === 'running');
          if ((runs.length > 0 && !active) || attempts >= 100) {
            setTesting(false);
            return;
          }
          pollTimer.current = setTimeout(tick, 3000);
        })
        .catch(() => {
          if (attempts >= 100) {
            setTesting(false);
          } else {
            pollTimer.current = setTimeout(tick, 3000);
          }
        });
    };
    pollTimer.current = setTimeout(tick, 3000);
  };

  const queueTests = () => {
    setTesting(true);
    setError(null);
    setNotice(null);
    runMyTests(summary.id)
      .then(() => {
        setNotice('Tests queued — results will appear shortly.');
        pollForResults();
      })
      .catch((err) => {
        setTesting(false);
        if (err.status === 429 && err.data?.retryAfter) {
          setCooldown(err.data.retryAfter);
          setError(err.message);
        } else {
          setError(err.message);
        }
      });
  };

  const onTest = () => {
    queueTests();
  };

  // onSubmitAndTest records the staged draft first, then queues tests against
  // the new submission — students pressing Test expect their staged files to be
  // the ones tested.
  const onSubmitAndTest = (confirmLate = false) => {
    setSubmitting(true);
    setError(null);
    setNotice(null);
    submitSubmission(summary.id, confirmLate)
      .then(() => {
        return refresh().then(() => {
          queueTests();
        });
      })
      .catch((err) => {
        if (err.status === 409 && err.data?.late) {
          const penalty = 100 - (summary.lateCapPercent ?? 90);
          const due = err.data.dueAt ? ` (${new Date(err.data.dueAt).toLocaleString()})` : '';
          if (window.confirm(`This submission is past the due date${due} — a ${penalty}% late penalty applies. Submit anyway?`)) {
            return onSubmitAndTest(true);
          }
          return undefined;
        }
        if (err.status === 409 && err.data?.missing) {
          setError(`Still missing: ${err.data.missing.join(', ')}`);
          return undefined;
        }
        setError(err.message);
        return undefined;
      })
      .finally(() => setSubmitting(false));
  };

  const toggleHistory = () => {
    setHistoryOpen((prev) => !prev);
  };

  const [overdue] = useState(
    () => Boolean(assignment.dueAt) && Date.parse(assignment.dueAt) < Date.now(),
  );

  const hasResults = (summary.publicPassed || 0) > 0 || (summary.publicFailed || 0) > 0;
  const hasTests = (summary.publicTestCount || 0) > 0;
  const latestRuns = detail?.latestPublicRuns || [];
  const testDisabled = testing || cooldown > 0 || !summary.isOpen || !summary.latestSubmission;

  const draftFiles = detail?.draft?.files || summary.draftFiles || [];
  const draftBySlot = new Map(draftFiles.map((f) => [(f.slot || f.name || '').toLowerCase(), f]));
  const expectedCount = (summary.expectedFilenames || []).length;
  // At least one staged file, and every required slot is either staged or was
  // part of a previous (always complete) submission.
  const allCovered =
    Boolean(summary.latestSubmission) ||
    (summary.expectedFilenames || []).every((n) => draftBySlot.has(n.toLowerCase()));
  const canSubmit = summary.isOpen && expectedCount > 0 && draftBySlot.size > 0 && allCovered;
  const stagedAny = draftBySlot.size > 0;
  // With staged files forming a valid submission, Test becomes "Submit and Test"
  // so the staged files are what actually gets tested.
  const submitAndTest = hasTests && canSubmit;

  const slotState = (display) => {
    const entry = draftBySlot.get(display.toLowerCase());
    if (entry) return { state: 'staged', size: entry.size, actualName: entry.name };
    if (summary.latestSubmission) return { state: 'submitted', size: null };
    return { state: 'missing', size: null };
  };

  return (
    <div className="px-6 py-4 space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-medium text-gray-900">{summary.title}</h3>
        <LanguageBadge language={summary.language} />
        {!summary.isOpen && (
          <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-500">Closed</span>
        )}
        {hasResults && (
          <span
            className={`px-2 py-0.5 rounded-full text-xs font-medium ${
              (summary.publicFailed || 0) === 0
                ? 'bg-green-50 text-green-700'
                : 'bg-amber-50 text-amber-700'
            }`}
          >
            {(summary.publicPassed || 0)} / {(summary.publicPassed || 0) + (summary.publicFailed || 0)} tests passed
          </span>
        )}
      </div>

      <div className="text-sm text-gray-500">{formatDue(summary.dueAt)}</div>

      {summary.isOpen && overdue && (
        <div className="text-sm text-amber-800 bg-amber-50 border border-amber-200 px-3 py-2 rounded-lg">
          This assignment is past due. Uploading now will mark your submission as late
          (capped at {summary.lateCapPercent ?? 90}% of the score).
        </div>
      )}

      <div className="text-xs text-gray-400">
        Max {formatSize(summary.maxFileBytes)} per file · {formatSize(summary.maxTotalBytes)} total
      </div>

      {summary.instructions && (
        <p className="text-sm text-gray-600 whitespace-pre-wrap">{summary.instructions}</p>
      )}

      {summary.latestSubmission && (
        <div className="text-xs text-gray-500">
          Last submitted {new Date(summary.latestSubmission.submittedAt).toLocaleString()}
          {summary.latestSubmission.isLate && (
            <span className="ml-1 px-1.5 py-0.5 rounded bg-amber-50 text-amber-700 font-medium">late</span>
          )}
        </div>
      )}

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

      <div className="space-y-2">
        {(summary.expectedFilenames || []).map((name) => {
          const { state, size, actualName } = slotState(name);
          const parts = slotParts(name);
          const picked = slotFiles[name];
          return (
            <div
              key={name}
              className="flex flex-wrap items-center justify-between gap-2 border border-gray-200 rounded-lg px-3 py-2"
            >
              <div className="flex flex-wrap items-center gap-2 min-w-0">
                <code className="px-1.5 py-0.5 bg-gray-50 border border-gray-200 rounded text-xs text-gray-700">
                  {name}
                </code>
                <SlotStatus state={state} size={size} />
                {state === 'staged' && actualName && actualName !== parts.key && (
                  <span className="text-xs text-gray-400">({actualName})</span>
                )}
                {slotErrors[name] && (
                  <span className="text-xs text-red-600">{slotErrors[name]}</span>
                )}
              </div>
              <div className="flex items-center gap-2">
                {state === 'staged' && !picked && (
                  <>
                    <button
                      onClick={() => onSlotDownload(parts.key)}
                      className="text-xs text-blue-600 hover:text-blue-700 font-medium"
                    >
                      Download
                    </button>
                    <button
                      onClick={() => onSlotRemove(parts.key)}
                      disabled={slotBusy !== null}
                      className="text-xs text-red-600 hover:text-red-700 font-medium"
                    >
                      Remove
                    </button>
                  </>
                )}
                <input
                  ref={(el) => { slotInputs.current[name] = el; }}
                  type="file"
                  accept={parts.accept}
                  className="hidden"
                  onChange={(e) => {
                    onSlotPick(name, parts.names, e.target.files);
                    e.target.value = '';
                  }}
                />
                {picked ? (
                  <>
                    <span className="text-xs text-gray-500 max-w-40 truncate">{picked.name}</span>
                    <button
                      onClick={() => onSlotUpload(name, parts.key)}
                      disabled={!summary.isOpen || slotBusy !== null || testing}
                      className="px-3 py-1 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
                    >
                      {slotBusy === name ? 'Uploading...' : 'Upload'}
                    </button>
                  </>
                ) : (
                  <button
                    onClick={() => slotInputs.current[name]?.click()}
                    disabled={!summary.isOpen || slotBusy !== null || testing}
                    className="px-3 py-1 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 disabled:opacity-50 transition"
                  >
                    {state === 'missing' ? 'Choose File' : 'Replace'}
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <button
          onClick={() => onSubmit(false)}
          disabled={!canSubmit || submitting || testing || slotBusy !== null}
          title={canSubmit ? 'Record the submission with the staged files' : summary.latestSubmission ? 'Stage at least one changed file first' : 'Stage all required files first'}
          className="px-3 py-1.5 text-sm font-medium text-white bg-green-600 rounded-lg hover:bg-green-700 disabled:opacity-50 transition"
        >
          {submitting ? 'Submitting...' : summary.latestSubmission ? 'Submit New Version' : 'Submit'}
        </button>
        {stagedAny && !canSubmit && (
          <span className="text-xs text-gray-400">
            {draftBySlot.size} of {expectedCount} files staged
          </span>
        )}
        {stagedAny && canSubmit && summary.latestSubmission && (
          <span className="text-xs text-gray-400">
            unstaged files are kept from your previous submission
          </span>
        )}
        {hasTests && (
          submitAndTest ? (
            <button
              onClick={() => onSubmitAndTest(false)}
              disabled={submitting || testing || cooldown > 0 || slotBusy !== null}
              title="Record the staged files as a new submission and run the public tests on it"
              className="px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
            >
              {submitting ? 'Submitting...' : testing ? 'Testing...' : cooldown > 0 ? `Submit and Test (${cooldown}s)` : 'Submit and Test'}
            </button>
          ) : (
            <button
              onClick={onTest}
              disabled={testDisabled}
              title={!summary.latestSubmission ? 'Upload your files first' : ''}
              className="px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
            >
              {testing ? 'Testing...' : cooldown > 0 ? `Test (${cooldown}s)` : 'Test'}
            </button>
          )
        )}
        <button
          onClick={toggleHistory}
          className="px-3 py-1.5 text-sm font-medium text-gray-600 hover:text-gray-900"
        >
          {historyOpen ? 'Hide History' : 'History'}
        </button>
      </div>

      {hasTests && (
        <div className="border border-gray-200 rounded-lg px-4 py-3 space-y-2">
          <div className="flex items-center gap-2">
            <span className="text-xs font-semibold uppercase tracking-wide text-gray-500">
              Tests
            </span>
            {testing && (
              <span className="inline-flex items-center gap-1.5 text-xs text-gray-400">
                <span className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
                running
              </span>
            )}
          </div>
          {!detail ? (
            <div className="text-sm text-gray-400">Loading...</div>
          ) : latestRuns.length === 0 ? (
            <div className="text-sm text-gray-400">
              {canSubmit
                ? 'Press Submit and Test to record your files and run the public tests.'
                : summary.latestSubmission
                  ? 'Not tested yet — press Test to run the public tests.'
                  : 'Submit your files, then press Test to run the public tests.'}
            </div>
          ) : (
            <>
              <div className="divide-y divide-gray-100">
                {latestRuns.map((run, idx) => (
                  <div key={`${run.testName}-${idx}`} className="py-1.5 text-sm">
                    <div className="flex items-center justify-between">
                      <span className="text-gray-700">{run.testName}</span>
                      <TestRunStatus run={run} />
                    </div>
                    {run.output && (
                      <pre className="mt-1 max-h-48 overflow-auto text-xs bg-gray-50 border border-gray-100 rounded p-2 whitespace-pre-wrap break-all font-mono text-gray-800">
                        {run.output}
                      </pre>
                    )}
                  </div>
                ))}
              </div>
              {latestRuns.length < (summary.publicTestCount || 0) && (
                <div className="text-xs text-gray-400">
                  {latestRuns.length} of {summary.publicTestCount} tests have run
                </div>
              )}
            </>
          )}
        </div>
      )}

      {historyOpen && (
        <div className="border-t border-gray-100 pt-3 space-y-3">
          {!detail ? (
            <div className="text-sm text-gray-400">Loading...</div>
          ) : (
            <div>
              <div className="text-xs font-semibold uppercase tracking-wide text-gray-500 mb-1">
                Attempts ({(detail.submissions || []).length})
              </div>
              {(detail.submissions || []).length === 0 ? (
                <div className="text-sm text-gray-400">No submissions yet.</div>
              ) : (
                <div className="divide-y divide-gray-100">
                  {(detail.submissions || []).map((s) => (
                    <div key={s.id} className="py-1.5 text-sm">
                      <div className="flex items-center gap-2">
                        <span className="text-gray-700">{new Date(s.submittedAt).toLocaleString()}</span>
                        {s.isLate && (
                          <span className="px-1.5 py-0.5 rounded bg-amber-50 text-amber-700 text-xs font-medium">
                            late · capped at {s.capPercent}%
                          </span>
                        )}
                      </div>
                      <div className="mt-1 space-y-1">
                        {(s.files || []).map((f) => {
                          const key = `${s.id}:${f.name}`;
                          return (
                            <div key={f.name}>
                              <div className="flex flex-wrap items-center gap-2">
                                <code className="text-xs text-gray-700">{f.name}</code>
                                <span className="text-xs text-gray-400">{formatSize(f.size)}</span>
                                {isViewable(f.name) && (
                                  <button
                                    onClick={() => toggleFileView(s.id, f.name)}
                                    className="text-xs text-blue-600 hover:text-blue-700 font-medium"
                                  >
                                    {viewing[key] ? 'Hide' : 'View'}
                                  </button>
                                )}
                                <button
                                  onClick={() => onFileDownload(s.id, f.name)}
                                  className="text-xs text-blue-600 hover:text-blue-700 font-medium"
                                >
                                  Download
                                </button>
                              </div>
                              {viewing[key] && (
                                <pre className="mt-1 max-h-96 overflow-auto text-xs bg-gray-50 border border-gray-200 rounded-lg p-3 whitespace-pre-wrap break-all font-mono text-gray-800">
                                  {viewing[key] === true ? 'Loading...' : viewing[key]}
                                </pre>
                              )}
                            </div>
                          );
                        })}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function Submissions({ selectedCourseKey }) {
  const [courses, setCourses] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    getSubmissions()
      .then((data) => setCourses(data?.courses || []))
      .catch((err) => setError(err.message));
  }, []);

  if (error) {
    return (
      <div className="text-center py-20">
        <div className="text-red-600 mb-2">Failed to load submissions</div>
        <div className="text-sm text-gray-500">{error}</div>
      </div>
    );
  }

  if (!courses) {
    return (
      <div className="text-center py-20">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  if (courses.length === 0) {
    return (
      <div className="text-center py-20">
        <div className="text-gray-500">No assignments posted yet.</div>
      </div>
    );
  }

  const course = resolveCourse(courses, selectedCourseKey);

  return (
    <div className="space-y-6">
      <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
        <div className="px-6 py-4 border-b border-gray-100">
          <h2 className="font-semibold text-gray-800">{course.courseName}</h2>
          <div className="text-sm text-gray-500">
            {course.courseYearName ? `${course.courseYearName} · ` : ''}{course.termName}
          </div>
        </div>
        {(course.assignments || []).length === 0 ? (
          <div className="px-6 py-4 text-sm text-gray-400">No assignments for this course.</div>
        ) : (
          <div className="divide-y divide-gray-100">
            {course.assignments.map((assignment) => (
              <AssignmentCard key={assignment.id} assignment={assignment} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

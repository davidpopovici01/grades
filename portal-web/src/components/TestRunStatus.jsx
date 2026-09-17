// TestRunStatus renders the state of one test run: a spinner while
// queued/running, the pass ratio when done, and a red label for
// error/timeout outcomes.
export function TestRunStatus({ run }) {
  if (run.status === 'queued' || run.status === 'running') {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs text-gray-400">
        {run.status === 'running' && (
          <span className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
        )}
        {run.status}
      </span>
    );
  }
  if (run.status !== 'done') {
    return <span className="text-xs font-medium text-red-600">{run.status}</span>;
  }
  return (
    <span className={`text-xs font-medium ${run.failed > 0 ? 'text-amber-700' : 'text-green-700'}`}>
      {run.passed} / {run.passed + run.failed} tests passed
    </span>
  );
}

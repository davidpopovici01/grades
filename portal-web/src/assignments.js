// Assignment action classification mirroring the CLI's grades overview logic
// (studentStatusGroups / hasPendingRedo in internal/app/grades.go).

export function hasScore(a) {
  return a.score !== null && a.score !== undefined;
}

function rawPercent(a) {
  return (a.score / a.maxPoints) * 100;
}

// A redo still needs attention unless the work was redone up to the pass rate.
// The redo flag stays on the record after a redo, so the flag alone is not
// enough — a passing score clears it.
export function isPendingRedo(a) {
  const flags = a.flags || [];
  if (flags.includes('cheat')) return false;
  if (flags.includes('missing')) return false;
  if (flags.includes('pass')) return false;
  const passRate = a.passPercent;
  if (!passRate || passRate <= 0) return false;
  if (flags.includes('redo')) {
    if (!hasScore(a) || a.maxPoints <= 0) return true;
    return rawPercent(a) < passRate;
  }
  return hasScore(a) && a.maxPoints > 0 && rawPercent(a) < passRate;
}

// Returns 'missing' | 'redo' | 'late' for assignments the student still needs
// to act on, or null. Skips assignments before the overview cutoff and in
// categories hidden from the overview.
export function classifyAction(a) {
  if (a.isBeforeCutoff) return null;
  if (a.showInOverview === false) return null;
  const flags = a.flags || [];
  if (flags.includes('missing')) return 'missing';
  if (isPendingRedo(a)) return 'redo';
  if (flags.includes('late') && !hasScore(a)) return 'late';
  return null;
}

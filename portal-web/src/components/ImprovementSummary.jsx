function formatPercent(value) {
  if (value === null || value === undefined || isNaN(value)) return '—';
  return `${value.toFixed(1)}%`;
}

export function ImprovementSummary({ grades }) {
  if (!grades) return null;

  const total = grades.weightedTotal;
  const activeCount = grades.activeCategoryCount;
  const totalCategories = grades.categories?.length || 0;

  // Filter to assignments after cutoff (the ones that matter now)
  const relevantAssignments = grades.assignments?.filter((a) => !a.isBeforeCutoff) || [];
  const missing = relevantAssignments.filter((a) => a.flags?.includes('missing'));
  const redo = relevantAssignments.filter((a) => a.flags?.includes('redo'));
  const late = relevantAssignments.filter((a) => a.flags?.includes('late'));
  const belowPass = relevantAssignments.filter(
    (a) => a.score !== null && a.score !== undefined && a.currentPercent < (a.passPercent || 60)
  );

  // Find lowest active category
  const activeCategories = grades.categories?.filter((c) => c.included) || [];
  const lowestCategory = activeCategories.length > 0
    ? activeCategories.reduce((min, c) => (c.score < min.score ? c : min), activeCategories[0])
    : null;

  // Determine status and color
  let status = 'good';
  let bgClass = 'bg-green-50 border-green-200';
  let textClass = 'text-green-800';
  let heading = 'Doing well!';

  if (total < 60) {
    status = 'critical';
    bgClass = 'bg-red-50 border-red-200';
    textClass = 'text-red-800';
    heading = 'Needs attention';
  } else if (total < 75) {
    status = 'warning';
    bgClass = 'bg-amber-50 border-amber-200';
    textClass = 'text-amber-800';
    heading = 'Room for improvement';
  } else if (total < 85) {
    status = 'caution';
    bgClass = 'bg-blue-50 border-blue-200';
    textClass = 'text-blue-800';
    heading = 'On track';
  }

  // Build actionable bullets
  const bullets = [];

  if (missing.length > 0) {
    const first = missing[0];
    bullets.push(
      <span key="missing">
        You have <strong>{missing.length} missing assignment{missing.length > 1 ? 's' : ''}</strong>.
        {missing.length === 1
          ? ` Start with "${first.title}" in ${first.categoryName}.`
          : ' Start with the most recent one to catch up quickly.'}
      </span>
    );
  }

  if (redo.length > 0) {
    const first = redo[0];
    bullets.push(
      <span key="redo">
        You have <strong>{redo.length} assignment{redo.length > 1 ? 's' : ''} to redo</strong>.
        {redo.length === 1
          ? ` Resubmit "${first.title}" to improve your ${first.categoryName} score.`
          : ' Resubmit them to boost your category scores.'}
      </span>
    );
  }

  if (late.length > 0 && missing.length === 0) {
    bullets.push(
      <span key="late">
        You have <strong>{late.length} late assignment{late.length > 1 ? 's' : ''}</strong>.
        Late work still counts — make sure everything is submitted.
      </span>
    );
  }

  if (lowestCategory && lowestCategory.score < 70 && missing.length === 0 && redo.length === 0) {
    bullets.push(
      <span key="lowest">
        Your lowest active category is <strong>{lowestCategory.categoryName}</strong> at{' '}
        {formatPercent(lowestCategory.score)}. Focus your next efforts here to boost your overall grade.
      </span>
    );
  }

  if (belowPass.length > 0 && missing.length === 0 && redo.length === 0) {
    const count = belowPass.length;
    bullets.push(
      <span key="below">
        <strong>{count} assignment{count > 1 ? 's are' : ' is'}</strong> currently below the pass threshold.
        {count === 1
          ? ` Review "${belowPass[0].title}" and ask for help if needed.`
          : ' Review them and consider asking for extra help.'}
      </span>
    );
  }

  // Fallback positive message
  if (bullets.length === 0) {
    if (total >= 85) {
      bullets.push(
        <span key="great">
          Great work! Your grade is <strong>{formatPercent(total)}</strong>. Keep maintaining your strong
          performance across all categories.
        </span>
      );
    } else {
      bullets.push(
        <span key="steady">
          Your grade is <strong>{formatPercent(total)}</strong>. No urgent issues right now — keep up
          steady work in all categories to improve further.
        </span>
      );
    }
  }

  return (
    <div className={`rounded-xl border p-5 ${bgClass}`}>
      <div className="mb-3">
        <h3 className={`font-semibold text-lg ${textClass}`}>{heading}</h3>
      </div>
      <ul className="space-y-2">
        {bullets.map((bullet, idx) => (
          <li key={idx} className={`flex items-start gap-2 text-sm ${textClass}`}>
            <span className={`mt-1.5 w-1.5 h-1.5 rounded-full shrink-0 ${
              status === 'critical' ? 'bg-red-400' :
              status === 'warning' ? 'bg-amber-400' :
              status === 'caution' ? 'bg-blue-400' :
              'bg-green-400'
            }`} />
            {bullet}
          </li>
        ))}
      </ul>
    </div>
  );
}

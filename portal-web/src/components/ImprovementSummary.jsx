import { classifyAction } from '../assignments';

function formatPercent(value) {
  if (value === null || value === undefined || isNaN(value)) return '—';
  return `${value.toFixed(1)}%`;
}

export function ImprovementSummary({ grades }) {
  if (!grades) return null;

  const total = grades.weightedTotal;

  if (total === null || total === undefined) {
    return (
      <div className="rounded-xl border p-5 bg-gray-50 border-gray-200">
        <h3 className="font-semibold text-lg text-gray-700">No grades yet</h3>
        <p className="text-sm text-gray-500 mt-1">
          Your grade will appear here once your teacher starts entering scores.
        </p>
      </div>
    );
  }

  // Classify actionable assignments the same way the CLI's grades overview
  // does: after the cutoff, in overview-visible categories only.
  const assignments = grades.assignments || [];
  const missing = assignments.filter((a) => classifyAction(a) === 'missing');
  const redo = assignments.filter((a) => classifyAction(a) === 'redo');
  const late = assignments.filter((a) => classifyAction(a) === 'late');

  // Find lowest active category
  const activeCategories = grades.categories?.filter((c) => c.included && c.showInOverview !== false) || [];
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

  // Build actionable bullets. Missing/redo/late assignments are listed
  // row-by-row in the ActionItems table right below, so this summary only
  // adds guidance the table doesn't already show.
  const bullets = [];

  if (lowestCategory && lowestCategory.score < 70 && missing.length === 0 && redo.length === 0) {
    bullets.push(
      <span key="lowest">
        Your lowest active category is <strong>{lowestCategory.categoryName}</strong> at{' '}
        {formatPercent(lowestCategory.score)}. Focus your next efforts here to boost your overall grade.
      </span>
    );
  }

  // Fallback message
  if (bullets.length === 0) {
    if (missing.length + redo.length + late.length > 0) {
      bullets.push(
        <span key="actions">
          See the action items below to get back on track.
        </span>
      );
    } else if (total >= 85) {
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

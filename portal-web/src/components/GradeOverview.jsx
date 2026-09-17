import { ActionItems } from './ActionItems';
import { ImprovementSummary } from './ImprovementSummary';

const FLAG_COLORS = {
  missing: 'bg-red-100 text-red-700 border-red-200',
  late: 'bg-yellow-100 text-yellow-700 border-yellow-200',
  redo: 'bg-orange-100 text-orange-700 border-orange-200',
  pass: 'bg-green-100 text-green-700 border-green-200',
  cheat: 'bg-gray-100 text-gray-700 border-gray-200',
};

function formatPercent(value) {
  if (value === null || value === undefined || isNaN(value)) return '—';
  return `${value.toFixed(1)}%`;
}

// Mirrors the admin panel's pending logic (pendingAction in
// internal/portalserver/handlers.go): missing/redo chips only show while they
// still need action, and an unflagged failing score still gets a redo chip.
function visibleFlags(a) {
  const flags = a.flags || [];
  const has = (f) => flags.includes(f);
  const pendingMissing = has('missing');
  let pendingRedo = false;
  if (!pendingMissing && !has('cheat') && !has('pass') && a.passPercent > 0 && a.maxPoints > 0) {
    const passing = a.score !== null && a.score !== undefined && (a.score / a.maxPoints) * 100 >= a.passPercent;
    pendingRedo = has('redo') ? !passing : (a.score !== null && a.score !== undefined && !passing);
  }
  const display = flags.filter((f) => {
    if (f === 'missing') return pendingMissing;
    if (f === 'redo') return pendingRedo;
    return true;
  });
  if (pendingRedo && !display.includes('redo')) display.push('redo');
  return display;
}

export function GradeOverview({ grades, children }) {
  if (!grades) {
    return (
      <div className="space-y-6">
        <div className="text-center py-20">
          <div className="text-gray-500">No grades available for this course.</div>
        </div>
        {children}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Hero Card */}
      <div className="bg-white rounded-xl border border-gray-200 p-6 shadow-sm">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <h2 className="text-lg font-semibold text-gray-900">
              {grades.firstName} {grades.lastName}
              {grades.chineseName && (
                <span className="text-gray-500 font-normal ml-2">({grades.chineseName})</span>
              )}
            </h2>
            <p className="text-sm text-gray-500 mt-0.5">
              {grades.courseName} · {grades.termName}
              {grades.sections?.length > 0 && ` · ${grades.sections.join(', ')}`}
            </p>
          </div>
          <div className="text-right">
            <div className="text-3xl font-bold text-blue-700">
              {formatPercent(grades.weightedTotal)}
              {grades.letterGrade && (
                <span className="ml-2 text-2xl text-gray-500">({grades.letterGrade})</span>
              )}
            </div>
            <div className="text-xs text-gray-500 uppercase tracking-wide">{grades.weightedTotalLabel}</div>
          </div>
        </div>

      </div>

      {/* Improvement Summary */}
      <ImprovementSummary grades={grades} />

      {/* Action Items */}
      <ActionItems grades={grades} />

      {/* Category Totals */}
      {grades.categories?.length > 0 && (
        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-gray-100">
            <h3 className="font-semibold text-gray-800">Categories</h3>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-gray-50 text-gray-600">
                <tr>
                  <th className="text-left px-6 py-3 font-medium">Category</th>
                  <th className="text-right px-6 py-3 font-medium">Weight</th>
                  <th className="text-right px-6 py-3 font-medium">Score</th>
                  <th className="text-right px-6 py-3 font-medium">Status</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {grades.categories.map((cat) => (
                  <tr key={cat.categoryId} className={cat.included ? '' : 'text-gray-400'}>
                    <td className="px-6 py-3">
                      {cat.categoryName}
                      {cat.dropLowest > 0 && (
                        <span className="text-xs text-gray-400 ml-2">drops lowest {cat.dropLowest}</span>
                      )}
                    </td>
                    <td className="px-6 py-3 text-right">
                      {cat.hasWeight ? `${cat.weightPercent.toFixed(0)}%` : '—'}
                    </td>
                    <td className="px-6 py-3 text-right font-medium">
                      {formatPercent(cat.score)}
                    </td>
                    <td className="px-6 py-3 text-right">
                      {cat.included ? (
                        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-50 text-green-700">
                          Active
                        </span>
                      ) : (
                        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 text-gray-500">
                          Waiting
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {children}

      {/* Assignments */}
      {grades.assignments?.length > 0 && (
        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-gray-100">
            <h3 className="font-semibold text-gray-800">Assignments</h3>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-gray-50 text-gray-600">
                <tr>
                  <th className="text-left px-6 py-3 font-medium">Assignment</th>
                  <th className="text-left px-6 py-3 font-medium">Category</th>
                  <th className="text-right px-6 py-3 font-medium">Score</th>
                  <th className="text-right px-6 py-3 font-medium">%</th>
                  <th className="text-left px-6 py-3 font-medium">Flags</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {[...grades.assignments].sort((a, b) => b.assignmentId - a.assignmentId).map((a) => (
                  <tr
                    key={a.assignmentId}
                    className={a.isBeforeCutoff ? 'bg-gray-50 text-gray-400' : ''}
                  >
                    <td className="px-6 py-3">
                      <div className={`font-medium ${a.isBeforeCutoff ? 'text-gray-500' : 'text-gray-900'}`}>
                        {a.title}
                        {a.isBeforeCutoff && (
                          <span className="ml-2 text-xs font-normal text-gray-400">(before cutoff)</span>
                        )}
                      </div>
                    </td>
                    <td className="px-6 py-3 text-gray-600">{a.categoryName}</td>
                    <td className="px-6 py-3 text-right">
                      {a.score !== null && a.score !== undefined ? (
                        <span className="font-medium">{a.score}/{a.maxPoints}</span>
                      ) : (
                        <span className="text-gray-400">—</span>
                      )}
                    </td>
                    <td className="px-6 py-3 text-right">
                      <span className={`font-medium ${a.currentPercent >= (a.passPercent || 60) ? 'text-gray-900' : 'text-red-600'}`}>
                        {formatPercent(a.currentPercent)}
                      </span>
                    </td>
                    <td className="px-6 py-3">
                      <div className="flex flex-wrap gap-1">
                        {visibleFlags(a).map((flag) => (
                          <span
                            key={flag}
                            className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border ${FLAG_COLORS[flag] || 'bg-gray-100 text-gray-700 border-gray-200'}`}
                          >
                            {flag}
                          </span>
                        ))}
                        {a.dropped && (
                          <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 text-gray-500 border border-gray-200">
                            dropped
                          </span>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

    </div>
  );
}

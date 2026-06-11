import { useEffect, useState } from 'react';
import { getGrades } from '../api';
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

export function GradeOverview() {
  const [grades, setGrades] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    getGrades()
      .then(setGrades)
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="text-gray-500">Loading grades...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-center py-20">
        <div className="text-red-600 mb-2">Failed to load grades</div>
        <div className="text-sm text-gray-500">{error}</div>
      </div>
    );
  }

  if (!grades) return null;

  return (
    <div className="space-y-6">
      {/* Improvement Summary */}
      <ImprovementSummary grades={grades} />

      {/* Action Items */}
      <ActionItems grades={grades} />

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
                      <span className="text-xs text-gray-400 ml-2">({cat.schemeKey})</span>
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
                      <div className="text-xs text-gray-400">Max: {a.maxPoints} pts</div>
                    </td>
                    <td className="px-6 py-3 text-gray-600">{a.categoryName}</td>
                    <td className="px-6 py-3 text-right">
                      {a.score !== null && a.score !== undefined ? (
                        <span className="font-medium">{a.score.toFixed(1)}</span>
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
                        {a.flags?.map((flag) => (
                          <span
                            key={flag}
                            className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border ${FLAG_COLORS[flag] || 'bg-gray-100 text-gray-700 border-gray-200'}`}
                          >
                            {flag}
                          </span>
                        ))}
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

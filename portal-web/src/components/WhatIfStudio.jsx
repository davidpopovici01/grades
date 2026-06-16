import { useEffect, useMemo, useState } from 'react';
import { getGrades } from '../api';

function formatPercent(value) {
  if (value === null || value === undefined || isNaN(value)) return '—';
  return `${value.toFixed(1)}%`;
}

function assignmentHasEntry(item) {
  return item.score !== null && item.score !== undefined || (item.flags?.length > 0);
}

function countsTowardAverage(item) {
  return item.maxPoints > 0 && (item.score !== null && item.score !== undefined || item.flags?.length > 0);
}

function curvedPercent(raw, anchor, lift) {
  if (raw <= 0) return 0;
  return Math.pow(raw, lift) * Math.pow(100, 1 - lift);
}

function recordPercent(item) {
  if (item.score === null || item.score === undefined || item.maxPoints <= 0 || item.flags?.includes('missing')) {
    return 0;
  }
  const raw = (item.score / item.maxPoints) * 100;
  return curvedPercent(raw, item.anchor, item.lift);
}

function completionPercent(item) {
  if (!item.passPercent || item.passPercent <= 0) {
    return recordPercent(item);
  }
  if (item.flags?.includes('missing') || item.score === null || item.score === undefined || item.maxPoints <= 0) {
    return 0;
  }
  const raw = (item.score / item.maxPoints) * 100;
  if (raw < item.passPercent) {
    return 0;
  }
  let out = 100;
  if (item.flags?.includes('redo')) out -= 10;
  if (item.flags?.includes('late')) out -= 10;
  return Math.max(out, 0);
}

function effectiveAssignmentPercent(item) {
  if (item.passPercent > 0) {
    return completionPercent(item);
  }
  let percent = recordPercent(item);
  if (item.flags?.includes('pass')) {
    percent = 100;
    if (item.flags?.includes('redo')) percent -= 10;
    if (item.flags?.includes('late')) percent -= 10;
    percent = Math.max(percent, 0);
  }
  return percent;
}

function computeCategoryScore(items, schemeKey) {
  let hasEntry = false;

  switch (schemeKey) {
    case 'completion': {
      if (items.length === 0) return null;
      let total = 0;
      let count = 0;
      for (const item of items) {
        if (assignmentHasEntry(item)) hasEntry = true;
        if (!countsTowardAverage(item)) continue;
        total += effectiveAssignmentPercent(item);
        count++;
      }
      if (!hasEntry || count === 0) return null;
      return total / count;
    }
    case 'total-points': {
      let sum = 0;
      let maxTotal = 0;
      for (const item of items) {
        if (assignmentHasEntry(item)) hasEntry = true;
        if (!countsTowardAverage(item)) continue;
        maxTotal += item.maxPoints;
        sum += (effectiveAssignmentPercent(item) / 100) * item.maxPoints;
      }
      if (!hasEntry || maxTotal === 0) return null;
      return (sum / maxTotal) * 100;
    }
    default: {
      if (items.length === 0) return null;
      let total = 0;
      let count = 0;
      for (const item of items) {
        if (assignmentHasEntry(item)) hasEntry = true;
        if (!countsTowardAverage(item)) continue;
        total += effectiveAssignmentPercent(item);
        count++;
      }
      if (!hasEntry || count === 0) return null;
      return total / count;
    }
  }
}

function computeWeightedTotal(categories) {
  let totalWeight = 0;
  let weightedSum = 0;
  for (const cat of categories) {
    if (cat.hasWeight && cat._score !== null) {
      totalWeight += cat.weightPercent;
      weightedSum += (cat._score / 100) * cat.weightPercent;
    }
  }
  if (totalWeight === 0) return null;
  return (weightedSum / totalWeight) * 100;
}

export function WhatIfStudio() {
  const [grades, setGrades] = useState(null);
  const [loading, setLoading] = useState(true);
  const [scenarios, setScenarios] = useState([]);
  const [selectedCategory, setSelectedCategory] = useState('');
  const [scenarioTitle, setScenarioTitle] = useState('');
  const [scenarioMax, setScenarioMax] = useState('');
  const [scenarioScore, setScenarioScore] = useState('');

  useEffect(() => {
    getGrades()
      .then((g) => {
        setGrades(g);
        if (g.categories?.length > 0) {
          setSelectedCategory(String(g.categories[0].categoryId));
        }
      })
      .finally(() => setLoading(false));
  }, []);

  const projectedCategories = useMemo(() => {
    if (!grades) return [];
    return grades.categories.map((cat) => {
      const catScenarios = scenarios.filter((s) => s.categoryId === cat.categoryId);
      const baseAssignments = grades.assignments
        .filter((a) => a.categoryId === cat.categoryId)
        .map((a) => ({
          ...a,
          score: a.score ?? null,
          maxPoints: a.maxPoints,
          anchor: a.anchor,
          lift: a.lift,
          passPercent: a.passPercent,
          flags: a.flags || [],
        }));

      const withScenarios = [...baseAssignments];
      for (const s of catScenarios) {
        withScenarios.push({
          ...s,
          score: s._percent !== null ? (s._percent / 100) * s.maxPoints : null,
          maxPoints: s.maxPoints,
          anchor: s.anchor ?? 100,
          lift: s.lift ?? 1,
          passPercent: s.passPercent ?? null,
          flags: s.flags || [],
        });
      }

      return {
        ...cat,
        _baseScore: computeCategoryScore(baseAssignments, cat.schemeKey),
        _score: computeCategoryScore(withScenarios, cat.schemeKey),
        _scenarioCount: catScenarios.length,
      };
    });
  }, [grades, scenarios]);

  const projectedTotal = useMemo(() => {
    return computeWeightedTotal(projectedCategories);
  }, [projectedCategories]);

  const addScenario = () => {
    const max = parseFloat(scenarioMax);
    const score = parseFloat(scenarioScore);
    if (!scenarioTitle.trim() || !selectedCategory || isNaN(max) || max <= 0) return;

    const category = grades.categories.find((c) => c.categoryId === parseInt(selectedCategory));

    setScenarios((prev) => [
      ...prev,
      {
        id: `scenario-${Date.now()}`,
        title: scenarioTitle.trim(),
        categoryId: parseInt(selectedCategory),
        categoryName: category?.categoryName || '',
        maxPoints: max,
        _percent: isNaN(score) ? null : (score / max) * 100,
        anchor: 100,
        lift: 1,
        passPercent: category?.defaultPassPercent > 0 ? category.defaultPassPercent : null,
        flags: [],
      },
    ]);
    setScenarioTitle('');
    setScenarioMax('');
    setScenarioScore('');
  };

  const removeScenario = (id) => {
    setScenarios((prev) => prev.filter((s) => s.id !== id));
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  if (!grades) return null;

  return (
    <div className="space-y-6">
      {/* Projected Total */}
      <div className="bg-white rounded-xl border border-gray-200 p-6 shadow-sm">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold text-gray-900">What-If Studio</h2>
            <p className="text-sm text-gray-500">See how future assignments affect your grade</p>
          </div>
          <div className="text-right">
            <div className="text-3xl font-bold text-blue-700">{formatPercent(projectedTotal)}</div>
            <div className="text-xs text-gray-500 uppercase tracking-wide">Projected Total</div>
          </div>
        </div>
      </div>

      {/* Add Scenario */}
      <div className="bg-white rounded-xl border border-gray-200 p-6 shadow-sm">
        <h3 className="font-semibold text-gray-800 mb-4">Add Scenario Assignment</h3>
        <div className="grid grid-cols-1 sm:grid-cols-4 gap-3">
          <select
            value={selectedCategory}
            onChange={(e) => setSelectedCategory(e.target.value)}
            className="px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            {grades.categories.map((cat) => (
              <option key={cat.categoryId} value={cat.categoryId}>
                {cat.categoryName}
              </option>
            ))}
          </select>
          <input
            type="text"
            placeholder="Title"
            value={scenarioTitle}
            onChange={(e) => setScenarioTitle(e.target.value)}
            className="px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <input
            type="number"
            placeholder="Max points"
            value={scenarioMax}
            onChange={(e) => setScenarioMax(e.target.value)}
            className="px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <input
            type="number"
            placeholder="Your score"
            value={scenarioScore}
            onChange={(e) => setScenarioScore(e.target.value)}
            className="px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>
        <button
          onClick={addScenario}
          disabled={!scenarioTitle.trim() || !selectedCategory || !scenarioMax}
          className="mt-3 bg-blue-600 hover:bg-blue-700 text-white font-medium py-2 px-4 rounded-lg transition disabled:opacity-50 disabled:cursor-not-allowed text-sm"
        >
          Add Scenario
        </button>
      </div>

      {/* Scenario List */}
      {scenarios.length > 0 && (
        <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-gray-100">
            <h3 className="font-semibold text-gray-800">Scenario Assignments</h3>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-gray-50 text-gray-600">
                <tr>
                  <th className="text-left px-6 py-3 font-medium">Title</th>
                  <th className="text-left px-6 py-3 font-medium">Category</th>
                  <th className="text-right px-6 py-3 font-medium">Max</th>
                  <th className="text-right px-6 py-3 font-medium">%</th>
                  <th className="text-right px-6 py-3 font-medium"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {scenarios.map((s) => (
                  <tr key={s.id} className="bg-blue-50/50">
                    <td className="px-6 py-3 font-medium text-gray-900">{s.title}</td>
                    <td className="px-6 py-3 text-gray-600">{s.categoryName}</td>
                    <td className="px-6 py-3 text-right">{s.maxPoints}</td>
                    <td className="px-6 py-3 text-right font-medium">
                      {s._percent !== null ? formatPercent(s._percent) : '—'}
                    </td>
                    <td className="px-6 py-3 text-right">
                      <button
                        onClick={() => removeScenario(s.id)}
                        className="text-red-600 hover:text-red-700 text-xs font-medium"
                      >
                        Remove
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Projected Categories */}
      <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
        <div className="px-6 py-4 border-b border-gray-100">
          <h3 className="font-semibold text-gray-800">Projected Categories</h3>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-gray-50 text-gray-600">
              <tr>
                <th className="text-left px-6 py-3 font-medium">Category</th>
                <th className="text-right px-6 py-3 font-medium">Weight</th>
                <th className="text-right px-6 py-3 font-medium">Original</th>
                <th className="text-right px-6 py-3 font-medium">Projected</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {projectedCategories.map((cat) => (
                <tr key={cat.categoryId}>
                  <td className="px-6 py-3">
                    {cat.categoryName}
                    {cat._scenarioCount > 0 && (
                      <span className="ml-2 text-xs text-blue-600 font-medium">
                        +{cat._scenarioCount} scenario{cat._scenarioCount > 1 ? 's' : ''}
                      </span>
                    )}
                  </td>
                  <td className="px-6 py-3 text-right">
                    {cat.hasWeight ? `${cat.weightPercent.toFixed(0)}%` : '—'}
                  </td>
                  <td className="px-6 py-3 text-right text-gray-500">
                    {cat._baseScore !== null ? formatPercent(cat._baseScore) : '—'}
                  </td>
                  <td className="px-6 py-3 text-right font-medium">
                    {cat._score !== null ? (
                      <span className={cat._score > (cat._baseScore || 0) ? 'text-green-600' : cat._score < (cat._baseScore || 0) ? 'text-red-600' : 'text-gray-900'}>
                        {formatPercent(cat._score)}
                      </span>
                    ) : (
                      <span className="text-gray-400">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

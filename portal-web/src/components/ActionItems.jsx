import { classifyAction } from '../assignments';

export function ActionItems({ grades }) {
  if (!grades?.assignments?.length) return null;

  const actionable = grades.assignments
    .map((a) => ({ assignment: a, action: classifyAction(a) }))
    .filter((item) => item.action !== null);

  if (actionable.length === 0) return null;

  const items = actionable.map((item) => ({ ...item.assignment, action: item.action }));

  const FLAG_STYLES = {
    missing: 'bg-red-50 text-red-700 border-red-200',
    redo: 'bg-orange-50 text-orange-700 border-orange-200',
    late: 'bg-yellow-50 text-yellow-700 border-yellow-200',
  };

  const FLAG_LABELS = {
    missing: 'Missing',
    redo: 'Redo',
    late: 'Late',
  };

  return (
    <div className="bg-amber-50 rounded-xl border border-amber-200 shadow-sm overflow-hidden">
      <div className="px-6 py-4 border-b border-amber-200">
        <h3 className="font-semibold text-amber-900">
          Action Items
          <span className="ml-2 text-xs font-normal text-amber-700">
            ({items.length} to address)
          </span>
        </h3>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="bg-amber-100/50 text-amber-800">
            <tr>
              <th className="text-left px-6 py-3 font-medium">Assignment</th>
              <th className="text-left px-6 py-3 font-medium">Category</th>
              <th className="text-left px-6 py-3 font-medium">Action</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-amber-100">
            {items.sort((a, b) => b.assignmentId - a.assignmentId).map((item) => (
              <tr key={item.assignmentId + item.action}>
                <td className="px-6 py-3 font-medium text-gray-900">
                  {item.title}
                </td>
                <td className="px-6 py-3 text-gray-600">{item.categoryName}</td>
                <td className="px-6 py-3">
                  <span
                    className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border ${FLAG_STYLES[item.action]}`}
                  >
                    {FLAG_LABELS[item.action]}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { adminGetPairDiff } from '../api';

// changedSpan finds the differing middle of two lines via common prefix and
// suffix, for highlighting inside a "changed" row.
function changedSpan(left, right) {
  let start = 0;
  while (start < left.length && start < right.length && left[start] === right[start]) start += 1;
  let endL = left.length;
  let endR = right.length;
  while (endL > start && endR > start && left[endL - 1] === right[endR - 1]) {
    endL -= 1;
    endR -= 1;
  }
  return { left: [start, endL], right: [start, endR] };
}

function HighlightedLine({ text, span, tone }) {
  if (!span || span[0] === span[1]) return <>{text}</>;
  return (
    <>
      {text.slice(0, span[0])}
      <span className={tone}>{text.slice(span[0], span[1])}</span>
      {text.slice(span[1])}
    </>
  );
}

const rowStyles = {
  same: { left: '', right: '' },
  changed: { left: 'bg-red-50', right: 'bg-green-50' },
  removed: { left: 'bg-red-50', right: '' },
  added: { left: '', right: 'bg-green-50' },
};

function DiffFile({ file }) {
  return (
    <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
      <div className="px-4 py-3 border-b border-gray-100 flex items-center justify-between gap-2">
        <code className="text-sm text-gray-800 break-all">{file.name}</code>
        <div className="flex items-center gap-2 shrink-0">
          {file.onlyIn && (
            <span className="text-xs text-gray-400">only in {file.onlyIn === 'a' ? 'student A' : 'student B'}</span>
          )}
          <span className="text-xs font-medium text-gray-700">{(file.similarity * 100).toFixed(1)}%</span>
        </div>
      </div>
      {file.tooLarge ? (
        <div className="px-4 py-3 text-sm text-gray-400">
          File too large to align line by line; similarity was scored by shared lines only.
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs font-mono">
            <tbody>
              {(file.rows || []).map((row, idx) => {
                const styles = rowStyles[row.tag] || rowStyles.same;
                const span = row.tag === 'changed' ? changedSpan(row.left, row.right) : null;
                return (
                  <tr key={idx}>
                    <td className="w-10 px-2 py-0.5 text-right text-gray-400 select-none border-r border-gray-100">
                      {row.leftNo || ''}
                    </td>
                    <td className={`px-3 py-0.5 whitespace-pre-wrap break-all ${styles.left}`}>
                      <HighlightedLine text={row.left} span={span?.left} tone="bg-red-200 rounded-sm" />
                    </td>
                    <td className="w-10 px-2 py-0.5 text-right text-gray-400 select-none border-r border-gray-100">
                      {row.rightNo || ''}
                    </td>
                    <td className={`px-3 py-0.5 whitespace-pre-wrap break-all ${styles.right}`}>
                      <HighlightedLine text={row.right} span={span?.right} tone="bg-green-200 rounded-sm" />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// AdminPlagPairDiff shows the side-by-side line diff of two students' latest
// submissions, as computed by the built-in diff check.
export function AdminPlagPairDiff() {
  const { id, a, b } = useParams();
  const navigate = useNavigate();
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!sessionStorage.getItem('adminToken')) {
      navigate('/admin/login');
      return;
    }
    adminGetPairDiff(id, a, b)
      .then((d) => setData(d))
      .catch((err) => {
        if (err.status === 401) {
          sessionStorage.removeItem('adminToken');
          navigate('/admin/login');
          return;
        }
        setError(err.message);
      })
      .finally(() => setLoading(false));
  }, [id, a, b, navigate]);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-50">
      <main className="max-w-6xl mx-auto px-4 py-6 space-y-4">
        <div className="flex items-center justify-between gap-2">
          <Link to={`/admin/submissions/${id}`} className="text-sm text-blue-600 hover:text-blue-700 font-medium">
            &larr; Back to assignment
          </Link>
        </div>
        {error && (
          <div className="text-sm text-red-700 bg-red-50 border border-red-200 px-3 py-2 rounded-lg">{error}</div>
        )}
        {data && (
          <>
            <h1 className="text-lg font-semibold text-gray-800">
              {data.nameA || `Student ${a}`} &harr; {data.nameB || `Student ${b}`}
            </h1>
            {data.files.length === 0 ? (
              <div className="text-sm text-gray-400">No comparable text files.</div>
            ) : (
              data.files.map((file) => <DiffFile key={file.name} file={file} />)
            )}
          </>
        )}
      </main>
    </div>
  );
}

import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  adminListCourses,
  adminListMaterials,
  adminUploadMaterials,
  adminDeleteMaterial,
  adminRenameMaterial,
  adminMoveMaterial,
  adminCreateCategory,
  adminRenameCategory,
  adminReorderCategories,
  adminDeleteCategory,
} from '../api';
import { formatSize } from '../format';
import { useCourseSelection } from '../hooks/useCourseSelection';
import { AdminNav } from './AdminNav';

const GENERAL = ''; // category id for uncategorized files

export function AdminMaterials() {
  const [courses, setCourses] = useState(null);
  const { selectedKey, selected, setSelectedKey } = useCourseSelection(courses);
  const [structure, setStructure] = useState({ files: [], categories: [] });
  const [error, setError] = useState(null);
  const [notice, setNotice] = useState(null);
  const [uploading, setUploading] = useState(0);
  const [newCategory, setNewCategory] = useState('');
  const [renamingCat, setRenamingCat] = useState(null); // {id, value}
  const [renamingFile, setRenamingFile] = useState(null); // {categoryId, name, value}
  const [uploadTarget, setUploadTarget] = useState(GENERAL);
  const fileInput = useRef(null);
  const navigate = useNavigate();

  const refresh = useCallback((course) => {
    return adminListMaterials(course.courseYearId, course.termId)
      .then((data) => setStructure({ files: data?.files || [], categories: data?.categories || [] }))
      .catch((err) => setError(err.message));
  }, []);

  useEffect(() => {
    if (!sessionStorage.getItem('adminToken')) {
      navigate('/admin/login');
      return;
    }
    adminListCourses()
      .then((data) => {
        setCourses(data?.courses || []);
      })
      .catch((err) => {
        if (err.status === 401) {
          sessionStorage.removeItem('adminToken');
          navigate('/admin/login');
          return;
        }
        setError(err.message);
      });
  }, [navigate]);

  useEffect(() => {
    if (selected) refresh(selected);
  }, [selected, refresh]);

  // Runs an action, then refreshes; shows the server's error message on failure.
  const run = (promise) => {
    setError(null);
    setNotice(null);
    return promise
      .then(() => refresh(selected))
      .catch((err) => setError(err.message));
  };

  const addCategory = () => {
    const name = newCategory.trim();
    if (!name || !selected) return;
    setNewCategory('');
    run(adminCreateCategory(selected.courseYearId, selected.termId, name));
  };

  const commitRenameCat = () => {
    if (!renamingCat?.value.trim()) {
      setRenamingCat(null);
      return;
    }
    run(adminRenameCategory(selected.courseYearId, selected.termId, renamingCat.id, renamingCat.value.trim()));
    setRenamingCat(null);
  };

  const moveCategory = (id, delta) => {
    const ids = structure.categories.map((c) => c.id);
    const idx = ids.indexOf(id);
    const swap = idx + delta;
    if (idx < 0 || swap < 0 || swap >= ids.length) return;
    [ids[idx], ids[swap]] = [ids[swap], ids[idx]];
    run(adminReorderCategories(selected.courseYearId, selected.termId, ids));
  };

  const commitRenameFile = () => {
    if (!renamingFile?.value.trim() || renamingFile.value.trim() === renamingFile.name) {
      setRenamingFile(null);
      return;
    }
    run(adminRenameMaterial(selected.courseYearId, selected.termId, renamingFile.categoryId, renamingFile.name, renamingFile.value.trim()));
    setRenamingFile(null);
  };

  const onUploadPicked = (e) => {
    const files = Array.from(e.target.files || []);
    e.target.value = '';
    if (files.length === 0 || !selected) return;
    setError(null);
    setNotice(null);
    setUploading(files.length);
    adminUploadMaterials(selected.courseYearId, selected.termId, uploadTarget, files)
      .then((data) => {
        if (data?.failed?.length > 0) {
          setError(`Failed: ${data.failed.map((f) => `${f.name} (${f.error})`).join(', ')}`);
        } else {
          setNotice(`Uploaded ${data?.uploaded?.length || 0} file(s).`);
        }
        return refresh(selected);
      })
      .catch((err) => setError(err.message))
      .finally(() => setUploading(0));
  };

  if (!courses) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  const { files, categories } = structure;

  return (
    <div className="min-h-screen bg-gray-50">
      <AdminNav courses={courses} selectedKey={selectedKey} onSelectCourse={setSelectedKey} />
      <main className="max-w-5xl mx-auto px-4 py-6 space-y-4">
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

        <input
          ref={fileInput}
          type="file"
          multiple
          className="hidden"
          onChange={onUploadPicked}
        />

        <CategorySection
          title="General"
          files={files}
          categoryId={GENERAL}
          isGeneral
          renamingFile={renamingFile}
          setRenamingFile={setRenamingFile}
          commitRenameFile={commitRenameFile}
          onMove={(file, to) => run(adminMoveMaterial(selected.courseYearId, selected.termId, GENERAL, to, file))}
          onDelete={(file) => {
            if (window.confirm(`Delete "${file}"?`)) {
              run(adminDeleteMaterial(selected.courseYearId, selected.termId, GENERAL, file));
            }
          }}
          onUpload={() => { setUploadTarget(GENERAL); fileInput.current?.click(); }}
          uploading={uploading}
          categories={categories}
        />

        {categories.map((cat, idx) => (
          <CategorySection
            key={cat.id}
            title={cat.name}
            files={cat.files}
            categoryId={cat.id}
            renamingCat={renamingCat}
            setRenamingCat={setRenamingCat}
            commitRenameCat={commitRenameCat}
            onMoveUp={idx > 0 ? () => moveCategory(cat.id, -1) : null}
            onMoveDown={idx < categories.length - 1 ? () => moveCategory(cat.id, 1) : null}
            onDeleteCategory={() => {
              if (window.confirm(`Delete category "${cat.name}"?`)) {
                run(adminDeleteCategory(selected.courseYearId, selected.termId, cat.id));
              }
            }}
            renamingFile={renamingFile}
            setRenamingFile={setRenamingFile}
            commitRenameFile={commitRenameFile}
            onMove={(file, to) => run(adminMoveMaterial(selected.courseYearId, selected.termId, cat.id, to, file))}
            onDelete={(file) => {
              if (window.confirm(`Delete "${file}"?`)) {
                run(adminDeleteMaterial(selected.courseYearId, selected.termId, cat.id, file));
              }
            }}
            onUpload={() => { setUploadTarget(cat.id); fileInput.current?.click(); }}
            uploading={uploading}
            categories={categories}
          />
        ))}

        {files.length === 0 && categories.length === 0 && (
          <div className="bg-white rounded-xl border border-gray-200 px-6 py-12 text-center text-gray-500 shadow-sm">
            No materials uploaded for this course yet.
          </div>
        )}

        <div className="bg-white rounded-xl border border-gray-200 p-4 shadow-sm flex gap-2">
          <input
            type="text"
            value={newCategory}
            onChange={(e) => setNewCategory(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && addCategory()}
            placeholder="New category name (e.g. Unit 1)"
            className="flex-1 px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <button
            onClick={addCategory}
            disabled={!newCategory.trim() || !selected}
            className="px-3 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
          >
            Add Category
          </button>
        </div>
      </main>
    </div>
  );
}

function CategorySection({
  title,
  files,
  categoryId,
  isGeneral,
  renamingCat,
  setRenamingCat,
  commitRenameCat,
  onMoveUp,
  onMoveDown,
  onDeleteCategory,
  renamingFile,
  setRenamingFile,
  commitRenameFile,
  onMove,
  onDelete,
  onUpload,
  uploading,
  categories,
}) {
  const editingThisCat = renamingCat?.id === categoryId;

  return (
    <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
      <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between gap-2">
        {editingThisCat ? (
          <input
            autoFocus
            type="text"
            value={renamingCat.value}
            onChange={(e) => setRenamingCat({ id: categoryId, value: e.target.value })}
            onBlur={commitRenameCat}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commitRenameCat();
              if (e.key === 'Escape') setRenamingCat(null);
            }}
            className="px-2 py-1 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        ) : (
          <h2
            className={`font-semibold text-gray-800 ${isGeneral ? '' : 'cursor-text'}`}
            title={isGeneral ? '' : 'Click to rename'}
            onClick={isGeneral ? undefined : () => setRenamingCat({ id: categoryId, value: title })}
          >
            {title}
          </h2>
        )}
        <div className="flex items-center gap-2">
          {onMoveUp && (
            <button onClick={onMoveUp} title="Move up" className="px-2 py-1 text-sm text-gray-500 hover:text-gray-800 border border-gray-200 rounded">
              ↑
            </button>
          )}
          {onMoveDown && (
            <button onClick={onMoveDown} title="Move down" className="px-2 py-1 text-sm text-gray-500 hover:text-gray-800 border border-gray-200 rounded">
              ↓
            </button>
          )}
          <button
            onClick={onUpload}
            disabled={uploading > 0}
            className="px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition"
          >
            {uploading > 0 ? `Uploading ${uploading}...` : 'Upload'}
          </button>
          {!isGeneral && (
            <button
              onClick={onDeleteCategory}
              disabled={files.length > 0}
              title={files.length > 0 ? 'Move or delete the files first' : 'Delete category'}
              className="px-3 py-1.5 text-sm font-medium text-red-600 hover:text-red-700 disabled:opacity-40"
            >
              Delete
            </button>
          )}
        </div>
      </div>
      {files.length === 0 ? (
        <div className="px-6 py-4 text-sm text-gray-400">No files.</div>
      ) : (
        <div className="divide-y divide-gray-100">
          {files.map((file) => {
            const editingThisFile = renamingFile?.categoryId === categoryId && renamingFile?.name === file.name;
            return (
              <div key={file.name} className="flex items-center justify-between px-6 py-3 gap-3">
                <div className="min-w-0">
                  {editingThisFile ? (
                    <input
                      autoFocus
                      type="text"
                      value={renamingFile.value}
                      onChange={(e) => setRenamingFile({ categoryId, name: file.name, value: e.target.value })}
                      onBlur={commitRenameFile}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') commitRenameFile();
                        if (e.key === 'Escape') setRenamingFile(null);
                      }}
                      className="px-2 py-1 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                    />
                  ) : (
                    <div
                      className="text-sm font-medium text-gray-900 break-all cursor-text"
                      title="Click to rename"
                      onClick={() => setRenamingFile({ categoryId, name: file.name, value: file.name })}
                    >
                      {file.name}
                    </div>
                  )}
                  <div className="text-xs text-gray-400">
                    {formatSize(file.size)} · {new Date(file.modified).toLocaleString()}
                  </div>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  <select
                    value=""
                    onChange={(e) => e.target.value !== '' && onMove(file.name, e.target.value === GENERAL ? GENERAL : e.target.value)}
                    className="px-2 py-1 border border-gray-300 rounded-lg text-xs text-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  >
                    <option value="" disabled>Move to…</option>
                    {categoryId !== GENERAL && <option value={GENERAL}>General</option>}
                    {categories.filter((c) => c.id !== categoryId).map((c) => (
                      <option key={c.id} value={c.id}>{c.name}</option>
                    ))}
                  </select>
                  <button
                    onClick={() => onDelete(file.name)}
                    className="text-sm text-red-600 hover:text-red-700 font-medium"
                  >
                    Delete
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

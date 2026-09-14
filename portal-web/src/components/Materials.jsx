import { useEffect, useState } from 'react';
import { getMaterials, materialDownloadURL } from '../api';
import { formatSize } from '../format';

function FileRow({ course, categoryId, file }) {
  return (
    <a
      href={materialDownloadURL(course.courseYearId, course.termId, categoryId, file.name)}
      className="flex items-center justify-between px-6 py-3 hover:bg-gray-50 transition"
    >
      <span className="text-sm font-medium text-blue-700 break-all">{file.name}</span>
      <span className="text-xs text-gray-400 whitespace-nowrap pl-4">
        {formatSize(file.size)} · {new Date(file.modified).toLocaleDateString()}
      </span>
    </a>
  );
}

function FileGroup({ course, categoryId, title, files }) {
  if (!files || files.length === 0) return null;
  return (
    <div>
      {title && (
        <div className="px-6 py-2 bg-gray-50 border-b border-gray-100 text-xs font-semibold uppercase tracking-wide text-gray-500">
          {title}
        </div>
      )}
      <div className="divide-y divide-gray-100">
        {files.map((file) => (
          <FileRow key={file.name} course={course} categoryId={categoryId} file={file} />
        ))}
      </div>
    </div>
  );
}

export function Materials() {
  const [courses, setCourses] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    getMaterials()
      .then((data) => setCourses(data?.courses || []))
      .catch((err) => setError(err.message));
  }, []);

  if (error) {
    return (
      <div className="text-center py-20">
        <div className="text-red-600 mb-2">Failed to load materials</div>
        <div className="text-sm text-gray-500">{error}</div>
      </div>
    );
  }

  if (!courses) {
    return (
      <div className="text-center py-20">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  if (courses.length === 0) {
    return (
      <div className="text-center py-20">
        <div className="text-gray-500">No materials posted yet.</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {courses.map((course) => (
        <div
          key={`${course.courseYearId}-${course.termId}`}
          className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden"
        >
          <div className="px-6 py-4 border-b border-gray-100">
            <h2 className="font-semibold text-gray-800">{course.courseName}</h2>
            <div className="text-sm text-gray-500">
              {course.courseYearName ? `${course.courseYearName} · ` : ''}{course.termName}
            </div>
          </div>
          <FileGroup
            course={course}
            categoryId=""
            title={course.categories?.length > 0 ? 'General' : null}
            files={course.files}
          />
          {(course.categories || []).map((category) => (
            <FileGroup
              key={category.id}
              course={course}
              categoryId={category.id}
              title={category.name}
              files={category.files}
            />
          ))}
        </div>
      ))}
    </div>
  );
}

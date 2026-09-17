import { courseKey } from '../hooks/useCourseSelection';

export function CourseSelect({ courses, value, onChange }) {
  if (!courses || courses.length === 0) return null;
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      title="Selected course"
      className="max-w-56 px-3 py-1.5 border border-gray-300 rounded-lg text-sm text-gray-700 focus:outline-none focus:ring-2 focus:ring-blue-500"
    >
      {courses.map((c) => (
        <option key={courseKey(c)} value={courseKey(c)}>
          {c.courseName}{c.courseYearName ? ` · ${c.courseYearName}` : ''} · {c.termName}
        </option>
      ))}
    </select>
  );
}

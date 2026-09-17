import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { adminListCourses } from '../api';
import { useCourseSelection } from '../hooks/useCourseSelection';
import { AdminNav } from './AdminNav';
import { AdminCourseDetail } from './AdminCourseView';

export function AdminDashboard() {
  const [courses, setCourses] = useState(null);
  const { selectedKey, selected, setSelectedKey } = useCourseSelection(courses);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const navigate = useNavigate();

  const loadCourses = useCallback(() => {
    return adminListCourses()
      .then((data) => setCourses(data?.courses || []))
      .catch((err) => {
        if (err.status === 401) {
          sessionStorage.removeItem('adminToken');
          navigate('/admin/login');
          return;
        }
        setError(err.message);
      })
      .finally(() => setLoading(false));
  }, [navigate]);

  useEffect(() => {
    const token = sessionStorage.getItem('adminToken');
    if (!token) {
      navigate('/admin/login');
      return;
    }
    loadCourses();
  }, [navigate, loadCourses]);

  return (
    <div className="min-h-screen bg-gray-50">
      <AdminNav courses={courses} selectedKey={selectedKey} onSelectCourse={setSelectedKey} wide />
      {loading ? (
        <main className="max-w-6xl mx-auto px-4 py-6">
          <div className="text-center py-20 text-gray-500">Loading...</div>
        </main>
      ) : error ? (
        <main className="max-w-6xl mx-auto px-4 py-6">
          <div className="text-center py-20">
            <div className="text-red-600 mb-2">Failed to load courses</div>
            <div className="text-sm text-gray-500">{error}</div>
          </div>
        </main>
      ) : !selected ? (
        <main className="max-w-6xl mx-auto px-4 py-6">
          <div className="text-center py-20 text-gray-500">No published courses yet.</div>
        </main>
      ) : (
        <AdminCourseDetail
          key={selectedKey}
          courseYearId={selected.courseYearId}
          termId={selected.termId}
          onUnpublished={() => {
            setLoading(true);
            loadCourses();
          }}
        />
      )}
    </div>
  );
}

import { useEffect, useState } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { useAuth } from './hooks/useAuth';
import { getGrades } from './api';
import { Layout } from './components/Layout';
import { LoginForm } from './components/LoginForm';
import { GradeOverview } from './components/GradeOverview';
import { WhatIfStudio } from './components/WhatIfStudio';
import { ChangePassword } from './components/ChangePassword';
import { AdminLogin } from './components/AdminLogin';
import { AdminDashboard } from './components/AdminDashboard';
import { AdminCourseView } from './components/AdminCourseView';

function App() {
  const { user, loading, error, login, logout, checkAuth } = useAuth();
  const [gradesData, setGradesData] = useState(null);
  const [selectedCourseIdx, setSelectedCourseIdx] = useState(0);

  useEffect(() => {
    if (!user) {
      setGradesData(null);
      setSelectedCourseIdx(0);
      return;
    }
    getGrades()
      .then((data) => {
        setGradesData(data);
        setSelectedCourseIdx(mostRecentCourseIdx(data?.courses));
      })
      .catch(() => setGradesData(null));
  }, [user]);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  return (
    <BrowserRouter>
      <Routes>
        {/* Admin routes */}
        <Route path="/admin/login" element={<AdminLogin />} />
        <Route path="/admin" element={<AdminDashboard />} />
        <Route path="/admin/courses/:courseYearId/:termId" element={<AdminCourseView />} />

        {/* Student routes */}
        <Route
          path="*"
          element={
            !user ? (
              <LoginForm onLogin={login} error={error} />
            ) : user.mustChangePassword ? (
              <Layout user={user} onLogout={logout}>
                <div className="max-w-md mx-auto space-y-4">
                  <div className="text-sm text-amber-800 bg-amber-50 border border-amber-200 px-3 py-2 rounded-lg">
                    Your password was reset by your teacher. Choose a new password to continue.
                  </div>
                  <ChangePassword onChanged={checkAuth} />
                </div>
              </Layout>
            ) : (
              <Layout user={user} onLogout={logout}>
                <Routes>
                  <Route
                    path="/"
                    element={
                      <StudentHome
                        gradesData={gradesData}
                        selectedCourseIdx={selectedCourseIdx}
                        onSelectCourse={setSelectedCourseIdx}
                      />
                    }
                  />
                  <Route
                    path="/what-if"
                    element={
                      <WhatIfStudioWrapper
                        gradesData={gradesData}
                        selectedCourseIdx={selectedCourseIdx}
                        onSelectCourse={setSelectedCourseIdx}
                      />
                    }
                  />
                  <Route path="/change-password" element={<ChangePassword />} />
                  <Route path="*" element={<Navigate to="/" replace />} />
                </Routes>
              </Layout>
            )
          }
        />
      </Routes>
    </BrowserRouter>
  );
}

function mostRecentCourseIdx(courses) {
  if (!courses || courses.length === 0) return 0;
  let best = 0;
  let bestTime = -Infinity;
  courses.forEach((c, idx) => {
    const t = Date.parse(c.publishedAt);
    if (!isNaN(t) && t > bestTime) {
      bestTime = t;
      best = idx;
    }
  });
  return best;
}

function StudentHome({ gradesData, selectedCourseIdx, onSelectCourse }) {
  if (!gradesData) {
    return (
      <div className="text-center py-20">
        <div className="text-gray-500">No grade data available.</div>
      </div>
    );
  }

  const courses = gradesData.courses || [];
  if (courses.length === 0) {
    return (
      <div className="text-center py-20">
        <div className="text-gray-500">No published courses found.</div>
      </div>
    );
  }

  const selected = courses[selectedCourseIdx] || courses[0];

  return (
    <div className="space-y-6">
      <CourseSelector
        courses={courses}
        selectedIdx={selectedCourseIdx}
        onSelect={onSelectCourse}
      />
      <GradeOverview grades={selected.snapshot} />
    </div>
  );
}

function WhatIfStudioWrapper({ gradesData, selectedCourseIdx, onSelectCourse }) {
  if (!gradesData) {
    return (
      <div className="text-center py-20">
        <div className="text-gray-500">No grade data available.</div>
      </div>
    );
  }

  const courses = gradesData.courses || [];
  if (courses.length === 0) {
    return (
      <div className="text-center py-20">
        <div className="text-gray-500">No published courses found.</div>
      </div>
    );
  }

  const selected = courses[selectedCourseIdx] || courses[0];

  return (
    <div className="space-y-6">
      <CourseSelector
        courses={courses}
        selectedIdx={selectedCourseIdx}
        onSelect={onSelectCourse}
      />
      <WhatIfStudio
        key={`${selected.courseYearId}-${selected.termId}`}
        grades={selected.snapshot}
      />
    </div>
  );
}

function CourseSelector({ courses, selectedIdx, onSelect }) {
  if (courses.length <= 1) return null;

  return (
    <div className="bg-white rounded-xl border border-gray-200 p-4 shadow-sm">
      <label className="block text-sm font-medium text-gray-700 mb-2">Course / Year</label>
      <select
        value={selectedIdx}
        onChange={(e) => onSelect(parseInt(e.target.value, 10))}
        className="w-full sm:w-auto px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
      >
        {courses.map((c, idx) => (
          <option key={`${c.courseYearId}-${c.termId}`} value={idx}>
            {c.courseName}{c.courseYearName ? ` · ${c.courseYearName}` : ''} · {c.termName}
          </option>
        ))}
      </select>
    </div>
  );
}

export default App;

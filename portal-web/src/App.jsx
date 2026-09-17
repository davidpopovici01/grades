import { useEffect, useState } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { useAuth } from './hooks/useAuth';
import { courseKey, readStoredCourseKey, writeStoredCourseKey } from './hooks/useCourseSelection';
import { getGrades } from './api';
import { Layout } from './components/Layout';
import { LoginForm } from './components/LoginForm';
import { GradeOverview } from './components/GradeOverview';
import { WhatIfStudio } from './components/WhatIfStudio';
import { ChangePassword } from './components/ChangePassword';
import { AdminLogin } from './components/AdminLogin';
import { AdminDashboard } from './components/AdminDashboard';
import { AdminCourseView } from './components/AdminCourseView';
import { AdminMaterials } from './components/AdminMaterials';
import { AdminSubmissions } from './components/AdminSubmissions';
import { AdminAssignmentDetail } from './components/AdminAssignmentDetail';
import { AdminActivity } from './components/AdminActivity';
import { Materials } from './components/Materials';
import { Submissions } from './components/Submissions';
import { UpcomingAssignments } from './components/UpcomingAssignments';

function App() {
  const { user, loading, error, login, logout, checkAuth } = useAuth();
  const [gradesData, setGradesData] = useState(null);
  const [selectedCourseKey, setSelectedCourseKey] = useState(readStoredCourseKey);
  // The same SPA serves both subdomains; on the materials host, land on /materials.
  const isMaterialsHost = window.location.hostname.startsWith('materials.');

  const selectCourse = (key) => {
    setSelectedCourseKey(key);
    writeStoredCourseKey(key);
  };

  const courses = gradesData?.courses || [];
  const matchedIdx = courses.findIndex((c) => courseKey(c) === selectedCourseKey);
  const selectedCourseIdx = matchedIdx >= 0 ? matchedIdx : mostRecentCourseIdx(courses);
  const headerCourseKey = courses.length > 0 ? courseKey(courses[selectedCourseIdx]) : '';

  // Drop stale grades when the signed-in user changes (login or logout): a
  // state adjustment during render, kept out of the effect below.
  const [prevUser, setPrevUser] = useState(user);
  if (prevUser !== user) {
    setPrevUser(user);
    setGradesData(null);
  }

  useEffect(() => {
    if (!user) return;
    getGrades()
      .then((data) => setGradesData(data))
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
        <Route path="/admin/materials" element={<AdminMaterials />} />
        <Route path="/admin/submissions" element={<AdminSubmissions />} />
        <Route path="/admin/submissions/:id" element={<AdminAssignmentDetail />} />
        <Route path="/admin/activity" element={<AdminActivity />} />

        {/* Student routes */}
        <Route
          path="*"
          element={
            !user ? (
              <LoginForm onLogin={login} error={error} />
            ) : user.mustChangePassword ? (
              <Layout
                user={user}
                onLogout={logout}
                courses={courses}
                selectedCourseKey={headerCourseKey}
                onSelectCourse={selectCourse}
              >
                <div className="max-w-md mx-auto space-y-4">
                  <div className="text-sm text-amber-800 bg-amber-50 border border-amber-200 px-3 py-2 rounded-lg">
                    Your password was reset by your teacher. Choose a new password to continue.
                  </div>
                  <ChangePassword onChanged={checkAuth} />
                </div>
              </Layout>
            ) : (
              <Layout
                user={user}
                onLogout={logout}
                courses={courses}
                selectedCourseKey={headerCourseKey}
                onSelectCourse={selectCourse}
              >
                <Routes>
                  <Route
                    path="/"
                    element={
                      isMaterialsHost ? (
                        <Navigate to="/materials" replace />
                      ) : (
                        <StudentHome
                          gradesData={gradesData}
                          selectedCourseIdx={selectedCourseIdx}
                          selectedCourseKey={headerCourseKey}
                        />
                      )
                    }
                  />
                  <Route path="/materials" element={<Materials selectedCourseKey={selectedCourseKey} />} />
                  <Route path="/submissions" element={<Submissions selectedCourseKey={selectedCourseKey} />} />
                  <Route
                    path="/what-if"
                    element={
                      <WhatIfStudioWrapper
                        gradesData={gradesData}
                        selectedCourseIdx={selectedCourseIdx}
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

function StudentHome({ gradesData, selectedCourseIdx, selectedCourseKey }) {
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
      <GradeOverview grades={selected.snapshot}>
        <UpcomingAssignments selectedCourseKey={selectedCourseKey} />
      </GradeOverview>
    </div>
  );
}

function WhatIfStudioWrapper({ gradesData, selectedCourseIdx }) {
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
      <WhatIfStudio
        key={`${selected.courseYearId}-${selected.termId}`}
        grades={selected.snapshot}
      />
    </div>
  );
}

export default App;

import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { useAuth } from './hooks/useAuth';
import { Layout } from './components/Layout';
import { LoginForm } from './components/LoginForm';
import { GradeOverview } from './components/GradeOverview';
import { WhatIfStudio } from './components/WhatIfStudio';
import { ChangePassword } from './components/ChangePassword';

function App() {
  const { user, loading, error, login, logout } = useAuth();

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  if (!user) {
    return <LoginForm onLogin={login} error={error} />;
  }

  return (
    <BrowserRouter>
      <Layout user={user} onLogout={logout}>
        <Routes>
          <Route path="/" element={<GradeOverview />} />
          <Route path="/what-if" element={<WhatIfStudio />} />
          <Route path="/change-password" element={<ChangePassword />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Layout>
    </BrowserRouter>
  );
}

export default App;

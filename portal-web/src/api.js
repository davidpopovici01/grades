const API_BASE = '/api';

async function apiFetch(path, options = {}) {
  const res = await fetch(`${API_BASE}${path}`, {
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
    ...options,
  });

  if (res.status === 401 && !path.startsWith('/admin/')) {
    window.dispatchEvent(new CustomEvent('auth:unauthorized'));
  }

  const data = await res.json().catch(() => null);

  if (!res.ok) {
    const error = new Error(data?.error || `HTTP ${res.status}`);
    error.status = res.status;
    error.data = data;
    throw error;
  }

  return data;
}

export const login = (username, password) =>
  apiFetch('/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });

export const logout = () =>
  apiFetch('/logout', { method: 'POST' });

export const getMe = () =>
  apiFetch('/me');

export const getGrades = () =>
  apiFetch('/grades');

export const adminFetch = (path, options = {}) => {
  const token = sessionStorage.getItem('adminToken') || '';
  return apiFetch(path, {
    ...options,
    headers: {
      Authorization: `Bearer ${token}`,
      ...options.headers,
    },
  });
};

export const adminListCourses = () =>
  adminFetch('/admin/courses');

export const adminActivity = (limit = 100) =>
  adminFetch(`/admin/activity?limit=${limit}`);

export const adminListStudents = (courseYearId, termId) =>
  adminFetch(`/admin/courses/${courseYearId}/${termId}/students`);

export const adminResetPassword = (studentId) =>
  adminFetch(`/admin/students/${studentId}/reset-password`, { method: 'POST' });

export const adminUnpublishCourse = (courseYearId, termId) =>
  adminFetch(`/admin/courses/${courseYearId}/${termId}`, { method: 'DELETE' });

export const getMaterials = () =>
  apiFetch('/materials');

export const materialDownloadURL = (courseYearId, termId, categoryId, name) =>
  `${API_BASE}/materials/download/${courseYearId}/${termId}/${categoryId ? `${encodeURIComponent(categoryId)}/` : ''}${encodeURIComponent(name)}`;

const materialsQuery = (courseYearId, termId, extra = '') =>
  `courseYearId=${courseYearId}&termId=${termId}${extra}`;

export const adminListMaterials = (courseYearId, termId) =>
  adminFetch(`/admin/materials?${materialsQuery(courseYearId, termId)}`);

export const adminUploadMaterials = (courseYearId, termId, categoryId, files) => {
  const token = sessionStorage.getItem('adminToken') || '';
  const form = new FormData();
  for (const file of files) {
    form.append('file', file);
  }
  const category = categoryId ? `&category=${encodeURIComponent(categoryId)}` : '';
  // No Content-Type header: the browser sets the multipart boundary.
  return fetch(`${API_BASE}/admin/materials/upload?${materialsQuery(courseYearId, termId, category)}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { Authorization: `Bearer ${token}` },
    body: form,
  }).then(async (res) => {
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      const error = new Error(data?.error || `HTTP ${res.status}`);
      error.status = res.status;
      throw error;
    }
    return data;
  });
};

export const adminDeleteMaterial = (courseYearId, termId, categoryId, name) =>
  adminFetch(`/admin/materials/delete?${materialsQuery(courseYearId, termId, `&file=${encodeURIComponent(name)}${categoryId ? `&category=${encodeURIComponent(categoryId)}` : ''}`)}`, {
    method: 'DELETE',
  });

export const adminRenameMaterial = (courseYearId, termId, categoryId, file, newName) =>
  adminFetch(`/admin/materials/rename?${materialsQuery(courseYearId, termId)}`, {
    method: 'POST',
    body: JSON.stringify({ category: categoryId || '', file, newName }),
  });

export const adminMoveMaterial = (courseYearId, termId, fromCategory, toCategory, file) =>
  adminFetch(`/admin/materials/move?${materialsQuery(courseYearId, termId)}`, {
    method: 'POST',
    body: JSON.stringify({ fromCategory: fromCategory || '', toCategory: toCategory || '', file }),
  });

export const adminCreateCategory = (courseYearId, termId, name) =>
  adminFetch(`/admin/materials/categories?${materialsQuery(courseYearId, termId)}`, {
    method: 'POST',
    body: JSON.stringify({ name }),
  });

export const adminRenameCategory = (courseYearId, termId, id, name) =>
  adminFetch(`/admin/materials/categories/rename?${materialsQuery(courseYearId, termId)}`, {
    method: 'POST',
    body: JSON.stringify({ id, name }),
  });

export const adminReorderCategories = (courseYearId, termId, ids) =>
  adminFetch(`/admin/materials/categories/reorder?${materialsQuery(courseYearId, termId)}`, {
    method: 'POST',
    body: JSON.stringify({ ids }),
  });

export const adminDeleteCategory = (courseYearId, termId, id) =>
  adminFetch(`/admin/materials/categories?${materialsQuery(courseYearId, termId, `&id=${encodeURIComponent(id)}`)}`, {
    method: 'DELETE',
  });

// ---- Code submissions (student) ----

export const getSubmissions = () =>
  apiFetch('/submissions');

export const getSubmissionDetail = (assignmentId) =>
  apiFetch(`/submissions/${assignmentId}`);

export const uploadSubmissionFiles = (assignmentId, files, confirmLate = false) => {
  const form = new FormData();
  for (const file of files) {
    form.append('file', file);
  }
  if (confirmLate) {
    form.append('confirmLate', 'true');
  }
  // No Content-Type header: the browser sets the multipart boundary.
  return fetch(`${API_BASE}/submissions/${assignmentId}/files`, {
    method: 'POST',
    credentials: 'same-origin',
    body: form,
  }).then(async (res) => {
    if (res.status === 401) {
      window.dispatchEvent(new CustomEvent('auth:unauthorized'));
    }
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      const error = new Error(data?.error || `HTTP ${res.status}`);
      error.status = res.status;
      error.data = data;
      throw error;
    }
    return data;
  });
};

export const runMyTests = (assignmentId) =>
  apiFetch(`/submissions/${assignmentId}/test`, { method: 'POST' });

// uploadSubmissionSlot uploads one file into its per-file slot; the server
// stores it under the slot's canonical name regardless of the picked name.
export const uploadSubmissionSlot = (assignmentId, name, file, confirmLate = false) => {
  const form = new FormData();
  if (confirmLate) {
    form.append('confirmLate', 'true');
  }
  form.append('file', file);
  // No Content-Type header: the browser sets the multipart boundary.
  return fetch(`${API_BASE}/submissions/${assignmentId}/slot/${encodeURIComponent(name)}`, {
    method: 'POST',
    credentials: 'same-origin',
    body: form,
  }).then(async (res) => {
    if (res.status === 401) {
      window.dispatchEvent(new CustomEvent('auth:unauthorized'));
    }
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      const error = new Error(data?.error || `HTTP ${res.status}`);
      error.status = res.status;
      error.data = data;
      throw error;
    }
    return data;
  });
};

export const deleteSubmissionSlot = (assignmentId, name) =>
  apiFetch(`/submissions/${assignmentId}/slot/${encodeURIComponent(name)}`, { method: 'DELETE' });

// downloadSubmissionSlot fetches a staged (not yet submitted) file.
export const downloadSubmissionSlot = (assignmentId, name) =>
  fetch(`${API_BASE}/submissions/${assignmentId}/slot/${encodeURIComponent(name)}`, {
    credentials: 'same-origin',
  }).then(async (res) => {
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      throw new Error(data?.error || `HTTP ${res.status}`);
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = name;
    a.click();
    URL.revokeObjectURL(url);
  });

// getSubmissionFileText fetches a submitted file as text for inline viewing.
export const getSubmissionFileText = (submissionId, name) =>
  fetch(`${API_BASE}/submission-files/${submissionId}/${encodeURIComponent(name)}`, {
    credentials: 'same-origin',
  }).then(async (res) => {
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      throw new Error(data?.error || `HTTP ${res.status}`);
    }
    return res.text();
  });

// downloadSubmissionFile downloads one file of a past submission attempt.
export const downloadSubmissionFile = (submissionId, name) =>
  fetch(`${API_BASE}/submission-files/${submissionId}/${encodeURIComponent(name)}`, {
    credentials: 'same-origin',
  }).then(async (res) => {
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      throw new Error(data?.error || `HTTP ${res.status}`);
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = name;
    a.click();
    URL.revokeObjectURL(url);
  });

// submitSubmission finalizes the staged draft into a submission attempt.
export const submitSubmission = (assignmentId, confirmLate = false) =>
  apiFetch(`/submissions/${assignmentId}/submit${confirmLate ? '?confirmLate=true' : ''}`, { method: 'POST' });

// ---- Code submissions (admin) ----

export const adminListSubAssignments = (courseYearId, termId) =>
  adminFetch(`/admin/submissions/assignments?courseYearId=${courseYearId}&termId=${termId}`);

export const adminCreateSubAssignment = (body) =>
  adminFetch('/admin/submissions/assignments', {
    method: 'POST',
    body: JSON.stringify(body),
  });

export const adminGetSubAssignment = (id) =>
  adminFetch(`/admin/submissions/assignments/${id}`);

export const adminUpdateSubAssignment = (id, body) =>
  adminFetch(`/admin/submissions/assignments/${id}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  });

export const adminDeleteSubAssignment = (id) =>
  adminFetch(`/admin/submissions/assignments/${id}`, { method: 'DELETE' });

export const adminUploadSubTest = (assignmentId, file, name, visibility) => {
  const token = sessionStorage.getItem('adminToken') || '';
  const form = new FormData();
  form.append('file', file);
  form.append('name', name);
  form.append('visibility', visibility);
  // No Content-Type header: the browser sets the multipart boundary.
  return fetch(`${API_BASE}/admin/submissions/assignments/${assignmentId}/tests`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { Authorization: `Bearer ${token}` },
    body: form,
  }).then(async (res) => {
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      const error = new Error(data?.error || `HTTP ${res.status}`);
      error.status = res.status;
      error.data = data;
      throw error;
    }
    return data;
  });
};

export const adminDeleteSubTest = (assignmentId, testId) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/tests/${testId}`, { method: 'DELETE' });

// adminGetSubTestFile fetches a test harness file as text for inline preview.
export const adminGetSubTestFile = (assignmentId, testId) => {
  const token = sessionStorage.getItem('adminToken') || '';
  return fetch(`${API_BASE}/admin/submissions/assignments/${assignmentId}/tests/${testId}`, {
    credentials: 'same-origin',
    headers: { Authorization: `Bearer ${token}` },
  }).then(async (res) => {
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      throw new Error(data?.error || `HTTP ${res.status}`);
    }
    return res.text();
  });
};

// adminUpdateSubTest overwrites a test harness file with new content.
export const adminUpdateSubTest = (assignmentId, testId, content) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/tests/${testId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'text/plain' },
    body: content,
  });

export const adminGetSampleFiles = (assignmentId) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/sample`);

// adminGetSampleFile fetches one sample solution file as text.
export const adminGetSampleFile = (assignmentId, name) => {
  const token = sessionStorage.getItem('adminToken') || '';
  return fetch(`${API_BASE}/admin/submissions/assignments/${assignmentId}/sample/files/${encodeURIComponent(name)}`, {
    credentials: 'same-origin',
    headers: { Authorization: `Bearer ${token}` },
  }).then(async (res) => {
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      throw new Error(data?.error || `HTTP ${res.status}`);
    }
    return res.text();
  });
};

// adminPutSampleFile creates or overwrites one sample solution file.
export const adminPutSampleFile = (assignmentId, name, content) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/sample/files/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'text/plain' },
    body: content,
  });

export const adminDeleteSampleFile = (assignmentId, name) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/sample/files/${encodeURIComponent(name)}`, { method: 'DELETE' });

// adminRunSample runs every test of the assignment against the sample
// solution files and returns the per-test results synchronously.
export const adminRunSample = (assignmentId) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/sample/run`, { method: 'POST' });

export const adminGetBasecodeFiles = (assignmentId) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/basecode`);

// adminGetBasecodeFile fetches one base code file as text.
export const adminGetBasecodeFile = (assignmentId, name) => {
  const token = sessionStorage.getItem('adminToken') || '';
  return fetch(`${API_BASE}/admin/submissions/assignments/${assignmentId}/basecode/files/${encodeURIComponent(name)}`, {
    credentials: 'same-origin',
    headers: { Authorization: `Bearer ${token}` },
  }).then(async (res) => {
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      throw new Error(data?.error || `HTTP ${res.status}`);
    }
    return res.text();
  });
};

// adminPutBasecodeFile creates or overwrites one base code file.
export const adminPutBasecodeFile = (assignmentId, name, content) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/basecode/files/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'text/plain' },
    body: content,
  });

export const adminDeleteBasecodeFile = (assignmentId, name) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/basecode/files/${encodeURIComponent(name)}`, { method: 'DELETE' });

export const adminListSubAssignmentSubmissions = (assignmentId) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/submissions`);

export const adminRunSubTests = (assignmentId, studentPk) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/run${studentPk ? `?student=${studentPk}` : ''}`, { method: 'POST' });

export const adminGetSubmission = (submissionId) =>
  adminFetch(`/admin/submissions/submissions/${submissionId}`);

// adminGetSubmissionFileText fetches a submission file as text for inline viewing.
export const adminGetSubmissionFileText = (submissionId, name) => {
  const token = sessionStorage.getItem('adminToken') || '';
  return fetch(`${API_BASE}/admin/submissions/submissions/${submissionId}/files/${encodeURIComponent(name)}`, {
    credentials: 'same-origin',
    headers: { Authorization: `Bearer ${token}` },
  }).then(async (res) => {
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      const error = new Error(data?.error || `HTTP ${res.status}`);
      error.status = res.status;
      throw error;
    }
    return res.text();
  });
};

// adminDownloadSubmissionsZip downloads every enrolled student's latest
// submission for an assignment as one zip, one folder per student.
export const adminDownloadSubmissionsZip = (assignmentId) => {
  const token = sessionStorage.getItem('adminToken') || '';
  return fetch(`${API_BASE}/admin/submissions/assignments/${assignmentId}/download`, {
    credentials: 'same-origin',
    headers: { Authorization: `Bearer ${token}` },
  }).then(async (res) => {
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      const error = new Error(data?.error || `HTTP ${res.status}`);
      error.status = res.status;
      throw error;
    }
    const blob = await res.blob();
    const disposition = res.headers.get('Content-Disposition') || '';
    const match = disposition.match(/filename="?([^";]+)"?/);
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = match ? match[1] : `submissions-assignment-${assignmentId}.zip`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  });
};

export const adminDownloadSubmissionFile = (submissionId, name) => {
  const token = sessionStorage.getItem('adminToken') || '';
  return fetch(`${API_BASE}/admin/submissions/submissions/${submissionId}/files/${encodeURIComponent(name)}`, {
    credentials: 'same-origin',
    headers: { Authorization: `Bearer ${token}` },
  }).then(async (res) => {
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      const error = new Error(data?.error || `HTTP ${res.status}`);
      error.status = res.status;
      throw error;
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = name;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  });
};

export const adminRunPlagiarism = (assignmentId) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/plagiarism`, { method: 'POST' });

export const adminGetPlagiarism = (assignmentId) =>
  adminFetch(`/admin/submissions/assignments/${assignmentId}/plagiarism`);

// adminDownloadPlagReport downloads the full JPlag report of the latest
// finished run; the server picks the extension (.jplag for JPlag 6, .zip for
// older runs).
export const adminDownloadPlagReport = (assignmentId) => {
  const token = sessionStorage.getItem('adminToken') || '';
  return fetch(`${API_BASE}/admin/submissions/assignments/${assignmentId}/plagiarism/report`, {
    credentials: 'same-origin',
    headers: { Authorization: `Bearer ${token}` },
  }).then(async (res) => {
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      const error = new Error(data?.error || `HTTP ${res.status}`);
      error.status = res.status;
      throw error;
    }
    const blob = await res.blob();
    const disposition = res.headers.get('Content-Disposition') || '';
    const match = disposition.match(/filename="?([^";]+)"?/);
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = match ? match[1] : `plag-report-assignment-${assignmentId}.jplag`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  });
};

export const adminGetSubQueue = () =>
  adminFetch('/admin/submissions/queue');

// adminCreateSession sets an HttpOnly cookie carrying the admin token so
// browser-embedded admin tools (the JPlag report viewer) can call admin
// endpoints without an Authorization header.
export const adminCreateSession = () =>
  adminFetch('/admin/session', { method: 'POST' });

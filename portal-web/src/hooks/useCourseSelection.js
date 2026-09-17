import { useCallback, useEffect, useState } from 'react';

const COURSE_KEY_STORAGE = 'portalSelectedCourse';

export function courseKey(course) {
  return `${course.courseYearId}-${course.termId}`;
}

export function readStoredCourseKey() {
  return localStorage.getItem(COURSE_KEY_STORAGE) || '';
}

export function writeStoredCourseKey(key) {
  localStorage.setItem(COURSE_KEY_STORAGE, key);
}

// resolveCourse returns the course matching key, or the first course when the
// key is missing or stale (e.g. stored by another portal or unpublished since).
export function resolveCourse(courses, key) {
  if (!courses || courses.length === 0) return undefined;
  return courses.find((c) => courseKey(c) === key) || courses[0];
}

// useCourseSelection tracks the portal-wide selected course. The raw key lives
// in localStorage so it survives navigation, reloads, and browser sessions;
// the returned selectedKey is always resolved against the given course list.
// initialKey (e.g. from URL params) overrides the stored key and is persisted.
export function useCourseSelection(courses, initialKey) {
  const [storedKey, setStoredKey] = useState(() => initialKey || readStoredCourseKey());
  const [syncedInitialKey, setSyncedInitialKey] = useState(initialKey);

  // Adopt an external key (e.g. URL params) when it changes: a state adjustment
  // during render; the localStorage write happens in the effect below.
  if (initialKey && initialKey !== syncedInitialKey) {
    setSyncedInitialKey(initialKey);
    setStoredKey(initialKey);
  }

  useEffect(() => {
    if (initialKey) writeStoredCourseKey(initialKey);
  }, [initialKey]);

  const setSelectedKey = useCallback((key) => {
    setStoredKey(key);
    writeStoredCourseKey(key);
  }, []);

  const selected = resolveCourse(courses, storedKey);
  return { selectedKey: selected ? courseKey(selected) : '', selected, setSelectedKey };
}

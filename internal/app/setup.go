package app

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

func (a *App) RunSetupWizard() error {
	courseName, err := a.promptNonEmpty("Course name")
	if err != nil {
		return err
	}

	termCount, err := a.promptIntRetry("How many terms", func(v int) error {
		if v <= 0 {
			return errors.New("term count must be at least 1")
		}
		return nil
	})
	if err != nil {
		return err
	}

	sectionCount, err := a.promptIntRetry("How many sections", func(v int) error {
		if v <= 0 {
			return errors.New("section count must be at least 1")
		}
		return nil
	})
	if err != nil {
		return err
	}

	now := time.Now()
	startYear := now.Year()
	endYear := startYear + 1
	yearLabel := fmt.Sprintf("%d-%02d", startYear, endYear%100)
	courseYearName := fmt.Sprintf("%s %s", courseName, yearLabel)

	courseID, err := a.ensureCourse(courseName)
	if err != nil {
		return err
	}
	courseYearID, err := a.ensureCourseYear(courseID, courseYearName)
	if err != nil {
		return err
	}

	termIDs := make([]int, 0, termCount)
	for i := 1; i <= termCount; i++ {
		name := fmt.Sprintf("Term %d", i)
		startDate, endDate := termDates(startYear, i)
		termID, err := a.ensureTerm(name, startDate, endDate)
		if err != nil {
			return err
		}
		termIDs = append(termIDs, termID)
		if _, err := a.db.Exec(`INSERT OR IGNORE INTO course_year_terms(course_year_id, term_id) VALUES (?, ?)`, courseYearID, termID); err != nil {
			return err
		}
	}

	firstSectionID := 0
	for i := 1; i <= sectionCount; i++ {
		sectionName, err := a.promptNonEmpty(fmt.Sprintf("Section %d name", i))
		if err != nil {
			return err
		}
		sectionID, err := a.ensureSection(courseYearID, sectionName)
		if err != nil {
			return err
		}
		if firstSectionID == 0 {
			firstSectionID = sectionID
		}

		studentCount, err := a.promptIntRetry(fmt.Sprintf("How many students in %s", sectionName), func(v int) error {
			if v < 0 {
				return fmt.Errorf("student count for %s cannot be negative", sectionName)
			}
			return nil
		})
		if err != nil {
			return err
		}
		for j := 1; j <= studentCount; j++ {
			first, err := a.promptNonEmpty(fmt.Sprintf("%s student %d first name", sectionName, j))
			if err != nil {
				return err
			}
			last, err := a.promptNonEmpty(fmt.Sprintf("%s student %d last name", sectionName, j))
			if err != nil {
				return err
			}
			chineseName, err := a.promptOptional(fmt.Sprintf("%s student %d chinese name (optional)", sectionName, j))
			if err != nil {
				return err
			}
			studentIDRaw, err := a.promptOptional(fmt.Sprintf("%s student %d student ID (optional)", sectionName, j))
			if err != nil {
				return err
			}
			student, err := a.upsertStudent(first, last, chineseName, studentIDRaw)
			if err != nil {
				return err
			}
			for _, termID := range termIDs {
				if err := a.enrollStudent(sectionID, termID, student.ID); err != nil {
					return err
				}
			}
		}
	}

	a.v.Set("context.term_id", termIDs[0])
	a.v.Set("context.year", yearLabel)
	a.v.Set("context.course_year_id", courseYearID)
	a.v.Set("context.section_id", firstSectionID)
	a.v.Set("context.assignment_id", 0)
	if err := a.v.WriteConfig(); err != nil {
		return err
	}

	fmt.Fprintf(a.out, "Created course: %s\n", baseCourseName(courseYearName))
	fmt.Fprintf(a.out, "Using year: %s\n", yearLabel)
	fmt.Fprintf(a.out, "Created %d term(s) and %d section(s)\n", termCount, sectionCount)
	if termCount > 0 {
		fmt.Fprintf(a.out, "Using term: Term 1\n")
	}
	if firstSectionID != 0 {
		fmt.Fprintf(a.out, "Setup complete. Current section is ready for use.\n")
	}
	return nil
}

func (a *App) promptInt(label string) (int, error) {
	raw, err := a.prompt(label)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("invalid number for %s: %s", label, raw)
	}
	return value, nil
}

func (a *App) promptNonEmpty(label string) (string, error) {
	for {
		raw, err := a.prompt(label)
		if err != nil {
			return "", err
		}
		value := normalizeSpaces(raw)
		if value != "" {
			return value, nil
		}
		fmt.Fprintln(a.out, retryMessage(fmt.Sprintf("%s cannot be blank", label)))
	}
}

func (a *App) promptIntRetry(label string, validate func(int) error) (int, error) {
	for {
		value, err := a.promptInt(label)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, err
			}
			fmt.Fprintln(a.out, retryMessage(err.Error()))
			continue
		}
		if validate != nil {
			if err := validate(value); err != nil {
				fmt.Fprintln(a.out, retryMessage(err.Error()))
				continue
			}
		}
		return value, nil
	}
}

func (a *App) ensureCourse(name string) (int, error) {
	var id int
	err := a.db.QueryRow(`SELECT course_id FROM courses WHERE lower(name)=lower(?)`, name).Scan(&id)
	switch {
	case err == nil:
		return id, nil
	case errors.Is(err, sql.ErrNoRows):
		res, err := a.db.Exec(`INSERT INTO courses(name) VALUES (?)`, name)
		if err != nil {
			return 0, err
		}
		lastID, err := res.LastInsertId()
		return int(lastID), err
	default:
		return 0, err
	}
}

func (a *App) ensureCourseYear(courseID int, name string) (int, error) {
	var id int
	err := a.db.QueryRow(`SELECT course_year_id FROM course_years WHERE course_id = ? AND lower(name)=lower(?)`, courseID, name).Scan(&id)
	switch {
	case err == nil:
		return id, nil
	case errors.Is(err, sql.ErrNoRows):
		res, err := a.db.Exec(`INSERT INTO course_years(course_id, name) VALUES (?, ?)`, courseID, name)
		if err != nil {
			return 0, err
		}
		lastID, err := res.LastInsertId()
		return int(lastID), err
	default:
		return 0, err
	}
}

func (a *App) ensureTerm(name, startDate, endDate string) (int, error) {
	var id int
	err := a.db.QueryRow(`SELECT term_id FROM terms WHERE lower(name)=lower(?)`, name).Scan(&id)
	switch {
	case err == nil:
		_, err = a.db.Exec(`UPDATE terms SET start_date = ?, end_date = ? WHERE term_id = ?`, startDate, endDate, id)
		return id, err
	case errors.Is(err, sql.ErrNoRows):
		res, err := a.db.Exec(`INSERT INTO terms(name, start_date, end_date) VALUES (?, ?, ?)`, name, startDate, endDate)
		if err != nil {
			return 0, err
		}
		lastID, err := res.LastInsertId()
		return int(lastID), err
	default:
		return 0, err
	}
}

func (a *App) ensureSection(courseYearID int, name string) (int, error) {
	var id int
	err := a.db.QueryRow(`SELECT section_id FROM sections WHERE course_year_id = ? AND lower(name)=lower(?)`, courseYearID, name).Scan(&id)
	switch {
	case err == nil:
		return id, nil
	case errors.Is(err, sql.ErrNoRows):
		res, err := a.db.Exec(`INSERT INTO sections(course_year_id, name) VALUES (?, ?)`, courseYearID, name)
		if err != nil {
			return 0, err
		}
		lastID, err := res.LastInsertId()
		return int(lastID), err
	default:
		return 0, err
	}
}

func termDates(startYear, termNumber int) (string, string) {
	if termNumber%2 == 1 {
		return fmt.Sprintf("%04d-08-01", startYear), fmt.Sprintf("%04d-12-31", startYear)
	}
	return fmt.Sprintf("%04d-01-01", startYear+1), fmt.Sprintf("%04d-06-30", startYear+1)
}

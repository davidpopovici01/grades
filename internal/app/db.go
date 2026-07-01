package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/davidpopovici01/grades/internal/migrate"
)

func (a *App) MigrateUp() error {
	if err := migrate.Up(a.db); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "Migrations applied.")
	return nil
}

func (a *App) MigrateDown() error {
	if err := migrate.Down(a.db); err != nil {
		return err
	}
	return a.resetContextState("Database schema dropped.")
}

func (a *App) ResetDB() error {
	if err := migrate.Down(a.db); err != nil {
		return err
	}
	if err := migrate.Up(a.db); err != nil {
		return err
	}
	return a.resetContextState("Database reset complete.")
}

func (a *App) resetContextState(message string) error {
	a.setContext("context.term_id", 0)
	a.setContext("context.course_year_id", 0)
	a.setContext("context.section_id", 0)
	a.setContext("context.assignment_id", 0)
	if err := a.writeContextConfig(); err != nil {
		return err
	}
	a.v.Set("context.current_course", "")
	if err := a.v.WriteConfig(); err != nil {
		return err
	}
	a.profileName = ""
	a.profileViper = nil
	a.profilePath = ""
	fmt.Fprintln(a.out, message)
	return nil
}

func (a *App) BackupDB(file string) error {
	if strings.TrimSpace(file) == "" {
		file = filepath.Join(a.homeDir, "..", "gradesBackups", "grades_"+time.Now().Format("20060102_150405")+".db")
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	escaped := strings.ReplaceAll(file, `'`, `''`)
	if _, err := a.db.Exec(`VACUUM INTO '` + escaped + `'`); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Database backup created: %s\n", file)
	return nil
}

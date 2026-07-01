package app

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/davidpopovici01/grades/internal/db"
	"github.com/davidpopovici01/grades/internal/migrate"
	"github.com/spf13/viper"
)

type App struct {
	db           *sql.DB
	v            *viper.Viper
	profileName  string
	profileViper *viper.Viper
	profilePath  string
	homeDir      string
	configPath   string
	dbPath       string
	in           *bufio.Reader
	out          io.Writer
	errOut       io.Writer
}

type Context struct {
	Year         string
	TermID       int
	CourseYearID int
	SectionID    int
	AssignmentID int
}

func New(in io.Reader, out, errOut io.Writer) (*App, error) {
	return NewWithClass(in, out, errOut, "")
}

func NewWithClass(in io.Reader, out, errOut io.Writer, class string) (*App, error) {
	trace := newStartupTrace(errOut)
	defer trace.finish()

	homeDir := os.Getenv("GRADES_HOME")
	if homeDir == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		homeDir = filepath.Join(userHome, ".grades")
	}
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		return nil, err
	}
	trace.mark("mkdir home")

	configPath := filepath.Join(homeDir, "config.yaml")
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	v.SetDefault("context.year", "")
	v.SetDefault("context.term_id", 0)
	v.SetDefault("context.course_year_id", 0)
	v.SetDefault("context.section_id", 0)
	v.SetDefault("context.assignment_id", 0)
	v.SetDefault("context.current_course", "")
	v.SetDefault("portal.server", "")
	v.SetDefault("portal.key", "")
	v.SetDefault("portal.remote_dir", "~/portal")
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		if err := v.WriteConfigAs(configPath); err != nil {
			return nil, err
		}
	}
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}
	trace.mark("load config")

	dbPath := os.Getenv("GRADES_DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(homeDir, "grades.db")
	}
	conn, err := db.Open(dbPath)
	if err != nil {
		return nil, err
	}
	trace.mark("open db")
	if err := migrate.Up(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	trace.mark("migrate")

	app := &App{
		db:         conn,
		v:          v,
		homeDir:    homeDir,
		configPath: configPath,
		dbPath:     dbPath,
		in:         bufio.NewReader(in),
		out:        out,
		errOut:     errOut,
	}

	if class == "" {
		class = os.Getenv("GRADES_CONTEXT")
	}
	if err := app.initContextProfile(class); err != nil {
		_ = conn.Close()
		return nil, err
	}
	trace.mark("load profile")

	return app, nil
}

type startupTrace struct {
	out     io.Writer
	enabled bool
	started time.Time
	last    time.Time
	steps   []string
}

func newStartupTrace(out io.Writer) *startupTrace {
	now := time.Now()
	return &startupTrace{
		out:     out,
		enabled: os.Getenv("GRADES_STARTUP_TRACE") == "1",
		started: now,
		last:    now,
	}
}

func (t *startupTrace) mark(label string) {
	if t == nil {
		return
	}
	now := time.Now()
	t.steps = append(t.steps, fmt.Sprintf("%s: %s", label, now.Sub(t.last).Round(time.Millisecond)))
	t.last = now
}

func (t *startupTrace) finish() {
	if t == nil || t.out == nil {
		return
	}
	total := time.Since(t.started).Round(time.Millisecond)
	if t.enabled {
		for _, step := range t.steps {
			fmt.Fprintf(t.out, "startup %s\n", step)
		}
		fmt.Fprintf(t.out, "startup total: %s\n", total)
		return
	}
	if total >= 5*time.Second {
		fmt.Fprintf(t.out, "startup total: %s (set GRADES_STARTUP_TRACE=1 for breakdown)\n", total)
	}
}

func (a *App) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

func (a *App) context() Context {
	if a.profileViper != nil {
		return Context{
			Year:         a.profileViper.GetString("context.year"),
			TermID:       a.profileViper.GetInt("context.term_id"),
			CourseYearID: a.profileViper.GetInt("context.course_year_id"),
			SectionID:    a.profileViper.GetInt("context.section_id"),
			AssignmentID: a.profileViper.GetInt("context.assignment_id"),
		}
	}
	return Context{
		Year:         a.v.GetString("context.year"),
		TermID:       a.v.GetInt("context.term_id"),
		CourseYearID: a.v.GetInt("context.course_year_id"),
		SectionID:    a.v.GetInt("context.section_id"),
		AssignmentID: a.v.GetInt("context.assignment_id"),
	}
}

func (a *App) setContext(key string, value any) {
	if a.profileViper != nil {
		a.profileViper.Set(key, value)
		return
	}
	a.v.Set(key, value)
}

func (a *App) writeContextConfig() error {
	if a.profileViper != nil {
		return a.profileViper.WriteConfig()
	}
	return a.v.WriteConfig()
}

func (a *App) initContextProfile(class string) error {
	ctxDir := filepath.Join(a.homeDir, "contexts")
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		return err
	}

	explicitClass := class
	profileName := class
	if profileName == "" {
		profileName = a.v.GetString("context.current_course")
	}
	if profileName == "" {
		legacyID := a.v.GetInt("context.course_year_id")
		if legacyID != 0 {
			name, err := a.courseBaseName(legacyID)
			if err != nil {
				return err
			}
			if name != "" {
				profileName = name
				if err := a.migrateLegacyContext(profileName); err != nil {
					return err
				}
			}
		}
	}
	if profileName == "" {
		return nil
	}

	if err := a.loadProfile(profileName); err != nil {
		return err
	}

	// If the user explicitly selected a class (--class or GRADES_CONTEXT),
	// make it the new default for plain `grades` invocations.
	if explicitClass != "" && a.v.GetString("context.current_course") != explicitClass {
		a.v.Set("context.current_course", explicitClass)
		return a.v.WriteConfig()
	}
	return nil
}

func (a *App) loadProfile(name string) error {
	a.profileName = name
	a.profilePath = filepath.Join(a.homeDir, "contexts", profileFileName(name)+".yaml")

	pv := viper.New()
	pv.SetConfigFile(a.profilePath)
	pv.SetConfigType("yaml")
	pv.SetDefault("context.year", "")
	pv.SetDefault("context.term_id", 0)
	pv.SetDefault("context.course_year_id", 0)
	pv.SetDefault("context.section_id", 0)
	pv.SetDefault("context.assignment_id", 0)

	if _, err := os.Stat(a.profilePath); errors.Is(err, os.ErrNotExist) {
		if err := pv.WriteConfigAs(a.profilePath); err != nil {
			return err
		}
	}
	if err := pv.ReadInConfig(); err != nil {
		return err
	}
	a.profileViper = pv
	return nil
}

func (a *App) saveCurrentProfile() error {
	if a.profileViper == nil || a.profilePath == "" {
		return nil
	}
	return a.profileViper.WriteConfig()
}

func (a *App) migrateLegacyContext(profileName string) error {
	if err := a.loadProfile(profileName); err != nil {
		return err
	}
	ctx := Context{
		Year:         a.v.GetString("context.year"),
		TermID:       a.v.GetInt("context.term_id"),
		CourseYearID: a.v.GetInt("context.course_year_id"),
		SectionID:    a.v.GetInt("context.section_id"),
		AssignmentID: a.v.GetInt("context.assignment_id"),
	}
	a.profileViper.Set("context.year", ctx.Year)
	a.profileViper.Set("context.term_id", ctx.TermID)
	a.profileViper.Set("context.course_year_id", ctx.CourseYearID)
	a.profileViper.Set("context.section_id", ctx.SectionID)
	a.profileViper.Set("context.assignment_id", ctx.AssignmentID)
	if err := a.profileViper.WriteConfig(); err != nil {
		return err
	}

	a.v.Set("context.current_course", profileName)
	a.v.Set("context.year", "")
	a.v.Set("context.term_id", 0)
	a.v.Set("context.course_year_id", 0)
	a.v.Set("context.section_id", 0)
	a.v.Set("context.assignment_id", 0)
	return a.v.WriteConfig()
}

func (a *App) courseBaseName(courseYearID int) (string, error) {
	if courseYearID == 0 {
		return "", nil
	}
	var name string
	err := a.db.QueryRow(`SELECT name FROM course_years WHERE course_year_id = ?`, courseYearID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return baseCourseName(name), nil
}

func profileFileName(name string) string {
	name = normalizeSpaces(name)
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "..", "-")
	return replacer.Replace(name)
}

func (a *App) prompt(label string) (string, error) {
	fmt.Fprintf(a.out, "%s: ", label)
	line, err := a.in.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && line != "" {
			return strings.TrimSpace(line), nil
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func fallback(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}

func normalizeSpaces(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func (a *App) promptOptional(label string) (string, error) {
	raw, err := a.prompt(label)
	if err != nil {
		return "", err
	}
	return normalizeSpaces(raw), nil
}

func (a *App) promptYesNo(label string, defaultYes bool) (bool, error) {
	for {
		suffix := "[y/N]"
		if defaultYes {
			suffix = "[Y/n]"
		}
		raw, err := a.prompt(label + " " + suffix)
		if err != nil {
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(raw))
		if answer == "" {
			return defaultYes, nil
		}
		switch answer {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(a.out, retryMessage("please answer yes or no"))
		}
	}
}

func retryMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "Retry."
	}
	last := message[len(message)-1]
	if last != '.' && last != '!' && last != '?' {
		message += "."
	}
	return message + " Retry."
}

func colorRed(s string) string {
	return "\x1b[31m" + s + "\x1b[0m"
}

func colorGreen(s string) string {
	return "\x1b[32m" + s + "\x1b[0m"
}

func colorOrange(s string) string {
	return "\x1b[33m" + s + "\x1b[0m"
}

func colorBlackOnWhite(s string) string {
	return "\x1b[30;47m" + s + "\x1b[0m"
}

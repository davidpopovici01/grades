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

	"github.com/davidpopovici01/grades/internal/db"
	"github.com/davidpopovici01/grades/internal/migrate"
	"github.com/spf13/viper"
)

type App struct {
	db         *sql.DB
	v          *viper.Viper
	homeDir    string
	configPath string
	dbPath     string
	in         *bufio.Reader
	out        io.Writer
	errOut     io.Writer
}

type Context struct {
	Year         string
	TermID       int
	CourseYearID int
	SectionID    int
	AssignmentID int
}

func New(in io.Reader, out, errOut io.Writer) (*App, error) {
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

	configPath := filepath.Join(homeDir, "config.yaml")
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	v.SetDefault("context.year", "")
	v.SetDefault("context.term_id", 0)
	v.SetDefault("context.course_year_id", 0)
	v.SetDefault("context.section_id", 0)
	v.SetDefault("context.assignment_id", 0)
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		if err := v.WriteConfigAs(configPath); err != nil {
			return nil, err
		}
	}
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	dbPath := os.Getenv("GRADES_DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(homeDir, "grades.db")
	}
	conn, err := db.Open(dbPath)
	if err != nil {
		return nil, err
	}
	if err := migrate.Up(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return &App{
		db:         conn,
		v:          v,
		homeDir:    homeDir,
		configPath: configPath,
		dbPath:     dbPath,
		in:         bufio.NewReader(in),
		out:        out,
		errOut:     errOut,
	}, nil
}

func (a *App) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

func (a *App) context() Context {
	return Context{
		Year:         a.v.GetString("context.year"),
		TermID:       a.v.GetInt("context.term_id"),
		CourseYearID: a.v.GetInt("context.course_year_id"),
		SectionID:    a.v.GetInt("context.section_id"),
		AssignmentID: a.v.GetInt("context.assignment_id"),
	}
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

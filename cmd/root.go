package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/davidpopovici01/grades/internal/app"
	"github.com/spf13/cobra"
)

// Execute builds and runs the CLI.
func Execute() {
	root := NewRootCmd(os.Stdin, os.Stdout, os.Stderr)
	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// NewRootCmd constructs the CLI with injectable IO for tests.
func NewRootCmd(in io.Reader, out, errOut io.Writer) *cobra.Command {
	gradesApp, err := app.New(in, out, errOut)
	cobra.CheckErr(err)
	cobra.OnFinalize(func() {
		_ = gradesApp.Close()
	})

	rootCmd := &cobra.Command{
		Use:   "grades",
		Short: "Keyboard-driven grade management",
		Long:  "Grades CLI manages terms, sections, assignments, students, and scores using a context-first workflow.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return gradesApp.PrintDashboard()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)

	rootCmd.AddCommand(
		newSetupCmd(gradesApp),
		newContextCmd(gradesApp),
		legacyHidden(newUseCmd(gradesApp)),
		legacyHidden(newClearCmd(gradesApp)),
		legacyHidden(newListCmd(gradesApp)),
		newCategoriesCmd(gradesApp),
		newStudentsCmd(gradesApp),
		newAssignmentsCmd(gradesApp),
		newEnterCmd(gradesApp),
		newShowCmd(gradesApp),
		newPassCmd(gradesApp),
		newRedoCmd(gradesApp),
		newMakeupCmd(gradesApp),
		newFillCmd(gradesApp),
		newMarkLateCmd(gradesApp),
		newClearLateCmd(gradesApp),
		newClearRedoCmd(gradesApp),
		newClearCheatCmd(gradesApp),
		legacyHidden(newGradesCmd(gradesApp)),
		newGradebookCmd(gradesApp),
		newOverviewCmd(gradesApp),
		newReportsCmd(gradesApp),
		newStatsCmd(gradesApp),
		newSystemCmd(gradesApp),
		legacyHidden(newRepairCmd(gradesApp)),
		newImportCmd(gradesApp),
		newExportCmd(gradesApp),
		newPublishCmd(gradesApp),
		newWebCmd(gradesApp),
		legacyHidden(newMigrateCmd(gradesApp)),
		legacyHidden(newDBCmd(gradesApp)),
	)

	return rootCmd
}

func newReportsCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reports",
		Short: "Suggest and generate study reports",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "suggest",
		Args:  cobra.NoArgs,
		Short: "Suggest students who may need a study report",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.SuggestStudyReports()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "create <student> [file]",
		Args:  cobra.RangeArgs(1, 2),
		Short: "Create a filled study report for one student",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := ""
			if len(args) == 2 {
				output = args[1]
			}
			return a.CreateStudyReport(strings.Join(args[:1], " "), output)
		},
	})
	return cmd
}

func legacyHidden(cmd *cobra.Command) *cobra.Command {
	cmd.Hidden = true
	return cmd
}

func newSetupCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Args:  cobra.NoArgs,
		Short: "Guided setup for a new year, course, terms, sections, and students",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.RunSetupWizard()
		},
	}
}

func newUseCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "use",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "year <name-or-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Set the active year",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.UseYear(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "term <name>",
		Args:  cobra.ExactArgs(1),
		Short: "Set the active term",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.UseTerm(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "course-year <name>",
		Args:  cobra.ExactArgs(1),
		Short: "Set the active course",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.UseCourseYear(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:     "course <name-or-id>",
		Aliases: []string{"class"},
		Args:    cobra.ExactArgs(1),
		Short:   "Set the active course",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.UseCourseYear(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "section <name-or-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Set the active section",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.UseSection(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "assignment <title-or-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Set the active assignment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.UseAssignment(args[0])
		},
	})

	return cmd
}

func newContextCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage active year, term, course, section, and assignment context",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newUseCmd(a), newClearCmd(a), newListCmd(a))
	return cmd
}

func newClearCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "clear",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	for _, scope := range []string{"year", "term", "course-year", "section", "assignment"} {
		scope := scope
		cmd.AddCommand(&cobra.Command{
			Use:   scope,
			Args:  cobra.NoArgs,
			Short: fmt.Sprintf("Clear the active %s", scope),
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.ClearScope(scope)
			},
		})
	}
	return cmd
}

func newListCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "list",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}

	cmd.AddCommand(&cobra.Command{
		Use:  "years",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListYears()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:  "terms",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListTerms()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:  "course-years",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListCourseYears()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:     "courses",
		Aliases: []string{"classes"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListCourseYears()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:  "sections",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListSections()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:  "assignments",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListAssignments()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:  "students",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListStudents()
		},
	})

	return cmd
}

func newStudentsCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "students",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "add",
		Args:  cobra.NoArgs,
		Short: "Add a student to the current section",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.AddStudentInteractive()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "remove [student]",
		Args:  cobra.ArbitraryArgs,
		Short: "Remove a student from the current section or current course",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.RemoveStudentInteractive(strings.Join(args, " "))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List students in the current section",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListStudents()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <student>",
		Args:  cobra.ArbitraryArgs,
		Short: "Show details for a student",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ShowStudent(strings.Join(args, " "))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "import-powerschool <file>",
		Args:  cobra.ExactArgs(1),
		Short: "Update PowerSchool student numbers from a PowerSchool export CSV",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ImportPowerSchoolNumbers(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "deactivate [student]",
		Args:  cobra.ArbitraryArgs,
		Short: "Set a student inactive so they are hidden from normal rosters",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.SetStudentStatus(strings.Join(args, " "), "inactive")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "activate [student]",
		Args:  cobra.ArbitraryArgs,
		Short: "Set a student active again",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.SetStudentStatus(strings.Join(args, " "), "active")
		},
	})

	return cmd
}

func newCategoriesCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "categories",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List categories and weights for the current course and term",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListCategories()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set-weight <category> <percent>",
		Args:  cobra.ExactArgs(2),
		Short: "Set the weight for a category in the current course and term",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.SetCategoryWeight(args[0], args[1])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "weight <category> <percent>",
		Args:  cobra.ExactArgs(2),
		Short: "Set the weight for a category in the current course and term",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.SetCategoryWeight(args[0], args[1])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "schemes",
		Args:  cobra.NoArgs,
		Short: "List available category grading schemes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListGradingSchemes()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set-scheme <category> <scheme>",
		Args:  cobra.ExactArgs(2),
		Short: "Set the grading scheme for a category in the current course and term",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.SetCategoryScheme(args[0], args[1])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "pass-rate <category> <percent|raw>",
		Args:  cobra.ExactArgs(2),
		Short: "Set the default pass rate for a category in the current course and term",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.SetCategoryPassRate(args[0], args[1])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "import [file]",
		Args:  cobra.MaximumNArgs(1),
		Short: "Import category weights, schemes, and pass rates from CSV",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return a.ImportCategoriesWithGuidance(args[0])
			}
			return a.RunCategoryImportWizard()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "setup-csv [file]",
		Args:  cobra.MaximumNArgs(1),
		Short: "Create and open a category setup CSV template",
		RunE: func(cmd *cobra.Command, args []string) error {
			file := ""
			if len(args) == 1 {
				file = args[0]
			}
			return a.WriteCategorySetupCSV(file)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "scores",
		Args:  cobra.NoArgs,
		Short: "Show category scores for the current section or course",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ShowCategoryScores()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "totals",
		Args:  cobra.NoArgs,
		Short: "Show category totals for each student in the current term",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ShowCategoryScores()
		},
	})

	return cmd
}

func newAssignmentsCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "assignments",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "add",
		Aliases: []string{"create"},
		Args:    cobra.NoArgs,
		Short:   "Create an assignment in the current course and term",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.AddAssignmentInteractive()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:  "list",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListAssignments()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:  "show [assignment-id]",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			return a.ShowAssignment(id)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "edit",
		Args:  cobra.NoArgs,
		Short: "Edit the current assignment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.EditAssignmentInteractive()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:  "delete [assignment-id]",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			return a.DeleteAssignment(id)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "max <points>",
		Args:  cobra.ExactArgs(1),
		Short: "Change the max score for the current assignment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.SetAssignmentMaxPoints(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "pass-rate <percent|raw|default>",
		Args:  cobra.ExactArgs(1),
		Short: "Set the pass rate for the current assignment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.SetAssignmentPassRate(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:                "export [file]",
		Args:               cobra.ArbitraryArgs,
		Short:              "Export the current assignment, or all pending assignments with -all",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "-all" || args[0] == "--all") {
				if err := a.ExportPendingAssignments(); err != nil {
					return err
				}
				return a.PublishStudentPortal("")
			}
			if len(args) > 1 {
				return fmt.Errorf("assignments export accepts either [file] or -all")
			}
			file := ""
			if len(args) == 1 {
				file = args[0]
			}
			if err := a.ExportGrades(file); err != nil {
				return err
			}
			return a.PublishStudentPortal("")
		},
	})
	curveCmd := &cobra.Command{
		Use:   "curve",
		Short: "Show or change the curve for the current assignment",
	}
	curveCmd.AddCommand(&cobra.Command{
		Use:   "show",
		Args:  cobra.NoArgs,
		Short: "Show the current assignment curve",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ShowAssignmentCurve()
		},
	})
	curveCmd.AddCommand(&cobra.Command{
		Use:   "set <lift>",
		Args:  cobra.ExactArgs(1),
		Short: "Set the current assignment curve",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.SetAssignmentCurve(args[0])
		},
	})
	curveCmd.AddCommand(&cobra.Command{
		Use:   "target <desired-average>",
		Args:  cobra.ExactArgs(1),
		Short: "Tune the current assignment curve toward a target average",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.TuneAssignmentCurve(args[0])
		},
	})
	cmd.AddCommand(curveCmd)

	return cmd
}

func newGradesCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "grades",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newEnterCmd(a),
		newShowCmd(a),
		newPassCmd(a),
		newRedoCmd(a),
		newMakeupCmd(a),
		newFillCmd(a),
		newMarkLateCmd(a),
		newClearLateCmd(a),
		newClearRedoCmd(a),
		newClearCheatCmd(a),
	)

	return cmd
}

func newEnterCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:                "enter [-lastname]",
		Args:               cobra.MaximumNArgs(1),
		Short:              "Enter grades for the active assignment",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			byLastName := false
			if len(args) == 1 {
				switch args[0] {
				case "-lastname", "--lastname":
					byLastName = true
				default:
					return fmt.Errorf("unknown argument: %s", args[0])
				}
			}
			return a.EnterGradesInteractive(byLastName)
		},
	}
}

func newShowCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Args:  cobra.NoArgs,
		Short: "Show grades for the active assignment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ShowGrades()
		},
	}
}

func newPassCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "pass [student]",
		Args:  cobra.ArbitraryArgs,
		Short: "Mark a student as pass on the active assignment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.PassStudent(strings.Join(args, " "))
		},
	}
}

func newRedoCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "redo",
		Short: "List or pass redo work for a student without switching assignments",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list <student>",
		Args:  cobra.ArbitraryArgs,
		Short: "List active redo assignments for a student",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListStudentRedo(strings.Join(args, " "))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "pass <student>",
		Args:  cobra.ArbitraryArgs,
		Short: "Mark one redo assignment as pass for a student",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.PassStudentRedo(strings.Join(args, " "))
		},
	})
	return cmd
}

func newMakeupCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "make-up",
		Short: "List or record late/missing make-up work for a student without switching assignments",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list <student>",
		Args:  cobra.ArbitraryArgs,
		Short: "List active late/missing make-up assignments for a student",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListStudentMakeup(strings.Join(args, " "))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "enter <student>",
		Args:  cobra.ArbitraryArgs,
		Short: "Record a score for one late/missing make-up assignment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.EnterStudentMakeup(strings.Join(args, " "))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "pass <student>",
		Args:  cobra.ArbitraryArgs,
		Short: "Mark one late/missing make-up assignment as pass",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.PassStudentMakeup(strings.Join(args, " "))
		},
	})
	return cmd
}

func newFillCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "fill",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "pass",
		Args:  cobra.NoArgs,
		Short: "Fill blank entries on the active assignment with pass",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.FillPass()
		},
	})
	return cmd
}

func newMarkLateCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:                "mark-late [-undo]",
		Args:               cobra.MaximumNArgs(1),
		Short:              "Mark all currently missing grades for the active assignment as late",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				switch args[0] {
				case "-undo", "--undo":
					return a.UndoMarkMissingLate()
				default:
					return fmt.Errorf("unknown argument: %s", args[0])
				}
			}
			return a.MarkMissingLate()
		},
	}
}

func newClearLateCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "clear-late [student]",
		Args:  cobra.ArbitraryArgs,
		Short: "Clear the late flag for a student on the active assignment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ClearLate(strings.Join(args, " "))
		},
	}
}

func newClearRedoCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "clear-redo [student]",
		Args:  cobra.ArbitraryArgs,
		Short: "Clear the redo flag for a student on the active assignment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ClearRedo(strings.Join(args, " "))
		},
	}
}

func newClearCheatCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "clear-cheat [student]",
		Args:  cobra.ArbitraryArgs,
		Short: "Clear the cheat flag for a student on the active assignment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ClearCheat(strings.Join(args, " "))
		},
	}
}

func newGradebookCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "gradebook",
		Args:  cobra.NoArgs,
		Short: "Show the section gradebook",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ShowGradebook()
		},
	}
}

func newOverviewCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "overview",
		Args:  cobra.NoArgs,
		Short: "Show status overview for the current section or course",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ShowOverview()
		},
	}
}

func newStatsCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "stats",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}

	cmd.AddCommand(&cobra.Command{
		Use:  "assignment",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ShowAssignmentStats()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:  "section",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ShowSectionStats()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:  "student <student-id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ShowStudentStats(args[0])
		},
	})

	return cmd
}

func newRepairCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "repair",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "audit",
		Args:  cobra.NoArgs,
		Short: "Preview legacy grade rows that can be normalized",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.AuditRepairs()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "apply",
		Args:  cobra.NoArgs,
		Short: "Normalize legacy grade rows to current rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ApplyRepairs()
		},
	})
	return cmd
}

func newSystemCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "Database maintenance, migrations, and repair tools",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newDBCmd(a), newMigrateCmd(a), newRepairCmd(a))
	return cmd
}

func newImportCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Args:  cobra.NoArgs,
		Short: "Import roster data from CSV or create a setup CSV",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.RunImportWizard()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:  "students <file>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ImportStudents(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "roster <file>",
		Args:  cobra.ExactArgs(1),
		Short: "Import students into multiple sections from a roster CSV",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ImportRosterWithGuidance(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "setup-csv [file]",
		Args:  cobra.MaximumNArgs(1),
		Short: "Create and open a roster setup CSV template",
		RunE: func(cmd *cobra.Command, args []string) error {
			file := ""
			if len(args) == 1 {
				file = args[0]
			}
			return a.WriteRosterSetupCSV(file)
		},
	})
	return cmd
}

func newExportCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Args:  cobra.NoArgs,
		Short: "Export all unexported or modified assignments in the current course and term",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ExportPendingAssignments(); err != nil {
				return err
			}
			return a.PublishStudentPortal("")
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:  "grades [file]",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := ""
			if len(args) == 1 {
				file = args[0]
			}
			if err := a.ExportGrades(file); err != nil {
				return err
			}
			return a.PublishStudentPortal("")
		},
	})
	return cmd
}

func newPublishCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "publish [dir]",
		Args:  cobra.MaximumNArgs(1),
		Short: "Publish student portal data for the current course and term",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) == 1 {
				dir = args[0]
			}
			return a.PublishStudentPortal(dir)
		},
	}
}

func newWebCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Student portal commands",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "serve [addr] [dir]",
		Args:  cobra.MaximumNArgs(2),
		Short: "Serve the student portal locally",
		RunE: func(cmd *cobra.Command, args []string) error {
			addr := ""
			dir := ""
			if len(args) >= 1 {
				addr = args[0]
			}
			if len(args) == 2 {
				dir = args[1]
			}
			return a.ServeStudentPortal(addr, dir)
		},
	})
	accounts := &cobra.Command{
		Use:   "accounts",
		Short: "Manage student portal accounts",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	accounts.AddCommand(&cobra.Command{
		Use:   "init [default-password]",
		Args:  cobra.MaximumNArgs(1),
		Short: "Create portal accounts for students in the current course and term",
		RunE: func(cmd *cobra.Command, args []string) error {
			password := ""
			if len(args) == 1 {
				password = args[0]
			}
			return a.InitStudentPortalAccounts(password)
		},
	})
	resetCmd := &cobra.Command{
		Use:   "reset <student>",
		Args:  cobra.MinimumNArgs(1),
		Short: "Reset a student's portal password",
		RunE: func(cmd *cobra.Command, args []string) error {
			password, _ := cmd.Flags().GetString("password")
			return a.ResetStudentPortalPassword(strings.Join(args, " "), password)
		},
	}
	resetCmd.Flags().StringP("password", "p", "", "temporary password to set")
	accounts.AddCommand(resetCmd)
	cmd.AddCommand(accounts)
	return cmd
}

func newMigrateCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "migrate",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:  "up",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.MigrateUp()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:  "down",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.MigrateDown()
		},
	})
	return cmd
}

func newDBCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "db",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:  "reset",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ResetDB()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "backup [file]",
		Args:  cobra.MaximumNArgs(1),
		Short: "Create a backup of the database",
		RunE: func(cmd *cobra.Command, args []string) error {
			file := ""
			if len(args) == 1 {
				file = args[0]
			}
			return a.BackupDB(file)
		},
	})
	return cmd
}

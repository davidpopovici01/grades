package app

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
)

func (a *App) ListCategories() error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	rules, err := a.categoryRulesForContext(ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		fmt.Fprintln(a.out, "No categories configured for the current course and term.")
		return nil
	}
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Category\tWeight\tScoring\tDefault pass")
	for _, rule := range rules {
		weightLabel := "unweighted"
		if rule.HasWeight {
			weightLabel = fmt.Sprintf("%.1f%%", rule.WeightPercent)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", rule.CategoryName, weightLabel, categoryScoringLabel(rule), passPercentLabel(rule.DefaultPassPercent, true))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	total, err := a.categoryWeightTotal(ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Total weight:\t%.1f%%\n", total)
	return nil
}

func (a *App) SetCategoryWeight(value, rawWeight string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	categoryID, categoryName, err := a.resolveCategoryInteractive(value)
	if err != nil {
		return err
	}
	weight, err := parseWeight(rawWeight)
	if err != nil {
		return err
	}
	schemeID, err := a.ensureCourseTermScheme(ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		INSERT INTO category_scheme_weights(scheme_id, category_id, weight_percent)
		VALUES (?, ?, ?)
		ON CONFLICT(scheme_id, category_id) DO UPDATE SET weight_percent = excluded.weight_percent`,
		schemeID, categoryID, weight); err != nil {
		return err
	}
	total, err := a.categoryWeightTotal(ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Set category weight: %s = %.1f%%\n", categoryName, weight)
	fmt.Fprintf(a.out, "Total weight:\t%.1f%%\n", total)
	if total > 100 {
		fmt.Fprintln(a.out, "Warning: category weights are over 100%.")
	}
	return nil
}

func (a *App) SetCategoryPassRate(value, raw string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	categoryID, categoryName, err := a.resolveCategoryInteractive(value)
	if err != nil {
		return err
	}
	passPercent, schemeKey, err := parsePassRateSetting(raw)
	if err != nil {
		return err
	}
	if err := a.upsertCategoryPolicy(ctx.CourseYearID, ctx.TermID, categoryID, schemeKey, passPercent); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Set category pass rate: %s = %s\n", categoryName, passPercentLabel(passPercent, false))
	return nil
}

func parseWeight(raw string) (float64, error) {
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	if raw == "" {
		return 0, errors.New("weight cannot be blank")
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid weight: %s", raw)
	}
	if value < 0 || value > 100 {
		return 0, errors.New("weight must be between 0 and 100")
	}
	return value, nil
}

func (a *App) ensureCourseTermScheme(courseYearID, termID int) (int, error) {
	var schemeID sql.NullInt64
	err := a.db.QueryRow(`
		SELECT scheme_id
		FROM course_year_terms
		WHERE course_year_id = ? AND term_id = ?`, courseYearID, termID).Scan(&schemeID)
	if err == nil && schemeID.Valid {
		return int(schemeID.Int64), nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	var courseName string
	if err := a.db.QueryRow(`SELECT name FROM course_years WHERE course_year_id = ?`, courseYearID).Scan(&courseName); err != nil {
		return 0, err
	}
	var termName string
	if err := a.db.QueryRow(`SELECT name FROM terms WHERE term_id = ?`, termID).Scan(&termName); err != nil {
		return 0, err
	}
	res, err := a.db.Exec(`INSERT INTO category_schemes(name) VALUES (?)`, normalizeSpaces(courseName+" "+termName))
	if err != nil {
		return 0, err
	}
	lastID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	scheme := int(lastID)
	_, err = a.db.Exec(`
		INSERT INTO course_year_terms(course_year_id, term_id, scheme_id)
		VALUES (?, ?, ?)
		ON CONFLICT(course_year_id, term_id) DO UPDATE SET scheme_id = excluded.scheme_id`,
		courseYearID, termID, scheme)
	if err != nil {
		return 0, err
	}
	return scheme, nil
}

func (a *App) categoryWeightTotal(courseYearID, termID int) (float64, error) {
	var total sql.NullFloat64
	if err := a.db.QueryRow(`
		SELECT SUM(category_scheme_weights.weight_percent)
		FROM course_year_terms
		JOIN category_scheme_weights ON category_scheme_weights.scheme_id = course_year_terms.scheme_id
		WHERE course_year_terms.course_year_id = ? AND course_year_terms.term_id = ?`,
		courseYearID, termID).Scan(&total); err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Float64, nil
}

func (a *App) categoryPolicy(courseYearID, termID, categoryID int) (CategoryRule, bool, error) {
	var rule CategoryRule
	err := a.db.QueryRow(`
		SELECT category_id, name
		FROM categories
		WHERE category_id = ?`, categoryID).Scan(&rule.CategoryID, &rule.CategoryName)
	if err != nil {
		return CategoryRule{}, false, err
	}
	err = a.db.QueryRow(`
		SELECT scheme_key, default_pass_percent
		FROM category_grading_policies
		WHERE course_year_id = ? AND term_id = ? AND category_id = ?`, courseYearID, termID, categoryID).
		Scan(&rule.SchemeKey, &rule.DefaultPassPercent)
	if errors.Is(err, sql.ErrNoRows) {
		rule.SchemeKey = "average"
		return rule, false, nil
	}
	if err != nil {
		return CategoryRule{}, false, err
	}
	return rule, true, nil
}

func (a *App) upsertCategoryPolicy(courseYearID, termID, categoryID int, schemeKey string, passPercent sql.NullFloat64) error {
	_, err := a.db.Exec(`
		INSERT INTO category_grading_policies(course_year_id, term_id, category_id, scheme_key, default_pass_percent)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(course_year_id, term_id, category_id) DO UPDATE
		SET scheme_key = excluded.scheme_key,
		    default_pass_percent = excluded.default_pass_percent`,
		courseYearID, termID, categoryID, schemeKey, nullablePassRateValue(passPercent))
	return err
}

func parsePassRateSetting(raw string) (sql.NullFloat64, string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "", "inherit", "default":
		return sql.NullFloat64{}, "completion", nil
	case "raw", "none", "off":
		return sql.NullFloat64{Float64: 0, Valid: true}, "average", nil
	}
	value, err := parseRangeFloat(raw, "pass rate", 1, 100)
	if err != nil {
		return sql.NullFloat64{}, "", err
	}
	return sql.NullFloat64{Float64: value, Valid: true}, "completion", nil
}

func passPercentLabel(value sql.NullFloat64, defaulted bool) string {
	if !value.Valid {
		if defaulted {
			return "(none)"
		}
		return "category default"
	}
	if value.Float64 <= 0 {
		return "raw"
	}
	return fmt.Sprintf("%.1f%%", value.Float64)
}

func categoryScoringLabel(rule CategoryRule) string {
	switch rule.SchemeKey {
	case "completion":
		if rule.DefaultPassPercent.Valid && rule.DefaultPassPercent.Float64 > 0 {
			return "Pass-rate"
		}
		return "Raw average"
	case "total-points":
		return "Total points"
	default:
		return "Raw average"
	}
}

func nullablePassRateValue(value sql.NullFloat64) any {
	if !value.Valid {
		return nil
	}
	return value.Float64
}

func (a *App) ImportCategories(file string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return err
	}
	if len(rows) < 2 {
		return errors.New("category import file is empty")
	}

	headers := map[string]int{}
	for idx, header := range rows[0] {
		headers[normalizedCSVHeader(header)] = idx
	}
	if _, ok := headers["category"]; !ok {
		return errors.New("missing column: category")
	}

	imported := 0
	for rowIndex, row := range rows[1:] {
		if isSkippedImportRow(row) {
			continue
		}
		record, err := categoryImportRecordFromRow(headers, row)
		if err != nil {
			return fmt.Errorf("row %d: %w", rowIndex+2, err)
		}
		categoryID, err := a.ensureCategory(record.Category)
		if err != nil {
			return err
		}
		if record.Scheme != "" {
			scheme, err := resolveGradingScheme(record.Scheme)
			if err != nil {
				return fmt.Errorf("row %d: %w", rowIndex+2, err)
			}
			rule, exists, err := a.categoryPolicy(ctx.CourseYearID, ctx.TermID, categoryID)
			if err != nil {
				return err
			}
			passPercent := rule.DefaultPassPercent
			if scheme.Key != "completion" {
				passPercent = sql.NullFloat64{Float64: 0, Valid: true}
			} else if !exists && !passPercent.Valid {
				passPercent = sql.NullFloat64{Float64: 80, Valid: true}
			}
			if err := a.upsertCategoryPolicy(ctx.CourseYearID, ctx.TermID, categoryID, scheme.Key, passPercent); err != nil {
				return err
			}
		}
		if record.PassRate != nil {
			passPercent, schemeKey, err := parsePassRateSetting(*record.PassRate)
			if err != nil {
				return fmt.Errorf("row %d: %w", rowIndex+2, err)
			}
			rule, exists, err := a.categoryPolicy(ctx.CourseYearID, ctx.TermID, categoryID)
			if err != nil {
				return err
			}
			if !exists && record.Scheme == "" && schemeKey == "completion" && !passPercent.Valid {
				passPercent = sql.NullFloat64{Float64: 80, Valid: true}
			}
			if rule.SchemeKey != "" && record.Scheme == "" {
				schemeKey = rule.SchemeKey
			}
			if err := a.upsertCategoryPolicy(ctx.CourseYearID, ctx.TermID, categoryID, schemeKey, passPercent); err != nil {
				return err
			}
		}
		if record.Weight != nil {
			weight, err := parseWeight(*record.Weight)
			if err != nil {
				return fmt.Errorf("row %d: %w", rowIndex+2, err)
			}
			schemeID, err := a.ensureCourseTermScheme(ctx.CourseYearID, ctx.TermID)
			if err != nil {
				return err
			}
			if _, err := a.db.Exec(`
				INSERT INTO category_scheme_weights(scheme_id, category_id, weight_percent)
				VALUES (?, ?, ?)
				ON CONFLICT(scheme_id, category_id) DO UPDATE SET weight_percent = excluded.weight_percent`,
				schemeID, categoryID, weight); err != nil {
				return err
			}
		}
		imported++
	}
	fmt.Fprintf(a.out, "Imported %d category row(s)\n", imported)
	return nil
}

func (a *App) RunCategoryImportWizard() error {
	defaultFile := filepath.Join(a.homeDir, "categories_setup.csv")
	defaultExists := true
	if _, err := os.Stat(defaultFile); os.IsNotExist(err) {
		defaultExists = false
	} else if err != nil {
		return err
	}

	action, err := a.promptFileImportAction("categories", defaultExists)
	if err != nil {
		return err
	}

	switch action {
	case "import-default":
		if !defaultExists {
			fmt.Fprintf(a.out, "Default category file not found: %s\n", defaultFile)
			return a.WriteCategorySetupCSV(defaultFile)
		}
		return a.ImportCategoriesWithGuidance(defaultFile)
	case "create-default":
		if !defaultExists {
			fmt.Fprintf(a.out, "Default category file not found: %s\n", defaultFile)
		}
		return a.WriteCategorySetupCSV(defaultFile)
	default:
		file, err := a.promptPath("Category CSV file path")
		if err != nil {
			return err
		}
		if file == "" {
			fmt.Fprintf(a.out, "Default category file not found: %s\n", defaultFile)
			return a.WriteCategorySetupCSV(defaultFile)
		}
		if _, err := os.Stat(file); err == nil {
			return a.ImportCategoriesWithGuidance(file)
		}
		fmt.Fprintf(a.out, "Category file not found: %s\n", file)
		return a.WriteCategorySetupCSV(file)
	}
}

func (a *App) ImportCategoriesWithGuidance(file string) error {
	if err := a.ImportCategories(file); err != nil {
		if openErr := openFile(file); openErr == nil {
			fmt.Fprintf(a.out, "Import failed. Opened %s so you can fix it.\n", file)
		} else {
			fmt.Fprintf(a.out, "Import failed. Could not open %s automatically: %v\n", file, openErr)
		}
		return fmt.Errorf("%w\nFix the CSV and run `grades categories import` again", err)
	}
	return nil
}

func (a *App) WriteCategorySetupCSV(file string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	if strings.TrimSpace(file) == "" {
		file = filepath.Join(a.homeDir, "categories_setup.csv")
	}

	rows, err := a.categorySetupRows()
	if err != nil {
		return err
	}
	f, err := os.Create(file)
	if err != nil {
		return rosterTemplateWriteError(file, err)
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	data := [][]string{
		{"category", "weight", "scheme", "pass_rate"},
		{"# example row - importer ignores rows whose first cell starts with #", "40", "completion", "80"},
	}
	data = append(data, rows...)
	for _, row := range data {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	if err := openFile(file); err != nil {
		fmt.Fprintf(a.out, "Created category setup CSV: %s\n", file)
		fmt.Fprintf(a.out, "Could not open it automatically: %v\n", err)
		return nil
	}
	fmt.Fprintf(a.out, "Created category setup CSV: %s\n", file)
	fmt.Fprintln(a.out, "Opened category setup CSV.")
	return nil
}

func (a *App) categorySetupRows() ([][]string, error) {
	ctx := a.context()
	rules, err := a.categoryRulesForContext(ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return nil, err
	}
	out := make([][]string, 0, len(rules))
	for _, rule := range rules {
		weight := ""
		if rule.HasWeight {
			weight = fmt.Sprintf("%.1f", rule.WeightPercent)
		}
		out = append(out, []string{
			rule.CategoryName,
			weight,
			rule.SchemeKey,
			exportPassPercent(rule.DefaultPassPercent),
		})
	}
	return out, nil
}

type categoryImportRecord struct {
	Category string
	Weight   *string
	Scheme   string
	PassRate *string
}

func categoryImportRecordFromRow(headers map[string]int, row []string) (categoryImportRecord, error) {
	get := func(key string) string {
		idx, ok := headers[key]
		if !ok || idx >= len(row) {
			return ""
		}
		return normalizeSpaces(row[idx])
	}
	record := categoryImportRecord{
		Category: get("category"),
		Scheme:   get("scheme"),
	}
	if record.Category == "" {
		return categoryImportRecord{}, errors.New("category cannot be blank")
	}
	if raw := get("weight"); raw != "" {
		record.Weight = &raw
	}
	if raw := get("pass_rate"); raw != "" {
		record.PassRate = &raw
	}
	return record, nil
}

func exportPassPercent(value sql.NullFloat64) string {
	if !value.Valid {
		return ""
	}
	if value.Float64 <= 0 {
		return "raw"
	}
	return fmt.Sprintf("%.1f", value.Float64)
}

package app

import (
	"database/sql"
	"fmt"
	"io"
)

func (a *App) statsForQuery(query string, args ...any) (Stats, error) {
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return Stats{}, err
	}
	defer rows.Close()

	stats := Stats{}
	first := true
	for rows.Next() {
		var score float64
		if err := rows.Scan(&score); err != nil {
			return Stats{}, err
		}
		stats.Count++
		stats.Average += score
		if first || score > stats.Highest {
			stats.Highest = score
		}
		if first || score < stats.Lowest {
			stats.Lowest = score
		}
		first = false
	}
	if err := rows.Err(); err != nil {
		return Stats{}, err
	}
	if stats.Count > 0 {
		stats.Average /= float64(stats.Count)
	}
	return stats, nil
}

func printStats(out io.Writer, stats Stats) error {
	if stats.Count == 0 {
		_, err := fmt.Fprintln(out, "No graded scores found.")
		return err
	}
	fmt.Fprintf(out, "Average: %.1f\n", stats.Average)
	fmt.Fprintf(out, "Highest: %.0f\n", stats.Highest)
	fmt.Fprintf(out, "Lowest: %.0f\n", stats.Lowest)
	return nil
}

func printRows(out io.Writer, rows *sql.Rows) error {
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		fmt.Fprintf(out, "%d\t%s\n", id, name)
	}
	return rows.Err()
}

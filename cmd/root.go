// Package cmd holds the functionality for the raw grades command
package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	_ "modernc.org/sqlite"
)

var (
	db      *sql.DB
	cfgFile string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "grades",
	Short: "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	Run: func(cmd *cobra.Command, args []string) {
		class := viper.GetString("current_class_id")
		open := viper.GetString("open_assignment_id")

		fmt.Println("Grades CLI")
		fmt.Println("Context:")
		fmt.Printf("  Class: %s\n", fallback(class))
		fmt.Printf("  Open assignment: %s\n", fallback(open))
		viper.Set("current_class_id", "APCSA2")
		viper.WriteConfig()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig, initDB)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.grades.yaml)")
}

func initDB() {
	home, err := os.UserHomeDir()
	cobra.CheckErr(err)

	configDir := filepath.Join(home, ".grades")
	dbPath := filepath.Join(configDir, "grades.db")

	// Open SQLite database (file is created if it doesn't exist)
	conn, err := sql.Open("sqlite", dbPath)
	cobra.CheckErr(err)

	// Verify connection is usable now (not later)
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		cobra.CheckErr(err)
	}

	db = conn
	fmt.Fprintln(os.Stderr, "Using database:", dbPath)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	// Find home directory.
	home, err := os.UserHomeDir()
	cobra.CheckErr(err)

	// Define config directory
	configDir := filepath.Join(home, ".grades")

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		cobra.CheckErr(err)
	}

	// Search config in home directory with name ".grades" (without extension).
	viper.AddConfigPath(configDir)
	viper.SetConfigType("yaml")
	viper.SetConfigName("config")

	viper.AutomaticEnv() // read in environment variables that match

	// 6. Try reading config
	if err := viper.ReadInConfig(); err != nil {
		// Config not found: create new one
		configPath := filepath.Join(configDir, "config.yaml")

		// Create empty config file
		if err := viper.WriteConfigAs(configPath); err != nil {
			cobra.CheckErr(err)
		}

		fmt.Fprintln(os.Stderr, "Created new config file:", configPath)
	} else {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

func fallback(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

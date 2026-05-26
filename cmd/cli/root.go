package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/kaichao/gopkg/errors"
	"github.com/kaichao/gopkg/logger"
	"github.com/kaichao/gopkg/self"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "packfs",
	Short: "packfs command line tool",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// initializeEnv(envFile)
	},
}

func executeRoot() int {
	defer func() {
		if err := recover(); err != nil {
			fmt.Fprintf(os.Stderr, "[PANIC] err: %v\nstack: \n", err)
			fmt.Fprintln(os.Stderr, self.GetCurrentGoroutineStack())
		}
	}()
	if err := rootCmd.Execute(); err != nil {
		// Print the full command that caused the error
		defer fmt.Fprintf(os.Stderr, "Error executing command: %s\n", getFullCommand())
		// Check if it's a UsageError
		if _, ok := err.(*UsageError); ok {
			rootCmd.Usage()
			return 1
		}
		defer logger.LogError(err, logEntry)
		return errors.GetCode(err)
	}
	return 0
}

// getFullCommand returns the full command line that was executed
func getFullCommand() string {
	return os.Args[0] + " " + strings.Join(os.Args[1:], " ")
}

// UsageError represents an error that should display command usage
type UsageError struct {
	msg string
}

func (e *UsageError) Error() string {
	return e.msg
}

// NewUsageError creates a new UsageError
func NewUsageError(format string, args ...interface{}) *UsageError {
	return &UsageError{msg: fmt.Sprintf(format, args...)}
}

var logEntry *logrus.Entry

func init() {
	level, err := logrus.ParseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil {
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)
	logrus.SetReportCaller(true)
	formatter := &logrus.TextFormatter{
		DisableQuote: true,
	}
	logrus.SetFormatter(formatter)

	// 配置logger
	log := logrus.New()
	log.SetLevel(level)
	if level >= logrus.DebugLevel {
		// debug / trace
		log.SetFormatter(formatter)
	} else {
		log.SetFormatter(&logrus.JSONFormatter{})
	}
	logEntry = logrus.NewEntry(log)
}

func init() {
	// rootCmd.PersistentFlags().StringVarP(&envFile, "env-file", "e", "", "Environment variables file")
}

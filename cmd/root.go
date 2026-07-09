package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/BU-Neuromics/gosf/internal/config"
	"github.com/BU-Neuromics/gosf/internal/output"
)

// version is set at build time via -ldflags "-X github.com/BU-Neuromics/gosf/cmd.version=vX.Y.Z"
var version = "dev"

// Global flag values shared across commands.
var (
	flagToken  string
	flagOutput string
	flagQuiet  bool
	flagColor  string
)

var rootCmd = &cobra.Command{
	Use:          "gosf",
	Short:        "CLI for the Open Science Framework (osf.io)",
	Long:         "gosf — push, pull, and manage files on the Open Science Framework.",
	SilenceUsage: true,
	Version:      version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := config.InitViper(); err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if flagOutput != "text" && flagOutput != "json" {
			return fmt.Errorf("--output must be 'text' or 'json', got %q", flagOutput)
		}
		switch flagColor {
		case "auto", "always", "never":
		default:
			return fmt.Errorf("--color must be 'auto', 'always', or 'never', got %q", flagColor)
		}
		output.InitColor(flagColor, flagOutput == "json", flagQuiet)
		return nil
	},
}

// exitCodeError is an error that carries a specific exit code without printing
// an additional message. Returned by commands like gosf status.
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("exit code %d", e.code)
}

// Execute runs the root command and exits with a non-zero code on error.
// It installs a signal-aware context so that Ctrl-C (SIGINT) or SIGTERM
// cancels in-flight HTTP requests and aborts long transfers cleanly.
func Execute() {
	rootCmd.SilenceErrors = true // we print errors ourselves below

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		// exitCodeError: use the specified exit code with no message printed.
		var exitErr *exitCodeError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.code)
		}
		fmt.Fprintln(os.Stderr, output.Red("Error:"), err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "OSF personal access token (overrides env/config/keychain)")
	rootCmd.PersistentFlags().StringVar(&flagOutput, "output", "text", "Output format: text or json")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress progress and non-error output")
	rootCmd.PersistentFlags().StringVar(&flagColor, "color", "auto", "Colorize output: auto, always, or never")

	// Bind OSF_TOKEN env var via viper so --token and OSF_TOKEN work the same way.
	viper.SetEnvPrefix("OSF")
	viper.AutomaticEnv()
}

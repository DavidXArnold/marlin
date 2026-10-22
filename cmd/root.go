package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/update"
)

var osExit = os.Exit

var cfgFile string

// currentVersion holds the version string injected via ldflags at release time.
var currentVersion string

// updateNoticeCh receives the latest tag when an update is available.
// Buffered so the goroutine never blocks.
var updateNoticeCh = make(chan string, 1)

// checkForUpdate is a var so tests can stub it without hitting the network.
// Used by `marlin upgrade`, which always wants a live answer.
var checkForUpdate = update.Check

// updateCheckTTL bounds the background notice to one network check per day.
const updateCheckTTL = 24 * time.Hour

// checkForUpdateNotice backs the background "new version available" notice
// that runs on every command; it's cached so routine use (and scripts polling
// `marlin status`) doesn't exhaust GitHub's unauthenticated rate limit.
var checkForUpdateNotice = func(ctx context.Context, current string, cfg *config.Config) (string, bool, error) {
	if cfg == nil || cfg.Paths.UpdateCacheFile == "" {
		return update.Check(ctx, current)
	}
	return update.CheckCached(ctx, current, cfg.Paths.UpdateCacheFile, updateCheckTTL)
}

// updateCheckDone is closed when the background update check finishes;
// nil when no check was started.
var updateCheckDone chan struct{}

// updateCheckGrace is how long Execute waits, after the command finishes, for
// an in-flight update check. Fresh cache hits return in microseconds, so this
// only adds latency on the once-a-day network refresh — without it, fast
// commands would exit mid-request and the cache would never be written.
var updateCheckGrace = 750 * time.Millisecond

var rootCmd = &cobra.Command{
	Use:          "marlin",
	Short:        "Local LLM inference server manager",
	Long:         `marlin manages vLLM and other inference backends — model switching, registry search, validation, and service control.`,
	SilenceUsage: true, // don't dump usage on every runtime error
	// Start the update check once flags and config are resolved.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := globalConfig() // best-effort; nil means use safe default (enabled)
		if cfg == nil || cfg.Behavior.CheckUpdates {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			done := make(chan struct{})
			updateCheckDone = done
			go func() {
				defer close(done)
				defer cancel()
				latest, newer, _ := checkForUpdateNotice(ctx, currentVersion, cfg)
				if newer {
					select {
					case updateNoticeCh <- latest:
					default:
					}
				}
			}()
		}
		return nil
	},
}

func SetVersionInfo(version, commit, date string) {
	currentVersion = version
	rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date)
}

func Execute() {
	err := rootCmd.Execute()

	if updateCheckDone != nil {
		select {
		case <-updateCheckDone:
		case <-time.After(updateCheckGrace):
		}
	}

	// Non-blocking: show notice only if the goroutine finished in time.
	select {
	case latest := <-updateNoticeCh:
		fmt.Fprintf(os.Stderr, "\nnotice: marlin %s is available (running v%s)\n%s\n",
			latest, currentVersion,
			"https://github.com/DavidXArnold/marlin/releases/latest")
	default:
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
}

// Verbosity is set by the -v/-vv/-vvv persistent flag.
var Verbosity int

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: /etc/marlin/config.toml or $HOME/.config/marlin/config.toml)")
	rootCmd.PersistentFlags().CountVarP(&Verbosity, "verbose", "v", "verbosity: -v requests, -vv headers, -vvv bodies")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "output format: table, json, jsonl, plain")
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
		viper.AddConfigPath("/etc/marlin")
		viper.AddConfigPath("$HOME/.config/marlin")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintln(os.Stderr, "error reading config:", err)
			osExit(1)
		}
	}
}

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var osExit = os.Exit

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "marlin",
	Short: "Local LLM inference server manager",
	Long:  `marlin manages vLLM and other inference backends — model switching, registry search, validation, and service control.`,
}

func SetVersionInfo(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: /etc/marlin/config.toml or $HOME/.config/marlin/config.toml)")
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

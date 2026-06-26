package cmd

import (
	marlinConfigs "github.com/DavidXArnold/marlin/configs"
	"github.com/DavidXArnold/marlin/internal/config"
)

func init() {
	config.BundledModels = marlinConfigs.Models
}

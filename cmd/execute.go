package cmd

import (
	"fmt"

	"github.com/akerl/cultivator/cultivator"
	"github.com/spf13/cobra"
)

func executeRunner(cmd *cobra.Command, _ []string) error {
	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}
	if configFile == "" {
		return fmt.Errorf("no config file given")
	}

	c, err := cultivator.NewFromFile(configFile)
	if err != nil {
		return err
	}

	return c.Execute()
}

var executeCmd = &cobra.Command{
	Use:   "execute",
	Short: "Execute checks on set of repos",
	RunE:  executeRunner,
}

func init() {
	rootCmd.AddCommand(executeCmd)
	executeCmd.Flags().StringP("config", "c", "", "Config file")
}

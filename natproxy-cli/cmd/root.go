package cmd

import (
	"fmt"
	"os"

	"natproxy/cli/internal/config"

	"github.com/spf13/cobra"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:   "natproxy-cli",
	Short: "NATProxy CLI — P2P Internet Sharing for Desktop",
	Long:  "Standalone CLI tool that replicates every feature of the NATProxy Flutter app for desktop (Linux/macOS/Windows).\nOn desktop the client exposes a local SOCKS5 proxy instead of a TUN/VPN.",
}

func Execute() {
	config.Init()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose log output")
}

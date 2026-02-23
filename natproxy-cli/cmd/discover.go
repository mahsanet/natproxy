package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"natproxy/cli/internal/orchestrator"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "List available servers from the signaling server",
	RunE: func(cmd *cobra.Command, args []string) error {
		sigURL := viper.GetString("discovery_url")
		room, _ := cmd.Flags().GetString("room")
		jsonOutput, _ := cmd.Flags().GetBool("json")

		raw, err := orchestrator.ListServers(sigURL, room)
		if err != nil {
			return fmt.Errorf("list servers: %w", err)
		}

		if jsonOutput {
			fmt.Println(raw)
			return nil
		}

		var servers []map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &servers); err != nil {
			fmt.Println(raw)
			return nil
		}

		if len(servers) == 0 {
			fmt.Fprintln(os.Stderr, "No servers found.")
			return nil
		}

		fmt.Println("Available Servers:")
		for i, s := range servers {
			name, _ := s["name"].(string)
			method, _ := s["method"].(string)
			transport, _ := s["transport"].(string)
			protocol, _ := s["protocol"].(string)
			sRoom, _ := s["room"].(string)

			desc := method
			if protocol != "" && protocol != method {
				desc += " / " + protocol
			}
			if transport != "" && transport != method {
				desc += " / " + transport
			}

			roomStr := ""
			if sRoom != "" {
				roomStr = " | Room: " + sRoom
			}
			fmt.Printf("  [%d] %-20s | %-30s%s\n", i+1, name, desc, roomStr)
		}

		return nil
	},
}

func init() {
	discoverCmd.Flags().String("discovery-url", "", "Discovery server URL")
	discoverCmd.Flags().String("room", "", "Filter by room")
	discoverCmd.Flags().Bool("json", false, "JSON output")
	viper.BindPFlag("discovery_url", discoverCmd.Flags().Lookup("discovery-url"))
	rootCmd.AddCommand(discoverCmd)
}

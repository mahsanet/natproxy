package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"natproxy/cli/internal/config"
	"natproxy/cli/internal/display"
	"natproxy/golib/nat"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var natCmd = &cobra.Command{
	Use:   "nat",
	Short: "Detect NAT type and public IP",
	RunE: func(cmd *cobra.Command, args []string) error {
		stunServer := viper.GetString("stun_server")
		jsonOutput, _ := cmd.Flags().GetBool("json")

		fmt.Fprintln(os.Stderr, "Detecting NAT type...")

		var natType string
		var publicIP string
		var publicPort int
		natResult, err := nat.DetectNATTypeFull(stunServer, config.DefaultStunServer2)
		if err != nil {
			natType = "unknown"
		} else {
			natType = natResult.Mapping.String()
			publicIP = natResult.MappedIP
			publicPort = natResult.MappedPort
		}

		// Fall back to separate STUN query if detection didn't return an address
		if publicIP == "" {
			fmt.Fprintln(os.Stderr, "Discovering public IP...")
			publicIP, publicPort, err = nat.DiscoverPublicAddr(stunServer)
			if err != nil {
				publicIP = "unknown"
			}
		}

		if jsonOutput {
			result := map[string]interface{}{
				"nat_type":    natType,
				"public_ip":   publicIP,
				"public_port": publicPort,
			}
			if natResult != nil {
				result["mapping"] = natResult.Mapping.String()
				result["filtering"] = natResult.Filtering.String()
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
		} else {
			fmt.Printf("NAT Type:    %s\n", display.FormatNATType(natType))
			fmt.Printf("Public IP:   %s\n", publicIP)
			if publicPort > 0 {
				fmt.Printf("Public Port: %d\n", publicPort)
			}
		}

		return nil
	},
}

func init() {
	natCmd.Flags().String("stun-server", "", "STUN server host:port")
	natCmd.Flags().Bool("json", false, "JSON output")
	viper.BindPFlag("stun_server", natCmd.Flags().Lookup("stun-server"))
	rootCmd.AddCommand(natCmd)
}

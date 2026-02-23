package display

import "fmt"

func FormatBytes(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func FormatDuration(seconds int) string {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func FormatNATType(natType string) string {
	switch natType {
	case "endpoint_independent":
		return "Endpoint Independent"
	case "address_dependent":
		return "Address Dependent"
	case "address_port_dependent":
		return "Address+Port Dependent"
	default:
		return "Unknown"
	}
}

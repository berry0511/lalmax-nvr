package gb28181

import (
	"fmt"
	"log/slog"
	"net"
	"strings"
)

// Config holds GB28181 SIP signaling configuration.
type Config struct {
	Enabled         bool   `yaml:"enabled"`
	Host            string `yaml:"host"`             // SIP listen host (empty = auto-detect)
	Port            int    `yaml:"port"`             // SIP listen port (default 5060)
	ID              string `yaml:"id"`               // 20-digit platform SIP ID
	Password        string `yaml:"password"`         // Global device registration password
	MediaIP         string `yaml:"media_ip"`         // IP address put in SDP for media reception
	MediaPort       int    `yaml:"media_port"`       // RTP media port (0=auto/multi-port, >0=single port mode)
	StandardVersion string `yaml:"standard_version"` // GB28181 standard version: 2016 or 2022

	// Upstream platform cascading
	Platforms []PlatformConfig `yaml:"platforms,omitempty"`
}

// GBVersion returns the configured GB28181 standard version.
func (c *Config) GBVersion() GBVersion {
	return NormalizeGBVersion(c.StandardVersion)
}

// GetDomain extracts the first 10 chars of the SIP ID as the GB domain.
func (c *Config) GetDomain() string {
	if len(c.ID) >= 10 {
		return c.ID[:10]
	}
	return c.ID
}

// Validate checks the configuration for correctness.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	c.detectMediaIP()
	if len(c.ID) < 10 || len(c.ID) > 20 {
		return fmt.Errorf("gb28181.id must be 10-20 digits, got %d", len(c.ID))
	}
	for _, digit := range c.ID {
		if digit < '0' || digit > '9' {
			return fmt.Errorf("gb28181.id must contain only digits")
		}
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("gb28181.port must be 1-65535, got %d", c.Port)
	}
	if c.MediaIP == "" {
		return fmt.Errorf("gb28181.media_ip is required")
	}
	if strings.TrimSpace(c.Password) == "" || strings.TrimSpace(c.Password) == "#" {
		return fmt.Errorf("gb28181.password is required when GB28181 is enabled")
	}
	if version := c.GBVersion(); version == GBVersionUnknown {
		return fmt.Errorf("gb28181.standard_version must be 2016 or 2022, got %s", c.StandardVersion)
	}
	mediaIP := net.ParseIP(c.MediaIP)
	if mediaIP == nil {
		return fmt.Errorf("gb28181.media_ip must be a valid IP address, got %s", c.MediaIP)
	}
	if mediaIP.IsUnspecified() {
		return fmt.Errorf("gb28181.media_ip must be a reachable media server IP, got %s", c.MediaIP)
	}
	return nil
}

// ApplyDefaults sets default values for the configuration.
func (c *Config) ApplyDefaults() {
	if c.Port == 0 {
		c.Port = 5060
	}
	c.StandardVersion = string(NormalizeGBVersion(c.StandardVersion))
	c.detectMediaIP()
}

func NormalizeGBVersion(version string) GBVersion {
	switch strings.TrimSpace(version) {
	case "", string(GBVersion2016), "GB/T28181-2016", "GB28181-2016":
		return GBVersion2016
	case string(GBVersion2022), "GB/T28181-2022", "GB28181-2022":
		return GBVersion2022
	default:
		return GBVersionUnknown
	}
}

func (c *Config) detectMediaIP() {
	if c.MediaIP != "" {
		ip := net.ParseIP(c.MediaIP)
		if ip != nil && !ip.IsUnspecified() {
			return
		}
	}
	if ip := firstNonLoopbackIPv4(); ip != "" {
		slog.Info("GB28181 media_ip auto-detected", "media_ip", ip)
		c.MediaIP = ip
	}
}

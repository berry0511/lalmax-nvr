package gb28181

import (
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
)

// Construction must report port conflicts synchronously instead of logging
// "started" for a server that can never accept REGISTER and SIP INVITE.
func TestNewServerCheckedFailsOnOccupiedUDPPort(t *testing.T) {
	occupied, err := net.ListenPacket("udp", "0.0.0.0:0")
	if err != nil { t.Fatal(err) }
	defer occupied.Close()
	port := occupied.LocalAddr().(*net.UDPAddr).Port

	db, err := storage.New(filepath.Join(t.TempDir(), "nvr.sqlite"))
	if err != nil { t.Fatal(err) }
	defer db.Close()

	cfg := &Config{
		Enabled: true,
		Port: port,
		Host: "127.0.0.1",
		MediaIP: "127.0.0.1",
		ID: "34020000002000000001",
		Password: "test-password-123",
		StandardVersion: "2016",
	}
	server, err := NewServerChecked(cfg, nil, db)
	if err == nil {
		if server != nil { server.Stop() }
		t.Fatal("server reported ready on an occupied UDP port")
	}
	if server != nil { t.Fatal("partially started server escaped") }
	if !strings.Contains(err.Error(), "bind GB28181 SIP UDP") {
		t.Fatalf("missing protocol/port context: %v", err)
	}
}

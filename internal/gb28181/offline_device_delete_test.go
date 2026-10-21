package gb28181

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
)

func TestDeleteOfflineGB28181DevicePreservesOnlineRegistration(t *testing.T) {
	db, err := storage.New(filepath.Join(t.TempDir(), "devices.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Init(context.Background()); err != nil { t.Fatal(err) }
	store := NewDeviceStore(db, nil)

	const onlineID = "34020000001320000001"
	const offlineID = "34020000001320000002"
	online := &Device{DeviceID: onlineID, IsOnline: true}
	offline := &Device{DeviceID: offlineID, IsOnline: false}
	store.Store(onlineID, online)
	store.Store(offlineID, offline)
	if err := store.SaveDevice(onlineID, online); err != nil { t.Fatal(err) }
	if err := store.SaveDevice(offlineID, offline); err != nil { t.Fatal(err) }

	if err := store.DeleteOfflineDevice(onlineID); !errors.Is(err, ErrDeviceOnline) {
		t.Fatalf("online deletion result = %v", err)
	}
	if _, ok := store.Load(onlineID); !ok { t.Fatal("online device removed from cache") }
	if row, err := db.GetGB28181Device(context.Background(), onlineID); err != nil || row == nil {
		t.Fatalf("online device removed from DB: row=%v err=%v", row, err)
	}
	if err := store.DeleteOfflineDevice(offlineID); err != nil { t.Fatal(err) }
	if _, ok := store.Load(offlineID); ok { t.Fatal("offline device remains in cache") }
	if row, err := db.GetGB28181Device(context.Background(), offlineID); !errors.Is(err, sql.ErrNoRows) || row != nil {
		t.Fatalf("offline device remains in DB: row=%v err=%v", row, err)
	}
	if err := store.DeleteOfflineDevice(offlineID); !errors.Is(err, ErrDeviceNotExist) {
		t.Fatalf("repeat delete = %v, want not found", err)
	}
}

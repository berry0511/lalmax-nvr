package gb28181

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

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


// blockingDeleteGB28181Repository exposes the interval between acquiring the
// store lifecycle lock and completing the persistent device deletion.
type blockingDeleteGB28181Repository struct {
	storage.GB28181DeviceStoreRepository
	deleteStarted chan struct{}
	allowDelete   chan struct{}
}

func (r *blockingDeleteGB28181Repository) DeleteGB28181Device(ctx context.Context, deviceID string) error {
	close(r.deleteStarted)
	<-r.allowDelete
	return r.GB28181DeviceStoreRepository.DeleteGB28181Device(ctx, deviceID)
}

func TestREGISTERAfterConcurrentOfflineDeleteRecreatesPersistedDevice(t *testing.T) {
	db, err := storage.New(filepath.Join(t.TempDir(), "register-delete-race.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Init(context.Background()); err != nil { t.Fatal(err) }
	repo := &blockingDeleteGB28181Repository{
		GB28181DeviceStoreRepository: db,
		deleteStarted: make(chan struct{}),
		allowDelete: make(chan struct{}),
	}
	store := NewDeviceStore(repo, nil)
	const id = "34020000001320000991"
	dev := &Device{DeviceID: id, IsOnline: false, GBVersion: GBVersionUnknown}
	store.Store(id, dev)
	if err := store.SaveDevice(id, dev); err != nil { t.Fatal(err) }

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- store.DeleteOfflineDevice(id) }()
	<-repo.deleteStarted
	registerStarted := make(chan struct{})
	registerDone := make(chan error, 1)
	go func() {
		close(registerStarted)
		_, err := store.ChangeAndSaveDevice(id, &Device{
			DeviceID: id, source: "127.0.0.1:5060", GBVersion: GBVersionUnknown,
		}, func(d *Device) {
			d.IsOnline = true
			d.Address = "127.0.0.1:5060"
			d.Transport = "UDP"
			d.LastRegisterAt = time.Now()
			d.LastKeepaliveAt = time.Now()
		})
		registerDone <- err
	}()
	<-registerStarted
	select {
	case err := <-registerDone:
		t.Fatalf("REGISTER escaped while deletion held lifecycle lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(repo.allowDelete)
	if err := <-deleteDone; err != nil { t.Fatalf("delete: %v", err) }
	if err := <-registerDone; err != nil { t.Fatalf("REGISTER transition: %v", err) }
	registered, ok := store.Load(id)
	if !ok || !registered.IsOnline {
		t.Fatalf("REGISTER completed but online device is absent: present=%v dev=%+v", ok, registered)
	}
	row, err := db.GetGB28181Device(context.Background(), id)
	if err != nil || row == nil || !row.IsOnline {
		t.Fatalf("REGISTER state not persisted after delete: row=%+v err=%v", row, err)
	}
}

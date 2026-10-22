package gb28181

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
)

type deviceReplyWriteBarrier struct {
	storage.GB28181DeviceStoreRepository
	writeKind string
	armed     bool
	once      sync.Once
	entered   chan struct{}
	release   chan struct{}
}

func (r *deviceReplyWriteBarrier) beforeWrite(kind string) {
	if r.armed && kind == r.writeKind {
		r.once.Do(func() {
			close(r.entered)
			<-r.release
		})
	}
}

func (r *deviceReplyWriteBarrier) UpsertGB28181Device(ctx context.Context, row *storage.GB28181DeviceRow) error {
	r.beforeWrite("device")
	return r.GB28181DeviceStoreRepository.UpsertGB28181Device(ctx, row)
}

func (r *deviceReplyWriteBarrier) BatchUpsertChannels(ctx context.Context, deviceID string, channels []storage.GB28181ChannelRow) error {
	r.beforeWrite("channels")
	return r.GB28181DeviceStoreRepository.BatchUpsertChannels(ctx, deviceID, channels)
}

const deletedReplyDeviceID = "34020000001320000993"

func newDeviceReplyTestStore(t *testing.T, writeKind string) (*storage.DB, *DeviceStore, *deviceReplyWriteBarrier) {
	t.Helper()
	db, err := storage.New(filepath.Join(t.TempDir(), "device-reply.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	repo := &deviceReplyWriteBarrier{
		GB28181DeviceStoreRepository: db,
		writeKind:                    writeKind,
		entered:                      make(chan struct{}),
		release:                      make(chan struct{}),
	}
	store := NewDeviceStore(repo, nil)
	dev := &Device{DeviceID: deletedReplyDeviceID, GBVersion: GBVersion2016}
	store.Store(deletedReplyDeviceID, dev)
	if err := store.SaveDevice(deletedReplyDeviceID, dev); err != nil {
		t.Fatal(err)
	}
	return db, store, repo
}

func deliverDeviceReply(api *GB28181API, kind string) {
	if kind == "info" {
		api.handleDeviceInfoResponse(deletedReplyDeviceID, []byte(`<Response><CmdType>DeviceInfo</CmdType><DeviceID>`+deletedReplyDeviceID+`</DeviceID><Manufacturer>Vendor</Manufacturer><Model>Camera</Model><Firmware>1.0</Firmware></Response>`))
		return
	}
	api.handleCatalogResponse(deletedReplyDeviceID, []byte(`<Response><CmdType>Catalog</CmdType><DeviceID>`+deletedReplyDeviceID+`</DeviceID><SumNum>1</SumNum><DeviceList Num="1"><Item><DeviceID>34020000001320000001</DeviceID><Name>Camera</Name></Item></DeviceList></Response>`))
}

func assertDeviceReplyDeletion(t *testing.T, db *storage.DB, store *DeviceStore) {
	t.Helper()
	if _, ok := store.Load(deletedReplyDeviceID); ok {
		t.Error("deleted device remains in memory")
	}
	row, err := db.GetGB28181Device(context.Background(), deletedReplyDeviceID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("reply recreated deleted device: row=%+v err=%v", row, err)
	}
	channels, err := db.ListGB28181Channels(context.Background(), deletedReplyDeviceID)
	if err != nil || len(channels) != 0 {
		t.Errorf("reply left deleted channels: channels=%+v err=%v", channels, err)
	}
	reloaded := NewDeviceStore(db, nil)
	if err := reloaded.LoadFromDB(); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Load(deletedReplyDeviceID); ok {
		t.Error("deleted device reappeared after reloading persisted devices")
	}
}

func TestLateDeviceRepliesDoNotRecreateDeletedDevice(t *testing.T) {
	for _, kind := range []string{"info", "catalog"} {
		t.Run(kind, func(t *testing.T) {
			db, store, _ := newDeviceReplyTestStore(t, "")
			if err := store.DeleteOfflineDevice(deletedReplyDeviceID); err != nil {
				t.Fatal(err)
			}
			api := &GB28181API{store: store, cfg: &Config{StandardVersion: "2016"}}
			deliverDeviceReply(api, kind)
			assertDeviceReplyDeletion(t, db, store)
		})
	}
}

func TestOfflineDeleteWaitsForDeviceReplyPersistence(t *testing.T) {
	for _, tc := range []struct{ replyKind, writeKind string }{
		{"info", "device"},
		{"catalog", "device"},
		{"catalog", "channels"},
	} {
		t.Run(tc.replyKind+"_"+tc.writeKind, func(t *testing.T) {
			db, store, repo := newDeviceReplyTestStore(t, tc.writeKind)
			repo.armed = true
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(repo.release) }) }
			defer release()
			api := &GB28181API{store: store, cfg: &Config{StandardVersion: "2016"}}
			replyDone := make(chan struct{})
			go func() {
				defer close(replyDone)
				deliverDeviceReply(api, tc.replyKind)
			}()
			select {
			case <-repo.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("reply did not reach persistence")
			}

			deleteStarted := make(chan struct{})
			deleteDone := make(chan error, 1)
			go func() {
				close(deleteStarted)
				deleteDone <- store.DeleteOfflineDevice(deletedReplyDeviceID)
			}()
			<-deleteStarted
			select {
			case err := <-deleteDone:
				release()
				<-replyDone
				t.Fatalf("deletion completed before the in-flight reply persisted: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			release()
			select {
			case <-replyDone:
			case <-time.After(5 * time.Second):
				t.Fatal("reply did not complete")
			}
			select {
			case err := <-deleteDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("deletion did not complete after reply persistence")
			}
			assertDeviceReplyDeletion(t, db, store)
		})
	}
}

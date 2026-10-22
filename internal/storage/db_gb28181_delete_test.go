package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newGB28181DeleteTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(filepath.Join(t.TempDir(), "gb28181-delete.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	db.DB().SetMaxOpenConns(1)
	require.NoError(t, db.Init(context.Background()))
	_, err = db.DB().Exec("PRAGMA foreign_keys = OFF;")
	require.NoError(t, err)
	for _, id := range []string{"deleted-device", "retained-device"} {
		require.NoError(t, db.UpsertGB28181Device(context.Background(), &GB28181DeviceRow{DeviceID: id}))
		require.NoError(t, db.BatchUpsertChannels(context.Background(), id, []GB28181ChannelRow{{DeviceID: id, ChannelID: "camera", Name: id}}))
		channels, err := db.ListGB28181Channels(context.Background(), id)
		require.NoError(t, err)
		require.Len(t, channels, 1)
	}
	return db
}

func TestDeleteGB28181DeviceRemovesOwnChannelsWithoutForeignKeys(t *testing.T) {
	db := newGB28181DeleteTestDB(t)
	ctx := context.Background()
	require.NoError(t, db.DeleteGB28181Device(ctx, "deleted-device"))
	_, err := db.GetGB28181Device(ctx, "deleted-device")
	require.ErrorIs(t, err, sql.ErrNoRows)
	channels, err := db.ListGB28181Channels(ctx, "deleted-device")
	require.NoError(t, err)
	require.Empty(t, channels)
	_, err = db.GetGB28181Device(ctx, "retained-device")
	require.NoError(t, err)
	channels, err = db.ListGB28181Channels(ctx, "retained-device")
	require.NoError(t, err)
	require.Len(t, channels, 1)
}

func TestDeleteGB28181DeviceRollsBackChannelsOnDeviceDeleteFailure(t *testing.T) {
	db := newGB28181DeleteTestDB(t)
	ctx := context.Background()
	_, err := db.DB().Exec(`CREATE TRIGGER reject_device_delete
		BEFORE DELETE ON gb28181_devices WHEN OLD.device_id = 'deleted-device'
		BEGIN SELECT RAISE(ABORT, 'test device delete failure'); END;`)
	require.NoError(t, err)
	require.Error(t, db.DeleteGB28181Device(ctx, "deleted-device"))
	_, err = db.GetGB28181Device(ctx, "deleted-device")
	require.NoError(t, err)
	channels, err := db.ListGB28181Channels(ctx, "deleted-device")
	require.NoError(t, err)
	require.Len(t, channels, 1, "failed device removal must roll back the catalog removal")
}

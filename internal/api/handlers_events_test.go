package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lalmax-pro/lalmax-nvr/internal/middleware"
	"github.com/lalmax-pro/lalmax-nvr/internal/model"
	"github.com/stretchr/testify/require"
)

func TestEvents_ListGetAck(t *testing.T) {
	db, store := setupTestDB(t)
	t.Cleanup(func() { db.Close() })
	h := TestHandler(db, store)

	eventID, err := db.InsertEvent(context.Background(), model.Event{
		CameraID:  "front-door",
		Source:    model.EventSourceHealth,
		Type:      string(model.HealthEventConnectionLost),
		Severity:  model.EventSeverityCritical,
		Status:    model.EventStatusOpen,
		Message:   "connection lost",
		Metadata:  "{}",
		StartedAt: time.Now().UTC(),
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	rr := doRequest(t, h.Routes(), "GET", "/api/events?camera_id=front-door", nil, "", "")
	require.Equal(t, http.StatusOK, rr.Code)
	var listResp struct {
		Events []model.Event `json:"events"`
		Total  int           `json:"total"`
	}
	parseJSON(t, rr, &listResp)
	require.Equal(t, 1, listResp.Total)
	require.Len(t, listResp.Events, 1)
	require.Equal(t, eventID, listResp.Events[0].ID)

	rr = doRequest(t, h.Routes(), "GET", "/api/events/"+toString(eventID), nil, "", "")
	require.Equal(t, http.StatusOK, rr.Code)
	var got model.Event
	parseJSON(t, rr, &got)
	require.Equal(t, "front-door", got.CameraID)

	rr = doRequest(t, h.Routes(), "POST", "/api/events/"+toString(eventID)+"/ack", nil, "", "")
	require.Equal(t, http.StatusOK, rr.Code)

	ack, err := db.GetEvent(context.Background(), eventID)
	require.NoError(t, err)
	require.Equal(t, model.EventStatusAcknowledged, ack.Status)
	require.NotNil(t, ack.AcknowledgedAt)
}

func TestEvents_AcknowledgeRequiresOperatePermission(t *testing.T) {
	db, store := setupTestDB(t)
	t.Cleanup(func() { db.Close() })
	h := TestHandler(db, store)
	h.SetMultiUserAuthMW(middleware.InjectUser(&model.User{Role: model.RoleUser}))

	eventID, err := db.InsertEvent(context.Background(), model.Event{
		CameraID: "front-door", Source: model.EventSourceHealth, Type: "connection_lost",
		Severity: model.EventSeverityCritical, Status: model.EventStatusOpen,
		StartedAt: time.Now().UTC(), CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	rr := doRequest(t, h.Routes(), "POST", "/api/events/"+toString(eventID)+"/ack", nil, "", "")
	require.Equal(t, http.StatusForbidden, rr.Code)

	got, err := db.GetEvent(context.Background(), eventID)
	require.NoError(t, err)
	require.Equal(t, model.EventStatusOpen, got.Status)
}

func TestEvents_CreateRuleRequiresActionTarget(t *testing.T) {
	db, store := setupTestDB(t)
	t.Cleanup(func() { db.Close() })
	h := TestHandler(db, store)

	for _, action := range []string{"webhook", "goto_preset"} {
		rr := doRequest(t, h.Routes(), "POST", "/api/events/rules", strings.NewReader(`{"name":"test","action":"`+action+`"}`), "", "")
		require.Equal(t, http.StatusBadRequest, rr.Code, "action %s must have a target", action)
	}
}

func toString(id int64) string {
	return strconv.FormatInt(id, 10)
}

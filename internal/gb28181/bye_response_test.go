package gb28181

import "testing"

func TestBYERemoteDialogAlreadyGoneAllowsCleanup(t *testing.T) {
	for _, tc := range []struct {
		status int
		cleanup bool
	}{
		{200, true}, {202, true}, {481, true},
		{400, false}, {403, false}, {408, false}, {486, false}, {500, false},
	} {
		if got := byeConfirmsRemoteDialogGone(tc.status); got != tc.cleanup {
			t.Errorf("SIP BYE response %d permits cleanup=%t, want %t", tc.status, got, tc.cleanup)
		}
	}
}

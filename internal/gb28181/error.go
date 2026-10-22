package gb28181

import "errors"

var (
	ErrXMLDecode = errors.New("xml decode error")
)

var (
	ErrDeviceNotExist  = errors.New("device not exist")
	ErrChannelNotExist = errors.New("channel not exist")
	ErrDeviceOffline   = errors.New("device offline")
	ErrDeviceOnline    = errors.New("device is online")
	ErrDeviceHasActiveStream = errors.New("device still has an active GB28181 stream")
	ErrDeviceSourceMismatch = errors.New("device keepalive source IP mismatch")
	ErrChannelOffline  = errors.New("channel offline")
)

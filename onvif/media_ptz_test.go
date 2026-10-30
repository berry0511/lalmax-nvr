package onvif

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func connectedMediaClient(t *testing.T, handler func(body string) string) *Client {
	t.Helper()
	server := newONVIFDeviceServer(t, handler)
	client := newTestClient(t, server)
	mediaURL, err := url.Parse(server.URL + "/onvif/media")
	require.NoError(t, err)
	ptzURL, err := url.Parse(server.URL + "/onvif/ptz")
	require.NoError(t, err)
	media2URL, err := url.Parse(server.URL + "/onvif/media2")
	require.NoError(t, err)
	client.endpoints = &ServiceEndpoints{Device: mediaURL, Media: mediaURL, Media2: media2URL, PTZ: ptzURL}
	client.soap = NewSOAPClient(server.Client(), "admin", "secret")
	client.mediaService = NewMediaService(client)
	client.ptzService = NewPTZService(client)
	return client
}

func TestGetProfilesMedia1AndMissingEndpoint(t *testing.T) {
	client := connectedMediaClient(t, func(body string) string {
		if strings.Contains(body, "GetProfiles") && strings.Contains(body, "ver10/media") {
			return soapOK(`<GetProfilesResponse>
				<Profiles token="main">
					<Name>Main</Name>
					<VideoSourceConfiguration token="vs0"/>
					<VideoEncoderConfiguration token="ve0">
						<Encoding>H264</Encoding>
						<Resolution><Width>1920</Width><Height>1080</Height></Resolution>
						<RateControl><FrameRateLimit>25</FrameRateLimit><BitrateLimit>4096</BitrateLimit></RateControl>
					</VideoEncoderConfiguration>
					<AudioSourceConfiguration token="as0"/>
					<AudioEncoderConfiguration token="ae0"/>
				</Profiles>
			</GetProfilesResponse>`)
		}
		if strings.Contains(body, "GetProfiles") {
			return soapFaultXML("s:Receiver", "no media2")
		}
		return ""
	})
	profiles, err := client.MediaService().GetProfiles(context.Background())
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	require.Equal(t, "main", profiles[0].Token)
	require.Equal(t, "Main", profiles[0].Name)
	require.Equal(t, "H264", profiles[0].Encoding)
	require.Equal(t, 1920, profiles[0].Resolution.Width)

	empty := NewMediaService(&Client{endpoints: &ServiceEndpoints{}})
	_, err = empty.GetProfiles(context.Background())
	require.ErrorContains(t, err, "no media service available")
}

func TestGetStreamURIMedia1Fallback(t *testing.T) {
	client := connectedMediaClient(t, func(body string) string {
		if strings.Contains(body, "ver20/media") {
			return soapFaultXML("s:Receiver", "no media2")
		}
		if strings.Contains(body, "GetStreamUri") {
			return soapOK(`<GetStreamUriResponse><MediaUri><Uri>rtsp://127.0.0.1/live</Uri></MediaUri></GetStreamUriResponse>`)
		}
		return ""
	})
	uri, err := client.MediaService().GetStreamURI(context.Background(), "main")
	require.NoError(t, err)
	require.Equal(t, "rtsp://127.0.0.1/live", uri)

	empty := NewMediaService(&Client{endpoints: &ServiceEndpoints{}})
	_, err = empty.GetStreamURI(context.Background(), "main")
	require.ErrorContains(t, err, "no media service available")
}

func TestGetSnapshotUriMedia2ThenMedia1(t *testing.T) {
	client := connectedMediaClient(t, func(body string) string {
		if strings.Contains(body, "ver20/media") && strings.Contains(body, "GetSnapshotUri") {
			return soapOK(`<GetSnapshotUriResponse><Uri></Uri></GetSnapshotUriResponse>`)
		}
		if strings.Contains(body, "GetSnapshotUri") {
			return soapOK(`<GetSnapshotUriResponse><MediaUri><Uri>http://127.0.0.1/snap.jpg</Uri></MediaUri></GetSnapshotUriResponse>`)
		}
		return ""
	})
	uri, err := client.MediaService().GetSnapshotUri(context.Background(), "main")
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1/snap.jpg", uri)

	empty := NewMediaService(&Client{endpoints: &ServiceEndpoints{}})
	_, err = empty.GetSnapshotUri(context.Background(), "main")
	require.ErrorContains(t, err, "Media service not available")
}

func TestPTZStatusMoveAndStop(t *testing.T) {
	client := connectedMediaClient(t, func(body string) string {
		switch {
		case strings.Contains(body, "GetStatus"):
			return soapOK(`<GetStatusResponse><Status>
				<Position><PanTilt x="0.1" y="-0.2"/><Zoom x="0.5"/></Position>
				<MoveStatus><PanTilt>MOVING</PanTilt><Zoom>IDLE</Zoom></MoveStatus>
			</Status></GetStatusResponse>`)
		case strings.Contains(body, "ContinuousMove"), strings.Contains(body, "Stop>"):
			return soapOK(`<Response/>`)
		default:
			return ""
		}
	})
	status, err := client.PTZService().GetStatus(context.Background(), "main")
	require.NoError(t, err)
	require.True(t, status.Moving)
	require.InDelta(t, 0.1, status.Position.PanTilt.X, 0.0001)
	require.InDelta(t, -0.2, status.Position.PanTilt.Y, 0.0001)
	require.InDelta(t, 0.5, status.Position.Zoom.X, 0.0001)

	require.NoError(t, client.PTZService().ContinuousMove(context.Background(), "main", PTZVelocity{
		PanTilt: Vector2D{X: 0.3, Y: 0}, Zoom: Vector1D{X: 0},
	}))
	require.NoError(t, client.PTZService().Stop(context.Background(), "main", true, true))

	none := NewPTZService(&Client{endpoints: &ServiceEndpoints{}})
	_, err = none.GetStatus(context.Background(), "main")
	require.ErrorContains(t, err, "PTZ service not available")
	require.ErrorContains(t, none.ContinuousMove(context.Background(), "main", PTZVelocity{}), "PTZ service not available")
	require.ErrorContains(t, none.Stop(context.Background(), "main", true, true), "PTZ service not available")
}

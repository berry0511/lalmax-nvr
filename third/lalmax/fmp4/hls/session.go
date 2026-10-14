package hls

import (
	"sync/atomic"
	"time"

	config "github.com/q191201771/lalmax/config"

	"github.com/bluenviron/gohlslib/v2"
	"github.com/bluenviron/gohlslib/v2/pkg/codecs"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/avc"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/hevc"
	"github.com/q191201771/naza/pkg/nazalog"
	uuid "github.com/satori/go.uuid"
)

type HlsSession struct {
	muxer               *gohlslib.Muxer
	videoTrack          *gohlslib.Track
	audioTrack          *gohlslib.Track
	done                atomic.Bool
	data                []Frame
	audioCodecId        int
	videoCodecId        int
	maxMsgSize          int
	appName             string
	streamName          string
	sps                 []byte
	pps                 []byte
	vps                 []byte
	asc                 []byte
	startAudioPts       time.Duration
	startVideoPts       time.Duration
	audioStartPTSFilled bool
	videoStartPTSFilled bool
	SessionId           string
	lastAccessUnix      atomic.Int64
	createdUnix         atomic.Int64
}

func NewHlsSession(streamName string, conf config.Fmp4HlsConfig) *HlsSession {
	return NewHlsSessionWithAppName("", streamName, conf)
}

func NewHlsSessionWithAppName(appName, streamName string, conf config.Fmp4HlsConfig) *HlsSession {
	variant := gohlslib.MuxerVariantFMP4
	if conf.LowLatency {
		variant = gohlslib.MuxerVariantLowLatency
	}

	u, _ := uuid.NewV4()

	session := &HlsSession{
		muxer: &gohlslib.Muxer{
			Variant: variant,
		},
		audioCodecId: -1,
		videoCodecId: -1,
		maxMsgSize:   128,
		data:         make([]Frame, 10)[0:0],
		appName:      appName,
		streamName:   streamName,
		SessionId:    u.String(),
	}

	uid, _ := uuid.NewV4()
	session.SessionId = uid.String()
	now := time.Now().Unix()
	session.createdUnix.Store(now)
	session.lastAccessUnix.Store(now)

	if !conf.LowLatency && conf.SegmentCount > 0 {
		// fmp4模式下可以设置分片个数
		session.muxer.SegmentCount = conf.SegmentCount
	}

	if conf.LowLatency && conf.PartDuration > 0 {
		// llhls设置part duration
		session.muxer.PartMinDuration = time.Millisecond * time.Duration(conf.PartDuration)
	}

	if conf.SegmentDuration > 0 {
		session.muxer.SegmentMinDuration = time.Second * time.Duration(conf.SegmentDuration)
	}

	return session
}

func (session *HlsSession) OnMsg(msg base.RtmpMsg) {
	if session.done.Load() {
		if msg.Header.MsgTypeId == base.RtmpTypeIdVideo {
			if msg.IsVideoKeySeqHeader() {
				session.videoCodecId = int(msg.VideoCodecId())
				if msg.IsAvcKeySeqHeader() {
					var err error
					session.sps, session.pps, err = avc.ParseSpsPpsFromSeqHeader(msg.Payload)
					if err != nil {
						nazalog.Error("ParseSpsPpsFromSeqHeader err:", err)
					}
				} else {
					session.vps, session.sps, session.pps, _ = hevc.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(msg.Payload)
				}
			} else {
				nals, err := avc.SplitNaluAvcc(msg.Payload[5:])
				if err != nil {
					nazalog.Error(err)
					return
				}

				var nalus [][]byte
				if msg.IsAvcKeyNalu() || msg.IsHevcKeyNalu() {
					if msg.IsAvcKeyNalu() {
						nalus = append(nalus, session.sps)
						nalus = append(nalus, session.pps)
					} else {
						nalus = append(nalus, session.vps)
						nalus = append(nalus, session.sps)
						nalus = append(nalus, session.pps)
					}
				}

				nalus = append(nalus, nals...)
				pts := time.Millisecond*time.Duration(msg.Pts()) - session.startVideoPts
				err = session.writeVideo(time.Now(), pts, nalus)
				if err != nil {
					nazalog.Error("hls-fmp4 WriteH26x failed, err:", err)
				}
			}

		} else {
			if session.audioCodecId == int(base.RtmpSoundFormatAac) {
				if !session.audioStartPTSFilled {
					session.startAudioPts = time.Millisecond * time.Duration(msg.Dts())
					session.audioStartPTSFilled = true
				}
				pts := time.Millisecond*time.Duration(msg.Dts()) - session.startAudioPts
				err := session.muxer.WriteMPEG4Audio(session.audioTrack, time.Now(), durationToClockTicks(pts, session.audioTrack.ClockRate), [][]byte{msg.Payload[2:]})
				if err != nil {
					nazalog.Error("hls-fmp4 WriteMPEG4Audio failed, err:", err)
				}
			} else if session.audioCodecId == int(base.RtmpSoundFormatOpus) {
				if !session.audioStartPTSFilled {
					session.startAudioPts = time.Millisecond * time.Duration(msg.Dts())
					session.audioStartPTSFilled = true
				}
				pts := time.Millisecond*time.Duration(msg.Dts()) - session.startAudioPts
				err := session.muxer.WriteOpus(session.audioTrack, time.Now(), durationToClockTicks(pts, session.audioTrack.ClockRate), [][]byte{msg.Payload[1:]})
				if err != nil {
					nazalog.Error("hls-fmp4 WriteOpus failed, err:", err)
				}
			}
		}

		return
	}

	switch msg.Header.MsgTypeId {
	case base.RtmpTypeIdAudio:
		session.audioCodecId = int(msg.AudioCodecId())
		if session.audioCodecId == int(base.RtmpSoundFormatAac) {
			if msg.IsAacSeqHeader() {
				session.asc = msg.Payload[2:]
			} else {
				if !session.audioStartPTSFilled {
					session.startAudioPts = time.Millisecond * time.Duration(msg.Dts())
					session.audioStartPTSFilled = true
				}

				pts := time.Millisecond*time.Duration(msg.Dts()) - session.startAudioPts

				frame := Frame{
					ntp:       time.Now(),
					pts:       pts,
					au:        [][]byte{msg.Payload[2:]},
					codecType: msg.AudioCodecId(),
				}
				session.data = append(session.data, frame)
			}
		} else if session.audioCodecId == int(base.RtmpSoundFormatOpus) {
			if !session.audioStartPTSFilled {
				session.startAudioPts = time.Millisecond * time.Duration(msg.Dts())
				session.audioStartPTSFilled = true
			}

			pts := time.Millisecond*time.Duration(msg.Dts()) - session.startAudioPts
			frame := Frame{
				ntp:       time.Now(),
				pts:       pts,
				au:        [][]byte{msg.Payload[1:]},
				codecType: msg.AudioCodecId(),
			}
			session.data = append(session.data, frame)
		} else {
			return
		}
	case base.RtmpTypeIdVideo:
		if msg.IsVideoKeySeqHeader() {
			session.videoCodecId = int(msg.VideoCodecId())
			if msg.IsAvcKeySeqHeader() {
				var err error
				session.sps, session.pps, err = avc.ParseSpsPpsFromSeqHeader(msg.Payload)
				if err != nil {
					nazalog.Error("ParseSpsPpsFromSeqHeader err:", err)
				}
			} else {
				session.vps, session.sps, session.pps, _ = hevc.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(msg.Payload)
			}
		} else {
			nals, err := avc.SplitNaluAvcc(msg.Payload[5:])
			if err != nil {
				return
			}

			if !session.videoStartPTSFilled {
				session.startVideoPts = time.Millisecond * time.Duration(msg.Pts())
				session.videoStartPTSFilled = true
			}

			var nalus [][]byte
			if msg.IsAvcKeyNalu() || msg.IsHevcKeyNalu() {
				if msg.IsAvcKeyNalu() {
					nalus = append(nalus, session.sps)
					nalus = append(nalus, session.pps)
				} else {
					nalus = append(nalus, session.vps)
					nalus = append(nalus, session.sps)
					nalus = append(nalus, session.pps)
				}
			}

			nalus = append(nalus, nals...)

			pts := time.Millisecond*time.Duration(msg.Pts()) - session.startVideoPts

			frame := Frame{
				ntp:       time.Now(),
				pts:       pts,
				au:        nalus,
				codecType: msg.VideoCodecId(),
			}

			session.data = append(session.data, frame)
		}
	}

	if session.readyToStart() {
		session.drain()
		return
	}

	if len(session.data) >= session.maxMsgSize {
		session.drain()
		return
	}
}

func (session *HlsSession) readyToStart() bool {
	if session.videoCodecId == int(base.RtmpCodecIdAvc) {
		return len(session.sps) > 0 && len(session.pps) > 0 && session.videoStartPTSFilled
	}
	if session.videoCodecId == int(base.RtmpCodecIdHevc) {
		return len(session.vps) > 0 && len(session.sps) > 0 && len(session.pps) > 0 && session.videoStartPTSFilled
	}
	return session.videoCodecId == -1 && session.audioCodecId != -1 && session.audioStartPTSFilled
}

func (session *HlsSession) writeVideo(ntp time.Time, pts time.Duration, au [][]byte) error {
	if session.videoTrack == nil {
		return nil
	}
	clockPTS := durationToClockTicks(pts, session.videoTrack.ClockRate)
	if session.videoCodecId == int(base.RtmpCodecIdHevc) {
		return session.muxer.WriteH265(session.videoTrack, ntp, clockPTS, au)
	}
	return session.muxer.WriteH264(session.videoTrack, ntp, clockPTS, au)
}

func durationToClockTicks(pts time.Duration, clockRate int) int64 {
	return int64(pts/time.Millisecond) * int64(clockRate) / 1000
}

func (session *HlsSession) drain() {
	if session.videoCodecId != -1 {
		if session.videoCodecId == int(base.RtmpCodecIdAvc) {
			session.videoTrack = &gohlslib.Track{
				Codec: &codecs.H264{
					SPS: session.sps,
					PPS: session.pps,
				},
				ClockRate: 90000,
			}
		} else if session.videoCodecId == int(base.RtmpCodecIdHevc) {
			session.videoTrack = &gohlslib.Track{
				Codec: &codecs.H265{
					VPS: session.vps,
					SPS: session.sps,
					PPS: session.pps,
				},
				ClockRate: 90000,
			}
		}
	}

	if session.audioCodecId != -1 {
		if session.audioCodecId == int(base.RtmpSoundFormatAac) {
			var mpegConf mpeg4audio.Config
			err := mpegConf.Unmarshal(session.asc)
			if err != nil {
				nazalog.Error(err)
				return
			}

			session.audioTrack = &gohlslib.Track{
				Codec: &codecs.MPEG4Audio{
					Config: mpegConf,
				},
				ClockRate: mpegConf.SampleRate,
			}
		} else if session.audioCodecId == int(base.RtmpSoundFormatOpus) {
			session.audioTrack = &gohlslib.Track{
				Codec: &codecs.Opus{
					ChannelCount: 1,
				},
				ClockRate: 48000,
			}
		}
	}

	if session.videoTrack != nil {
		session.muxer.Tracks = append(session.muxer.Tracks, session.videoTrack)
	}
	if session.audioTrack != nil {
		session.muxer.Tracks = append(session.muxer.Tracks, session.audioTrack)
	}

	if err := session.muxer.Start(); err != nil {
		nazalog.Error(err)
		return
	}

	for _, data := range session.data {
		if (data.codecType == base.RtmpCodecIdAvc || data.codecType == base.RtmpCodecIdHevc) && session.videoCodecId != -1 {
			err := session.writeVideo(data.ntp, data.pts, data.au)
			if err != nil {
				nazalog.Error("hls-fmp4 WriteH26x failed, err:", err)
				continue
			}
		} else if session.audioCodecId != -1 {
			if data.codecType == base.RtmpSoundFormatAac {
				err := session.muxer.WriteMPEG4Audio(session.audioTrack, data.ntp, durationToClockTicks(data.pts, session.audioTrack.ClockRate), data.au)
				if err != nil {
					nazalog.Error("hls-fmp4 WriteMPEG4Audio failed, err:", err)
					continue
				}
			} else if data.codecType == base.RtmpSoundFormatOpus {
				err := session.muxer.WriteOpus(session.audioTrack, data.ntp, durationToClockTicks(data.pts, session.audioTrack.ClockRate), data.au)
				if err != nil {
					nazalog.Error("hls-fmp4 WriteMPEG4Audio failed, err:", err)
					continue
				}
			} else {
				// gohlslib不支持g711
				continue
			}
		}
	}

	session.done.Store(true)
}

func (session *HlsSession) OnStop() {
	if session.done.Load() {
		session.muxer.Close()
	}
}

func (session *HlsSession) touchAccess() {
	session.lastAccessUnix.Store(time.Now().Unix())
}

func (session *HlsSession) HandleRequest(ctx *gin.Context) {
	nazalog.Info("handle hls request, appName:", session.appName, " streamName:", session.streamName, " path:", ctx.Request.URL.Path)
	session.touchAccess()
	if !session.done.Load() {
		ctx.Header("Retry-After", "1")
		ctx.AbortWithStatus(503)
		return
	}
	session.muxer.Handle(ctx.Writer, ctx.Request)
}

type Frame struct {
	ntp       time.Time
	pts       time.Duration
	au        [][]byte
	codecType uint8
}

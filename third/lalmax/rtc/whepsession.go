package rtc

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"

	maxlogic "github.com/q191201771/lalmax/logic"

	"github.com/gofrs/uuid"
	"github.com/pion/rtp"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/q191201771/lal/pkg/avc"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/hevc"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
)

const whepMaxReplayPaceDelay = 5 * time.Millisecond

type whepSession struct {
	group          *maxlogic.Group
	pc             *peerConnection
	subscriberId   string
	lalServer      logic.ILalServer
	videoTrack     *webrtc.TrackLocalStaticRTP
	audioTrack     *webrtc.TrackLocalStaticRTP
	videoSender    *webrtc.RTPSender
	audioSender    *webrtc.RTPSender
	videopacker    *Packer
	audiopacker    *Packer
	msgChan        chan base.RtmpMsg
	streamID       string
	onRunEnd       func()
	closeOnce      sync.Once
	closing        atomic.Bool
	replayEnqueuing atomic.Bool // true only while Group synchronously replays cached GOP
	done           chan struct{} // broadcast close to a blocked replay producer
	closeChan      chan bool
	connectedChan  chan struct{}
	connectedOnce  sync.Once
	paceBaseDts    uint32
	paceBaseAt     time.Time
	paceStarted    bool
	replayingCache bool
	wroteBytes     atomic.Uint64
	videoPackets   atomic.Uint64
	videoWriteErrs atomic.Uint64
	pliRequests    atomic.Uint64
	nackRequests   atomic.Uint64
	remoteAddr     atomic.Value
}

// WebRTC viewers are unreliable network consumers; media queue memory must
// never grow without bound. Disconnect slow consumers instead of stalling
// publisher dispatch or silently accumulating a multi-minute backlog.
func boundedWHEPQueueSize(requested int) int {
	if requested <= 0 { return 256 }
	if requested > 2048 { return 2048 }
	return requested
}

func (conn *whepSession) requestClose() {
	if conn == nil { return }
	conn.closeOnce.Do(func() {
		conn.closing.Store(true)
		if conn.done != nil { close(conn.done) }
		select {
		case conn.closeChan <- true:
		default:
		}
	})
}

// enqueue prevents the synchronous cached GOP replay from looking like a slow
// live consumer. Replay applies bounded backpressure while Run drains in
// parallel; live traffic keeps the existing fail-fast overflow protection.
func (conn *whepSession) enqueue(msg base.RtmpMsg) {
	if conn == nil || conn.closing.Load() { return }
	if conn.replayEnqueuing.Load() {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case conn.msgChan <- msg:
		case <-conn.done:
		case <-timer.C:
			nazalog.Warnf("WHEP replay queue stalled; disconnecting. id=%s stream=%s", conn.subscriberId, conn.streamID)
			conn.requestClose()
		}
		return
	}
	select {
	case conn.msgChan <- msg:
	default:
		nazalog.Warnf("WHEP subscriber too slow; disconnecting. id=%s stream=%s", conn.subscriberId, conn.streamID)
		conn.requestClose()
	}
}

func NewWhepSession(appName, streamid string, writeChanSize int, pc *peerConnection, lalServer logic.ILalServer) *whepSession {
	ok, group := maxlogic.GetGroupManagerInstance().GetGroup(maxlogic.NewStreamKey(appName, streamid))
	if !ok {
		nazalog.Errorf("not found stream, appName:%s, streamid:%s", appName, streamid)
		return nil
	}

	u, _ := uuid.NewV4()
	return &whepSession{
		group:         group,
		pc:            pc,
		lalServer:     lalServer,
		subscriberId:  u.String(),
		msgChan:       make(chan base.RtmpMsg, boundedWHEPQueueSize(writeChanSize)),
		streamID:      streamid,
		closeChan:     make(chan bool, 2),
		done:          make(chan struct{}),
		connectedChan: make(chan struct{}, 1),
	}
}

func (conn *whepSession) GetAnswerSDP(offer string) (sdp string) {
	var err error

	videoHeader := conn.group.GetVideoSeqHeaderMsg()
	if videoHeader != nil {
		if videoHeader.IsAvcKeySeqHeader() {
			conn.videoTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "lalmax")
			if err != nil {
				nazalog.Error(err)
				return
			}

			conn.videoSender, err = conn.pc.AddTrack(conn.videoTrack)
			if err != nil {
				nazalog.Error(err)
				return
			}
			go conn.observeVideoRTCP(conn.videoSender)

			conn.videopacker = NewPacker(PacketH264, videoHeader.Payload)
		} else if videoHeader.IsHevcKeySeqHeader() {
			conn.videoTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH265}, "video", "lalmax")
			if err != nil {
				nazalog.Error(err)
				return
			}

			conn.videoSender, err = conn.pc.AddTrack(conn.videoTrack)
			if err != nil {
				nazalog.Error(err)
				return
			}
			go conn.observeVideoRTCP(conn.videoSender)

			conn.videopacker = NewPacker(PacketHEVC, videoHeader.Payload)
		}
	}

	audioHeader := conn.group.GetAudioSeqHeaderMsg()
	if audioHeader != nil {
		var mimeType string
		audioId := audioHeader.AudioCodecId()
		switch audioId {
		case base.RtmpSoundFormatG711A:
			conn.audioTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMA}, "audio", "lalmax")
			if err != nil {
				nazalog.Error(err)
				return
			}

			mimeType = PacketPCMA
		case base.RtmpSoundFormatG711U:
			conn.audioTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU}, "audio", "lalmax")
			if err != nil {
				nazalog.Error(err)
				return
			}

			mimeType = PacketPCMU
		case base.RtmpSoundFormatOpus:
			conn.audioTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "lalmax")
			if err != nil {
				nazalog.Error(err)
				return
			}

			mimeType = PacketOPUS
		default:
			nazalog.Error("unsupport audio codeid:", audioId)
		}

		if conn.audioTrack != nil {
			conn.audioSender, err = conn.pc.AddTrack(conn.audioTrack)
			if err != nil {
				nazalog.Error(err)
				return
			}

			conn.audiopacker = NewPacker(mimeType, nil)
		}
	}

	gatherComplete := webrtc.GatheringCompletePromise(conn.pc.PeerConnection)

	conn.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(offer),
	})

	answer, err := conn.pc.CreateAnswer(nil)
	if err != nil {
		nazalog.Error(err)
		return
	}

	err = conn.pc.SetLocalDescription(answer)
	if err != nil {
		nazalog.Error(err)
		return
	}

	<-gatherComplete

	sdp = conn.pc.LocalDescription().SDP
	return
}

func (conn *whepSession) Run() {
	defer conn.Close()
	if conn.onRunEnd != nil { defer conn.onRunEnd() }
	conn.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		nazalog.Info("peer connection state: ", state.String())

		switch state {
		case webrtc.PeerConnectionStateConnected:
			conn.signalConnected()
		case webrtc.PeerConnectionStateDisconnected:
			fallthrough
		case webrtc.PeerConnectionStateFailed:
			fallthrough
		case webrtc.PeerConnectionStateClosed:
			conn.requestClose()
		}
	})

	if conn.pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
		conn.signalConnected()
	}

	var replayDone <-chan struct{}
	for {
		select {
		case <-conn.connectedChan:
			// AddSubscriber replays cached GOP synchronously before returning.
			// Start it alongside the consumer loop so replay cannot fill the
			// bounded queue before any message is consumed.
			subscriptionDone := make(chan struct{})
			replayDone = subscriptionDone
			go func() {
				defer close(subscriptionDone)
				conn.group.AddSubscriber(maxlogic.SubscriberInfo{
					SubscriberID: conn.subscriberId,
					Protocol:     maxlogic.SubscriberProtocolWHEP,
				}, conn)
			}()
			goto connected
		case <-conn.closeChan:
			nazalog.Info("RemoveConsumer, connid:", conn.subscriberId)
			conn.group.RemoveSubscriber(conn.subscriberId)
			return
		}
	}

connected:
	statsTicker := time.NewTicker(15 * time.Second)
	defer statsTicker.Stop()
	for {
		select {
		case msg := <-conn.msgChan:
			if conn.consumeReplayControl(msg) { continue }
			// Codec changes must run on the same goroutine as sendVideo.
			if msg.IsVideoKeySeqHeader() {
				conn.updateVideoCodec(msg)
				continue
			}
			if conn.replayingCache {
				conn.paceReplayMsg(msg)
			}
			if msg.Header.MsgTypeId == base.RtmpTypeIdAudio && conn.audioTrack != nil {
				conn.sendAudio(msg)
			} else if msg.Header.MsgTypeId == base.RtmpTypeIdVideo && conn.videoTrack != nil {
				conn.sendVideo(msg)
			}
		case <-statsTicker.C:
			nazalog.Infof("WHEP video stats subscriber=%s rtp_packets=%d write_errors=%d pli=%d nack=%d", conn.subscriberId, conn.videoPackets.Load(), conn.videoWriteErrs.Load(), conn.pliRequests.Load(), conn.nackRequests.Load())
		case <-conn.closeChan:
			// The replay producer must finish before RemoveSubscriber, or a
			// subscription could be registered after removal during shutdown.
			if replayDone != nil { <-replayDone }
			nazalog.Info("RemoveConsumer, connid:", conn.subscriberId)
			conn.group.RemoveSubscriber(conn.subscriberId)
			return
		}
	}
}

func (conn *whepSession) signalConnected() {
	conn.connectedOnce.Do(func() {
		conn.refreshRemoteAddr()
		conn.connectedChan <- struct{}{}
	})
}

// consumeReplayControl runs only on the queue reader goroutine. Replay
// boundary markers preserve FIFO order across the synchronous GOP snapshot.
func (conn *whepSession) consumeReplayControl(msg base.RtmpMsg) bool {
	if msg.Header.MsgTypeId != 0 { return false }
	if len(msg.Payload) == 1 && msg.Payload[0] == 1 {
		conn.replayingCache = true
	} else {
		conn.replayingCache = false
		// End-of-replay is reached only after queued GOP messages were read.
		conn.replayEnqueuing.Store(false)
	}
	conn.paceBaseAt = time.Time{}
	conn.paceBaseDts = 0
	conn.paceStarted = false
	return true
}

func (conn *whepSession) OnReplayStart() {
	conn.replayEnqueuing.Store(true)
	// Start/end markers maintain replay ordering and pacing state on the
	// queue consumer goroutine instead of racing producer callbacks.
	conn.enqueue(base.RtmpMsg{Payload: []byte{1}})
}

func (conn *whepSession) OnReplayStop() {
	conn.enqueue(base.RtmpMsg{})
}

func (conn *whepSession) paceReplayMsg(msg base.RtmpMsg) {
	if msg.Header.MsgTypeId != base.RtmpTypeIdAudio && msg.Header.MsgTypeId != base.RtmpTypeIdVideo {
		return
	}
	if msg.IsVideoKeySeqHeader() || msg.IsAacSeqHeader() {
		return
	}

	if !conn.paceStarted {
		conn.paceBaseDts = msg.Dts()
		conn.paceBaseAt = time.Now()
		conn.paceStarted = true
		return
	}

	dtsDelta := int64(msg.Dts()) - int64(conn.paceBaseDts)
	if dtsDelta <= 0 {
		return
	}

	mediaElapsed := time.Duration(dtsDelta) * time.Millisecond
	delay := time.Until(conn.paceBaseAt.Add(mediaElapsed))
	if delay <= 0 {
		return
	}
	if delay > whepMaxReplayPaceDelay {
		delay = whepMaxReplayPaceDelay
	}

	time.Sleep(delay)
}

func (conn *whepSession) OnMsg(msg base.RtmpMsg) {
	switch msg.Header.MsgTypeId {
	case base.RtmpTypeIdMetadata:
		return
	case base.RtmpTypeIdAudio:
		if conn.audioTrack != nil {
			conn.enqueue(msg)
		}
	case base.RtmpTypeIdVideo:
		if msg.IsVideoKeySeqHeader() {
			conn.enqueue(msg)
			return
		}
		if conn.videoTrack != nil {
			conn.enqueue(msg)
		}
	}
}

func (conn *whepSession) OnStop() {
	conn.requestClose()
}

func (conn *whepSession) sendAudio(msg base.RtmpMsg) {
	if conn.audiopacker != nil {
		pkts, err := conn.audiopacker.Encode(msg)
		if err != nil {
			nazalog.Error(err)
			return
		}

		for _, pkt := range pkts {
			if err := conn.audioTrack.WriteRTP(pkt); err != nil {
				continue
			}
			conn.recordSentRTP(pkt)
		}
	}
}

// Read RTCP feedback to diagnose browser keyframe/retransmission requests.
func (conn *whepSession) observeVideoRTCP(sender *webrtc.RTPSender) {
	for {
		packets, _, err := sender.ReadRTCP()
		if err != nil { return }
		for _, packet := range packets {
			switch feedback := packet.(type) {
			case *rtcp.PictureLossIndication:
				conn.pliRequests.Add(1)
			case *rtcp.TransportLayerNack:
				conn.nackRequests.Add(uint64(len(feedback.Nacks)))
			}
		}
	}
}

func (conn *whepSession) sendVideo(msg base.RtmpMsg) {
	if conn.videopacker != nil {

		pkts, err := conn.videopacker.Encode(msg)
		if err != nil {
			nazalog.Error(err)
			return
		}

		for _, pkt := range pkts {
			if err := conn.videoTrack.WriteRTP(pkt); err != nil {
				conn.videoWriteErrs.Add(1)
				continue
			}
			conn.videoPackets.Add(1)
			conn.recordSentRTP(pkt)
		}
	}
}

func (conn *whepSession) updateVideoCodec(msg base.RtmpMsg) {
	if conn.videopacker == nil {
		return
	}

	if msg.IsAvcKeySeqHeader() {
		sps, pps, err := avc.ParseSpsPpsFromSeqHeader(msg.Payload)
		if err != nil {
			nazalog.Error("ParseSpsPpsFromSeqHeader err:", err)
			return
		}

		if h264Encoder, ok := conn.videopacker.enc.(*H264RtpEncoder); ok {
			if bytes.Equal(h264Encoder.sps, sps) && bytes.Equal(h264Encoder.pps, pps) {
				return
			}
		}

		conn.videopacker.UpdateVideoCodec(nil, sps, pps)
		return
	}

	if msg.IsHevcKeySeqHeader() {
		vps, sps, pps, err := hevc.ParseVpsSpsPpsFromSeqHeader(msg.Payload)
		if err != nil {
			nazalog.Error("ParseVpsSpsPpsFromSeqHeader err:", err)
			return
		}

		if hevcEncoder, ok := conn.videopacker.enc.(*HevcRtpEncoder); ok {
			if bytes.Equal(hevcEncoder.vps, vps) && bytes.Equal(hevcEncoder.sps, sps) && bytes.Equal(hevcEncoder.pps, pps) {
				return
			}
		}

		conn.videopacker.UpdateVideoCodec(vps, sps, pps)
	}
}

func (conn *whepSession) Close() {
	conn.requestClose()
	if conn.pc != nil {
		conn.pc.Close()
	}
}

func (conn *whepSession) GetSubscriberStat() maxlogic.SubscriberStat {
	conn.refreshRemoteAddr()
	return maxlogic.SubscriberStat{
		RemoteAddr:    conn.loadRemoteAddr(),
		WroteBytesSum: conn.wroteBytes.Load(),
	}
}

func (conn *whepSession) recordSentRTP(pkt *rtp.Packet) {
	if pkt == nil {
		return
	}
	conn.wroteBytes.Add(uint64(pkt.MarshalSize()))
}

func (conn *whepSession) refreshRemoteAddr() {
	if remoteAddr := conn.currentRemoteAddr(); remoteAddr != "" {
		conn.remoteAddr.Store(remoteAddr)
	}
}

func (conn *whepSession) currentRemoteAddr() string {
	if conn.videoSender != nil {
		if remoteAddr := remoteAddrFromDTLSTransport(conn.videoSender.Transport()); remoteAddr != "" {
			return remoteAddr
		}
	}
	if conn.audioSender != nil {
		if remoteAddr := remoteAddrFromDTLSTransport(conn.audioSender.Transport()); remoteAddr != "" {
			return remoteAddr
		}
	}
	if sctp := conn.pc.SCTP(); sctp != nil {
		return remoteAddrFromDTLSTransport(sctp.Transport())
	}
	return ""
}

func (conn *whepSession) loadRemoteAddr() string {
	v := conn.remoteAddr.Load()
	addr, _ := v.(string)
	return addr
}

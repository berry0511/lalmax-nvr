package api

import (
 "context"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "sync"
 "time"

 "github.com/lalmax-pro/lalmax-nvr/internal/config"
 "github.com/lalmax-pro/lalmax-nvr/internal/storage"
)

const localVideoMaxBytes int64 = 2 << 30

type localVideoPlayers struct {
 mu sync.Mutex
 cancels map[string]context.CancelFunc
}

func (h *Handler) localVideoDirectory() (string,error) {
 if h.config == nil || strings.TrimSpace(h.config.Storage.RootDir)=="" {
  return "",errors.New("storage root directory is not configured")
 }
 dir:=filepath.Join(h.config.Storage.RootDir,"local-videos")
 if err:=os.MkdirAll(dir,0700);err!=nil{return "",err}
 return dir,nil
}

func (h *Handler) localVideoRTMP(streamID string) (string,error) {
 if h.config==nil || h.config.RTMP.Enabled==nil || !*h.config.RTMP.Enabled {
  return "",errors.New("RTMP ingest must be enabled to loop local video")
 }
 port:=h.config.RTMP.Port
 if port==0 {port=config.DefaultLalRTMPPort}
 if port<1 || port>65535 {return "",errors.New("invalid RTMP port")}
 target:=fmt.Sprintf("rtmp://127.0.0.1:%d/live/%s",port,streamID)
 if h.config.RTMP.RTMPPublishAuthEnabled() {
  key:=h.config.RTMP.StreamKeys[streamID]
  if key=="" {return "",errors.New("RTMP publish token for this stream is not configured")}
  target+="?"+url.Values{"stream_key":{key}}.Encode()
 }
 return target,nil
}

func (h *Handler) startLocalVideo(row storage.CreatedStream) error {
 if _,err:=exec.LookPath("ffmpeg");err!=nil{return fmt.Errorf("ffmpeg not installed: %w",err)}
 target,err:=h.localVideoRTMP(row.StreamID)
 if err!=nil{return err}
 dir,err:=h.localVideoDirectory()
 if err!=nil{return err}
 abs,err:=filepath.Abs(row.SourceURL)
 if err!=nil{return err}
 relative,err:=filepath.Rel(dir,abs)
 if err!=nil || relative==".." || strings.HasPrefix(relative,".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
  return errors.New("local video must be under the managed video directory")
 }
 if fi,err:=os.Stat(abs);err!=nil || !fi.Mode().IsRegular() {return errors.New("local video file is unavailable")}
 h.stopLocalVideo(row.StreamID)
 ctx,cancel:=context.WithCancel(context.Background())
 h.localVideos.mu.Lock()
 if h.localVideos.cancels==nil {h.localVideos.cancels=make(map[string]context.CancelFunc)}
 h.localVideos.cancels[row.StreamID]=cancel
 h.localVideos.mu.Unlock()
 go func(){
  for ctx.Err()==nil {
   // Re-encode to H264/AAC for predictable FLV/RTMP playback.
   cmd:=exec.CommandContext(ctx,"ffmpeg","-hide_banner","-nostdin","-loglevel","error","-re","-stream_loop","-1",
    "-i",abs,"-c:v","libx264","-preset","veryfast","-tune","zerolatency","-pix_fmt","yuv420p",
    "-c:a","aac","-f","flv",target)
   cmd.Stdout=io.Discard
   cmd.Stderr=io.Discard
   if err:=cmd.Run();err!=nil && ctx.Err()==nil {
    logger.Warn("local video publisher exited, retrying", "stream_id",row.StreamID,"err",err)
   }
   select {case <-ctx.Done():return;case <-time.After(3*time.Second):}
  }
 }()
 return nil
}

func (h *Handler) stopLocalVideo(streamID string){
 h.localVideos.mu.Lock()
 cancel:=h.localVideos.cancels[streamID]
 delete(h.localVideos.cancels,streamID)
 h.localVideos.mu.Unlock()
 if cancel!=nil {cancel()}
}

// handleCreateLocalVideo accepts a browser-uploaded media file and creates a
// persistent stream slot. Uploaded bytes stay inside the configured storage root.
func (h *Handler) handleCreateLocalVideo(w http.ResponseWriter,r *http.Request){
 if h.mediaEngine==nil || h.db==nil {writeError(w,http.StatusServiceUnavailable,"stream management unavailable");return}
 if _,err:=exec.LookPath("ffmpeg");err!=nil {writeError(w,http.StatusServiceUnavailable,"ffmpeg is required");return}
 r.Body=http.MaxBytesReader(w,r.Body,localVideoMaxBytes+(16<<20))
 if err:=r.ParseMultipartForm(16<<20);err!=nil {writeError(w,http.StatusBadRequest,"invalid or oversized video upload");return}
 if r.MultipartForm!=nil {defer r.MultipartForm.RemoveAll()}
 id:=strings.TrimSpace(r.FormValue("stream_id"))
 if !pushStreamIDPattern.MatchString(id) {writeError(w,http.StatusBadRequest,"invalid stream_id");return}
 if _,err:=h.localVideoRTMP(id);err!=nil {writeError(w,http.StatusBadRequest,err.Error());return}
 existing,err:=h.db.GetCreatedStream(r.Context(),id)
 if err!=nil {writeError(w,http.StatusInternalServerError,"stream lookup failed");return}
 if existing!=nil {writeError(w,http.StatusConflict,"stream already exists");return}
 existingMedia,err:=h.mediaEngine.GetStream(r.Context(),id)
 if err!=nil {writeError(w,http.StatusInternalServerError,"stream lookup failed");return}
 if existingMedia!=nil {writeError(w,http.StatusConflict,"stream already active");return}
 cam,err:=h.db.GetCamera(r.Context(),id)
 if err!=nil {writeError(w,http.StatusInternalServerError,"camera lookup failed");return}
 if cam!=nil && !cam.Archived {writeError(w,http.StatusConflict,"stream id belongs to camera");return}
 binding,err:=h.db.GetStreamBinding(r.Context(),id)
 if err!=nil {writeError(w,http.StatusInternalServerError,"binding lookup failed");return}
 if binding!=nil {writeError(w,http.StatusConflict,"stream id is bound to camera");return}

 input,header,err:=r.FormFile("video")
 if err!=nil {writeError(w,http.StatusBadRequest,"video file is required");return}
 defer input.Close()
 ext:=strings.ToLower(filepath.Ext(header.Filename))
 switch ext {case ".mp4",".mkv",".mov",".avi",".webm",".ts":default:
  writeError(w,http.StatusBadRequest,"unsupported video file extension");return
 }
 dir,err:=h.localVideoDirectory()
 if err!=nil {writeError(w,http.StatusInternalServerError,"video storage unavailable");return}
 dest:=filepath.Join(dir,id+ext)
 temp,err:=os.CreateTemp(dir,id+"-upload-*")
 if err!=nil {writeError(w,http.StatusInternalServerError,"cannot create video file");return}
 tempPath:=temp.Name()
 defer os.Remove(tempPath)
 n,copyErr:=io.Copy(temp,io.LimitReader(input,localVideoMaxBytes+1))
 closeErr:=temp.Close()
 if copyErr!=nil || closeErr!=nil || n==0 || n>localVideoMaxBytes {
  writeError(w,http.StatusBadRequest,"invalid or oversized video file");return
 }
 if err=os.Rename(tempPath,dest);err!=nil {writeError(w,http.StatusInternalServerError,"failed to save video");return}
 row:=storage.CreatedStream{StreamID:id,Name:normalizeStreamDisplayName(r.FormValue("name"),id),
  AppName:"live",InputMode:storage.CreatedStreamLocalVideo,SourceURL:dest}
 if err:=h.db.InsertCreatedStream(r.Context(),row);err!=nil {
  _=os.Remove(dest)
  if storage.IsUniqueViolation(err){writeError(w,http.StatusConflict,"stream already exists")}else{writeError(w,http.StatusInternalServerError,"failed to create stream")}
  return
 }
 if err:=h.startLocalVideo(row);err!=nil {
  _,_=h.db.DeleteCreatedStream(r.Context(),id)
  _=os.Remove(dest)
  writeError(w,http.StatusInternalServerError,err.Error())
  return
 }
 writeJSON(w,http.StatusCreated,h.summaryFromCreated(r.Context(),row))
}

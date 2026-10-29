package api

import (
 "net/http"
 "strings"

 "github.com/go-chi/chi/v5"
)

// Demand leases are restricted to superadmins at the route layer until
// consumer-scoped source grants are implemented. They are not media tokens.
func (h *Handler) handleAcquireTranscodeDemandLease(w http.ResponseWriter,r *http.Request){
 if !h.requireTranscodeService(w){return}
 generation,ok:=transcodeGeneration(w,r)
 if !ok{return}
 lease,runtime,err:=h.transcodeService.AcquireDemand(r.Context(),chi.URLParam(r,"id"),generation)
 if err!=nil{transcodeAPIError(w,err);return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusCreated,map[string]any{"lease":lease,"runtime":runtime})
}
// Lease control tokens are passed in JSON bodies, never in URLs (which may
// be recorded by access logs, reverse proxies and observability tooling).
func transcodeDemandToken(w http.ResponseWriter,r *http.Request) (string,bool) {
 var req struct{Token string `json:"token"`}
 if err:=decodeTranscodeBody(w,r,&req);err!=nil || len(req.Token)<16 || len(req.Token)>128 || strings.ContainsAny(req.Token," \r\n\x00") {
  writeError(w,http.StatusBadRequest,"valid lease token required")
  return "",false
 }
 return req.Token,true
}
func (h *Handler) handleRenewTranscodeDemandLease(w http.ResponseWriter,r *http.Request){
 if !h.requireTranscodeService(w){return}
 token,ok:=transcodeDemandToken(w,r)
 if !ok{return}
 lease,err:=h.transcodeService.RenewDemand(token)
 if err!=nil{transcodeAPIError(w,err);return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,lease)
}
func (h *Handler) handleReleaseTranscodeDemandLease(w http.ResponseWriter,r *http.Request){
 if !h.requireTranscodeService(w){return}
 token,ok:=transcodeDemandToken(w,r)
 if !ok{return}
 if err:=h.transcodeService.ReleaseDemand(r.Context(),token);err!=nil{
  transcodeAPIError(w,err);return
 }
 w.WriteHeader(http.StatusNoContent)
}

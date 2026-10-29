package api

import (
 "net/http"

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
func (h *Handler) handleRenewTranscodeDemandLease(w http.ResponseWriter,r *http.Request){
 if !h.requireTranscodeService(w){return}
 lease,err:=h.transcodeService.RenewDemand(chi.URLParam(r,"token"))
 if err!=nil{transcodeAPIError(w,err);return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,lease)
}
func (h *Handler) handleReleaseTranscodeDemandLease(w http.ResponseWriter,r *http.Request){
 if !h.requireTranscodeService(w){return}
 if err:=h.transcodeService.ReleaseDemand(r.Context(),chi.URLParam(r,"token"));err!=nil{
  transcodeAPIError(w,err);return
 }
 w.WriteHeader(http.StatusNoContent)
}

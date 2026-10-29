package transcode

import (
 "context"
 "crypto/rand"
 "time"
)

// DemandLease is ephemeral consumer interest in an enabled on-demand job.
// Tokens are random, never persisted, and do not convey media playback rights.
type DemandLease struct {
 Token string `json:"token"`
 JobID string `json:"job_id"`
 Generation uint64 `json:"generation"`
 ExpiresAt time.Time `json:"expires_at"`
}

const demandLeaseTTL = 90*time.Second

func (s *Service) invalidateDemandLeasesLocked(jobID string) {
 for token,lease := range s.leases {
  if lease.JobID == jobID { delete(s.leases,token) }
 }
}

func (s *Service) demandOwnersLocked(jobID string) int {
 owners:=0
 for _,l:=range s.leases {if l.JobID==jobID{owners++}}
 return owners
}

// AcquireDemand creates a lease only for an enabled on-demand job. Multiple
// leases for one job share the same manager owner and physical transcode work.
func (s *Service) AcquireDemand(ctx context.Context, id string, generation uint64) (DemandLease, Runtime, error) {
 s.mu.Lock()
 defer s.mu.Unlock()
 j,err:=s.job(ctx,id,generation)
 if err!=nil{return DemandLease{},Runtime{},err}
 if j.RunPolicy!=OnDemand || !j.Enabled || j.StoppedByUser {
  return DemandLease{},Runtime{},&Error{Code:"demand_unavailable",Message:"on-demand job is not enabled for consumers"}
 }
 p,err:=s.profile(ctx,j)
 if err!=nil{return DemandLease{},Runtime{},err}
 if err=s.allow(p);err!=nil{return DemandLease{},Runtime{},err}
 runtime,err:=s.manager.Acquire(j,p)
 if err!=nil{return DemandLease{},Runtime{},err}
 lease:=DemandLease{Token:rand.Text(),JobID:id,Generation:generation,ExpiresAt:time.Now().Add(demandLeaseTTL)}
 if s.leases==nil{s.leases=make(map[string]DemandLease)}
 s.leases[lease.Token]=lease
 return lease,runtime,nil
}

// RenewDemand requires the existing unexpired token; it never changes the job
// generation or creates a new process.
func (s *Service) RenewDemand(token string) (DemandLease,error) {
 s.mu.Lock()
 defer s.mu.Unlock()
 lease,ok:=s.leases[token]
 if !ok || !time.Now().Before(lease.ExpiresAt) {
  return DemandLease{},&Error{Code:"lease_not_found",Message:"consumer lease has expired"}
 }
 lease.ExpiresAt=time.Now().Add(demandLeaseTTL)
 s.leases[token]=lease
 return lease,nil
}

// ReleaseDemand stops this job's manager ownership only after the final
// consumer releases it. Other jobs sharing the effective work are unaffected.
func (s *Service) ReleaseDemand(ctx context.Context, token string) error {
 s.mu.Lock()
 defer s.mu.Unlock()
 lease,ok:=s.leases[token]
 if !ok {return &Error{Code:"lease_not_found",Message:"consumer lease not found"}}
 delete(s.leases,token)
 if s.demandOwnersLocked(lease.JobID)!=0 || s.manager==nil {return nil}
 return s.manager.Release(ctx,lease.JobID,lease.Generation)
}

func (s *Service) expireDemandLeases(ctx context.Context) {
 s.mu.Lock()
 defer s.mu.Unlock()
 now:=time.Now()
 expired:=map[string]uint64{}
 for token,l:=range s.leases{
  if !now.Before(l.ExpiresAt){
   delete(s.leases,token)
   expired[l.JobID]=l.Generation
  }
 }
 for id,gen:=range expired{
  if s.demandOwnersLocked(id)==0 && s.manager!=nil{
   _=s.manager.Release(ctx,id,gen)
  }
 }
}

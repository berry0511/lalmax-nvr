package api

import (
    "context"
    "time"

    "github.com/lalmax-pro/lalmax-nvr/internal/transcode"
)

// RunHAPreload keeps one short-lived consumer lease per explicitly opted-in HA
// publication. It never creates playback rights, persists tokens or starts
// outputs for unpublished sources. Reconciliation stops leases after unpublish,
// disable, job revision change or shutdown.
func (h *Handler) RunHAPreload(ctx context.Context) {
    if h.transcodeService == nil || h.db == nil {
        return
    }
    repository, ok := h.db.(transcode.Repository)
    if !ok {
        return
    }
    held := map[string]transcode.DemandLease{}
    release := func(id string) {
        lease, ok := held[id]
        if !ok { return }
        delete(held, id)
        cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        _ = h.transcodeService.ReleaseDemand(cleanup, lease.Token)
        cancel()
    }
    defer func() {
        for id := range held { release(id) }
    }()

    refresh := func() {
        cfg, err := h.db.GetHomeAssistantSettings(ctx)
        if err != nil { return }
        desired := map[string]uint64{}
        if cfg.Enabled {
            jobs, err := repository.ListTranscodeJobs(ctx)
            if err != nil { return }
            for _, publication := range cfg.Streams {
                if !publication.PreloadTranscode || publication.TranscodeJobID == "" { continue }
                for _, job := range jobs {
                    if job.ID == publication.TranscodeJobID &&
                        job.Source.StreamID == publication.StreamID &&
                        job.Enabled && !job.StoppedByUser && job.RunPolicy == transcode.OnDemand {
                        desired[job.ID] = job.Generation
                        break
                    }
                }
            }
        }
        for id, lease := range held {
            generation, wanted := desired[id]
            if !wanted || generation != lease.Generation {
                release(id)
                continue
            }
            updated, err := h.transcodeService.RenewDemand(lease.Token)
            if err != nil {
                release(id)
                continue
            }
            held[id] = updated
        }
        for id, generation := range desired {
            if _, ok := held[id]; ok { continue }
            lease, _, err := h.transcodeService.AcquireDemand(ctx, id, generation)
            if err == nil { held[id] = lease }
        }
    }

    // On restart the persisted publication list is authoritative. The worker
    // does not preload other tasks by default, and retries only opted-in jobs.
    refresh()
    ticker := time.NewTicker(25 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C: refresh()
        }
    }
}

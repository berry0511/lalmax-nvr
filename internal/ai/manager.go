package ai

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lalmax-pro/lalmax-nvr/internal/config"

	"github.com/lalmax-pro/lalmax-nvr/internal/ai/multimodal"
	"github.com/lalmax-pro/lalmax-nvr/internal/ai/webhook"
)

var logger = slog.Default().With("component", "ai-manager")

// Manager coordinates browser/third-party detection submissions via webhook and optional multimodal analysis.
// Architecture:
//   - The browser can run YOLO on decoded WebCodecs frames, or an external
//     ai-detector can subscribe to RTSP streams and run inference itself.
//   - Both submit detection results through the webhook receiver.
//   - NVR stores results and broadcasts them via SSE
//   - Optionally triggers multimodal LLM analysis on detection events
type Manager struct {
	cfg               config.AIConfig
	receiver          *webhook.Receiver   // Webhook mode: receives external pushes
	multimodalManager *multimodal.Manager // Multimodal analysis (optional)
	store             Store

	mu           sync.RWMutex
	callbacks    map[string]webhook.CallbackFunc
	cbCounter    int
	analysisSem  chan struct{}
	lastAnalysis map[string]time.Time
	ctx          context.Context
	cancel       context.CancelFunc
}

// SetStore sets the persistence backend for AI history.
func (m *Manager) SetStore(store Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = store
}

// NewManager creates a new AI Manager based on configuration.
func NewManager(cfg config.AIConfig) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		cfg:          cfg,
		callbacks:    make(map[string]webhook.CallbackFunc),
		analysisSem:  make(chan struct{}, 2),
		lastAnalysis: make(map[string]time.Time),
		ctx:          ctx,
		cancel:       cancel,
	}

	if !cfg.Enabled {
		logger.Info("AI detection disabled")
		return m
	}

	// Initialize multimodal analyzer if configured
	m.initMultimodal()

	// Initialize webhook receiver
	m.receiver = webhook.NewReceiver()
	m.receiver.OnDetection(func(result webhook.DetectionResult) {
		// Persist and dispatch detection
		m.persistDetection(result)
		m.dispatchDetection(result)

		// Trigger multimodal analysis if configured and image is available
		if m.multimodalManager != nil && len(result.Detections) > 0 && result.ImageURL != "" {
			m.triggerMultimodalAnalysis(result)
		}
	})
	logger.Info("AI webhook backend initialized")

	return m
}

func (m *Manager) initMultimodal() {
	if m.cfg.Multimodal == nil || !m.cfg.Multimodal.Enabled {
		return
	}
	m.multimodalManager = multimodal.NewManager()
	for name, providerCfg := range m.cfg.Multimodal.Providers {
		analyzer, err := multimodal.CreateAnalyzer(multimodal.ProviderConfig{
			Provider:    providerCfg.Provider,
			APIKey:      providerCfg.APIKey,
			Endpoint:    providerCfg.Endpoint,
			Model:       providerCfg.Model,
			VisionModel: providerCfg.VisionModel,
			MaxTokens:   providerCfg.MaxTokens,
			Temperature: providerCfg.Temperature,
			Timeout:     providerCfg.Timeout,
		})
		if err != nil {
			logger.Error("Failed to create analyzer", "provider", name, "error", err)
			continue
		}
		m.multimodalManager.RegisterAnalyzer(name, analyzer)
	}
	if err := m.multimodalManager.SetProvider(m.cfg.Multimodal.Provider); err != nil {
		logger.Error("Failed to set active provider", "provider", m.cfg.Multimodal.Provider, "error", err)
		return
	}
	logger.Info("AI multimodal analyzer initialized", "provider", m.cfg.Multimodal.Provider)
}

// GetReceiver returns the webhook receiver.
func (m *Manager) GetReceiver() *webhook.Receiver {
	return m.receiver
}

// GetMultimodalManager returns the multimodal manager (nil if not configured).
func (m *Manager) GetMultimodalManager() *multimodal.Manager {
	return m.multimodalManager
}

// Status returns the current AI subsystem status.
func (m *Manager) Status() Status {
	if !m.cfg.Enabled {
		return Status{Backend: "disabled", Available: false, Reason: "AI 已禁用"}
	}

	backend := m.cfg.Backend
	if backend == "" || backend == "disabled" {
		backend = "webhook" // preserve behavior for older configs that only set enabled=true
	}
	status := Status{Backend: backend, Available: true}

	// Check if multimodal is also available
	if m.multimodalManager != nil && m.multimodalManager.IsAvailable() {
		status.Reason = backend + " + 多模态分析已启用"
	}

	return status
}

// OnDetection registers a callback for detection events.
func (m *Manager) OnDetection(cb webhook.CallbackFunc) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cbCounter++
	id := fmt.Sprintf("mgr-cb-%d", m.cbCounter)
	m.callbacks[id] = cb
	return id
}

// UnregisterCallback removes a detection callback.
func (m *Manager) UnregisterCallback(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.callbacks[id]
	delete(m.callbacks, id)
	return ok
}

// StopAll stops the manager.
func (m *Manager) StopAll() {
	if m.multimodalManager != nil {
		m.multimodalManager.Close()
	}

	m.cancel()
	logger.Info("AI manager stopped")
}

// triggerMultimodalAnalysis triggers LLM analysis on a webhook detection result.
func (m *Manager) triggerMultimodalAnalysis(result webhook.DetectionResult) {
	if m.multimodalManager == nil || len(result.Detections) == 0 {
		return
	}

	// Check cooldown
	if m.analysisCoolingDown(result.CameraID) {
		return
	}

	// Try to acquire semaphore (non-blocking)
	select {
	case m.analysisSem <- struct{}{}:
	default:
		logger.Debug("Skipping multimodal analysis, queue is full", "camera_id", result.CameraID)
		return
	}

	// Decode image from base64 URL
	imageData, err := decodeImageURL(result.ImageURL)
	if err != nil {
		logger.Debug("Failed to decode image URL", "camera_id", result.CameraID, "error", err)
		<-m.analysisSem
		return
	}

	// Run analysis in goroutine
	go func() {
		defer func() { <-m.analysisSem }()

		timeout := m.cfg.InferenceTimeoutMs
		if timeout <= 0 {
			timeout = 30000
		}
		ctx, cancel := context.WithTimeout(m.ctx, time.Duration(timeout)*time.Millisecond)
		defer cancel()

		prompt := m.buildAnalysisPrompt(result.Detections)
		analysisResult, err := m.multimodalManager.Analyze(ctx, imageData, prompt)
		if err != nil {
			logger.Debug("Multimodal analysis failed", "camera_id", result.CameraID, "error", err)
			return
		}

		analysisResult.CameraID = result.CameraID
		analysisResult.ImageURL = result.ImageURL
		analysisResult.TriggerDetections = toTriggerDetections(result.Detections)
		if analysisResult.Metadata == nil {
			analysisResult.Metadata = map[string]string{}
		}
		analysisResult.Metadata["trigger"] = "webhook"
		analysisResult.Metadata["pts"] = fmt.Sprintf("%d", result.PTS)
		analysisResult.Metadata["objects"] = detectionSummary(result.Detections)

		m.persistAnalysis(*analysisResult)
		m.multimodalManager.PublishResult(*analysisResult)

		logger.Info("Multimodal analysis completed",
			"camera_id", result.CameraID,
			"provider", analysisResult.Metadata["provider"],
			"labels", analysisResult.Labels,
		)
	}()
}

func (m *Manager) persistDetection(result webhook.DetectionResult) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()
	if store == nil {
		return
	}
	aiDetections := make([]Detection, 0, len(result.Detections))
	for _, d := range result.Detections {
		det := Detection{
			Label:      d.Label,
			Confidence: d.Confidence,
			Box:        d.Box,
			ZoneID:     d.ZoneID,
		}
		if d.TrackID != nil {
			det.TrackID = d.TrackID
		}
		aiDetections = append(aiDetections, det)
	}
	if err := store.InsertAIDetection(m.ctx, DetectionResult{
		CameraID:   result.CameraID,
		PTS:        result.PTS,
		Timestamp:  result.Timestamp,
		ImageURL:   result.ImageURL,
		Detections: aiDetections,
	}); err != nil {
		logger.Debug("persist AI detection failed", "camera_id", result.CameraID, "error", err)
	}
}

func (m *Manager) persistAnalysis(result multimodal.AnalysisResult) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()
	if store == nil {
		return
	}
	if err := store.InsertAIAnalysis(m.ctx, result); err != nil {
		logger.Debug("persist AI analysis failed", "camera_id", result.CameraID, "error", err)
	}
}

func (m *Manager) analysisCoolingDown(camID string) bool {
	interval := 10 * time.Second
	if m.cfg.Multimodal != nil && m.cfg.Multimodal.AnalysisInterval != "" {
		if parsed, err := time.ParseDuration(m.cfg.Multimodal.AnalysisInterval); err == nil && parsed > 0 {
			interval = parsed
		}
	}

	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if last, ok := m.lastAnalysis[camID]; ok && now.Sub(last) < interval {
		return true
	}
	m.lastAnalysis[camID] = now
	return false
}

func (m *Manager) buildAnalysisPrompt(detections []webhook.Detection) string {
	base := multimodal.DefaultPrompt
	if m.cfg.Multimodal != nil && m.cfg.Multimodal.AnalysisPrompt != "" {
		base = m.cfg.Multimodal.AnalysisPrompt
	}
	return fmt.Sprintf("%s\n\nYOLO 已检测到以下目标：%s。请结合目标检测结果分析这些目标之间的行为语义、风险等级和建议处置。", base, detectionSummary(detections))
}

func (m *Manager) dispatchDetection(result webhook.DetectionResult) {
	m.mu.RLock()
	callbacks := make([]webhook.CallbackFunc, 0, len(m.callbacks))
	for _, cb := range m.callbacks {
		callbacks = append(callbacks, cb)
	}
	m.mu.RUnlock()

	for _, cb := range callbacks {
		go cb(result)
	}
}

// Helper functions

func decodeImageURL(imageURL string) ([]byte, error) {
	if imageURL == "" {
		return nil, fmt.Errorf("empty image URL")
	}

	// Handle data URL format: data:image/jpeg;base64,<data>
	if strings.HasPrefix(imageURL, "data:") {
		parts := strings.SplitN(imageURL, ",", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid data URL format")
		}
		return base64.StdEncoding.DecodeString(parts[1])
	}

	return nil, fmt.Errorf("unsupported image URL format")
}

func detectionSummary(detections []webhook.Detection) string {
	if len(detections) == 0 {
		return "无"
	}
	counts := make(map[string]int)
	for _, d := range detections {
		counts[d.Label]++
	}
	summary := ""
	for label, count := range counts {
		if summary != "" {
			summary += "，"
		}
		summary += fmt.Sprintf("%s x%d", label, count)
	}
	return summary
}

func toTriggerDetections(detections []webhook.Detection) []multimodal.TriggerDetection {
	out := make([]multimodal.TriggerDetection, 0, len(detections))
	for _, d := range detections {
		out = append(out, multimodal.TriggerDetection{
			Label:      d.Label,
			Confidence: d.Confidence,
			Box:        d.Box,
		})
	}
	return out
}

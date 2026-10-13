# AI Setup Guide

This guide covers setting up AI-powered analysis for lalmax-nvr, including local YOLO detection and cloud-based multimodal analysis.

## Overview

lalmax-nvr supports two object-detection integrations, plus optional multimodal LLM analysis:

| Mode | Use Case | Latency | Cost |
|------|----------|---------|------|
| **Browser ONNX** | Run object detection on WebCodecs live frames in the current browser | Device-dependent | Free |
| **Third-party Webhook** | An external detector analyzes camera streams and posts detections back | Service-dependent | Service-dependent |
| **Multimodal LLM** | Scene understanding, anomaly analysis, natural language descriptions | 2-10s | API costs |

Browser ONNX and third-party Webhook are selectable object-detection approaches. Multimodal LLM analysis can be configured independently.

## Browser-side inference

Browser mode uses ONNX Runtime Web in a Web Worker to analyze frames received by the WebCodecs player. It does not require a separate YOLO service; the NVR serves the ONNX model from its data directory. The browser prefers WebGPU and falls back to WASM when WebGPU is unavailable.

1. Put a compatible YOLOv11-nano COCO ONNX model at `models/yolo11n.onnx` under the NVR data directory. The directory is configured by `storage.root_dir`; in Docker, use the data directory mounted into the NVR container.
2. Open **Settings → AI Detection**, enable AI, choose **Browser inference**, and save with the model URL `/models/yolo11n.onnx`.
3. Select **WebCodecs** for live playback. On first use, the browser downloads the model. It must be able to reach `/models/yolo11n.onnx` and the `/ort/` runtime assets on the NVR.
4. Review results on the AI Detection or Events page. Positive detections are sent to the NVR and archived as AI events. Repeated events for the same camera and class set are suppressed for 30 seconds.

Confidence threshold, frame skip, and inference timeout can be adjusted in Settings. Defaults are `0.3`, every `3` frames, and `30000` milliseconds. To use an external detector instead, select **Webhook** mode and post detections to `POST /api/ai/webhook`.

## Third-party detector: Docker setup

Docker provides a consistent environment across all platforms.

### Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) installed
- At least 4GB RAM available for Docker

### Quick Start

```bash
# Clone the repository
git clone https://github.com/lalmax-pro/lalmax-nvr.git
cd lalmax-nvr

# Start with AI services
docker compose -f docker-compose.ai.yml up -d
```

### Enable YOLO Service

Edit `docker-compose.ai.yml` and uncomment the `yolo-service` section:

```yaml
services:
  # ... existing services ...
  
  yolo-service:
    build:
      context: ./docker/yolo-service
      dockerfile: Dockerfile
    environment:
      - PORT=8080
      - MODEL=yolov8n.pt
      - CONFIDENCE=0.5
      - DEVICE=cpu
    ports:
      - "8080:8080"
    restart: unless-stopped
```

Then restart:

```bash
docker compose -f docker-compose.ai.yml up -d
```

### GPU Support (NVIDIA)

For faster inference with NVIDIA GPUs:

1. Install [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html)

2. Edit `docker-compose.ai.yml`:

```yaml
  yolo-service:
    # ... other config ...
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 1
              capabilities: [gpu]
```

3. Restart the service

## Option 2: Local Installation

### macOS

#### Install Python and Dependencies

```bash
# Install Homebrew if not installed
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Install Python
brew install python@3.11

# Install OpenCV dependencies
brew install opencv

# Create virtual environment
python3 -m venv ~/yolo-env
source ~/yolo-env/bin/activate

# Install Python packages
pip install ultralytics fastapi uvicorn pillow numpy opencv-python-headless
```

#### Run YOLO Service

```bash
cd docker/yolo-service
python main.py
```

#### Troubleshooting macOS

If you encounter OpenCV errors:

```bash
# Install OpenCV with GUI support
brew install opencv
export OPENCV_VIDEOIO_PRIORITY_AVFOUNDATION=1
```

For M1/M2 Macs, ensure you're using ARM-compatible Python:

```bash
# Check architecture
uname -m  # Should show "arm64"

# If using Rosetta, reinstall Python for ARM
arch -arm64 brew install python@3.11
```

### Linux (Ubuntu/Debian)

#### Install System Dependencies

```bash
# Update package list
sudo apt update

# Install Python and OpenCV dependencies
sudo apt install -y python3.11 python3.11-venv python3-pip
sudo apt install -y libgl1-mesa-glx libglib2.0-0 libsm6 libxext6 libxrender-dev

# Install NVIDIA drivers (if using GPU)
sudo apt install -y nvidia-driver-535
sudo apt install -y nvidia-container-toolkit
```

#### Create Virtual Environment

```bash
python3 -m venv ~/yolo-env
source ~/yolo-env/bin/activate

pip install ultralytics fastapi uvicorn pillow numpy opencv-python-headless
```

#### Run the Service

Start the YOLO service in the foreground from the service directory:

```bash
cd /path/to/lalmax-nvr/docker/yolo-service
~/yolo-env/bin/python main.py
```

### Windows

#### Install Python

1. Download Python 3.11 from [python.org](https://www.python.org/downloads/)
2. During installation, check "Add Python to PATH"

#### Install Dependencies

Open PowerShell as Administrator:

```powershell
# Create virtual environment
python -m venv C:\yolo-env
C:\yolo-env\Scripts\Activate.ps1

# Install packages
pip install ultralytics fastapi uvicorn pillow numpy opencv-python
```

#### Run YOLO Service

```powershell
cd docker\yolo-service
python main.py
```

#### Run as Windows Service

Use [NSSM](https://nssm.cc/) to run as a Windows service:

```powershell
# Download NSSM
# Run as Administrator
nssm install YOLOService C:\yolo-env\python.exe main.py
nssm set YOLOService AppDirectory C:\path\to\lalmax-nvr\docker\yolo-service
nssm start YOLOService
```

## Configure Multimodal Analysis

### DeepSeek Setup

1. Register at [DeepSeek Platform](https://platform.deepseek.com/)
2. Get your API key
3. In lalmax-nvr web UI:
   - Go to Settings → AI Detection
   - Select "大模型分析" as backend
   - Enable multimodal analysis
   - Select "DeepSeek" as provider
   - Enter your API key

### OpenAI Setup

1. Get API key from [OpenAI Platform](https://platform.openai.com/)
2. In lalmax-nvr web UI:
   - Go to Settings → AI Detection
   - Select "大模型分析" as backend
   - Select "OpenAI" as provider
   - Enter your API key

### Custom Provider

For compatible APIs (Qwen, Kimi, etc.):

1. Select "自定义" as provider
2. Enter the API endpoint URL
3. Enter model name and API key

## Environment Detection

lalmax-nvr automatically detects available AI services on startup. Check status in:

- **Web UI**: Settings → AI Detection → Service Status
- **API**: `GET /api/ai/status`

### Detection Logic

```
┌─────────────────────────────────────────────────────────────┐
│                    AI Service Detection                      │
├─────────────────────────────────────────────────────────────┤
│  1. Check YOLO service at configured endpoint               │
│  2. Check multimodal API connectivity                       │
│  3. Report status and available features                    │
└─────────────────────────────────────────────────────────────┘
```

### Status Codes

| Status | Meaning |
|--------|---------|
| `available: true` | Service is ready |
| `available: false` | Service not configured or unreachable |
| `backend: "disabled"` | AI not enabled in config |

## Performance Tuning

### YOLO Models

| Model | Speed | Accuracy | Use Case |
|-------|-------|----------|----------|
| yolov8n.pt | Fastest | Lower | Raspberry Pi, real-time |
| yolov8s.pt | Fast | Good | General use |
| yolov8m.pt | Medium | Better | Balanced |
| yolov8l.pt | Slow | High | Accuracy focused |
| yolov8x.pt | Slowest | Highest | Best accuracy |

### Frame Skip Rate

Adjust in Settings → AI Detection:
- **1**: Every frame (high CPU usage)
- **3**: Every 3rd frame (balanced)
- **10**: Every 10th frame (low CPU usage)

### Confidence Threshold

- **0.3**: More detections, more false positives
- **0.5**: Balanced (recommended)
- **0.7**: Fewer detections, fewer false positives

## Troubleshooting

### YOLO Service Not Starting

```bash
# Check logs
docker logs yolo-service

# Common issues:
# 1. Port already in use
lsof -i :8080

# 2. Insufficient memory
docker stats

# 3. Model download failed
docker exec yolo-service ls -la /root/.cache/ultralytics/
```

### Multimodal API Errors

```bash
# Test DeepSeek API
curl -X POST https://api.deepseek.com/v1/chat/completions \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"hello"}]}'

# Check NVR logs
docker logs lalmax-nvr | grep -i "ai\|multimodal"
```

### Performance Issues

```bash
# Monitor CPU usage
docker stats

# Check inference time
curl http://localhost:8080/api/detect -X POST -d '{"frame":"base64..."}'

# Reduce frame skip rate in settings
```

## API Reference

### YOLO Service Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/api/detect` | POST | Object detection |
| `/api/models` | GET | List available models |
| `/api/labels` | GET | List supported labels |

### Detection Request

```json
{
  "frame": "base64_encoded_image",
  "camera_id": "cam-1",
  "timestamp": 1234567890,
  "confidence": 0.5
}
```

### Detection Response

```json
{
  "detections": [
    {
      "label": "person",
      "confidence": 0.95,
      "box": [0.1, 0.2, 0.3, 0.4]
    }
  ],
  "processing_time_ms": 45.2
}
```

## Next Steps

- [Camera Setup Guide](camera-guide.md)
- [Configuration Reference](configuration.md)
- [API Reference](api-reference.md)

# AI 配置指南

本指南介绍如何为 lalmax-nvr 配置 AI 智能分析功能，包括本地 YOLO 检测和云端大模型分析。

## 功能概述

lalmax-nvr 支持两种目标检测接入方式，以及可选的多模态大模型分析：

| 模式 | 用途 | 延迟 | 成本 |
|------|------|------|------|
| **浏览器 ONNX** | 使用当前浏览器对 WebCodecs 直播画面做目标检测 | 取决于设备 | 免费 |
| **第三方 Webhook** | 由外部检测器处理摄像头流并回调检测结果 | 取决于服务 | 取决于服务 |
| **大模型分析** | 场景理解、异常分析、自然语言描述 | 2-10s | API 费用 |

浏览器 ONNX 和第三方 Webhook 是可选择的目标检测方式；多模态大模型可独立配置用于场景分析。

## 浏览器端推理

浏览器端模式使用 ONNX Runtime Web，在 Web Worker 中对 WebCodecs 播放器收到的画面推理。它不需要运行独立 YOLO 服务；ONNX 模型由 NVR 从数据目录提供。浏览器优先尝试 WebGPU，不可用时回退到 WASM。

1. 将兼容的 YOLOv11-nano COCO ONNX 模型放到 NVR 数据目录的 `models/yolo11n.onnx`。数据目录由 `storage.root_dir` 配置；Docker 部署时是挂载给 NVR 的数据目录。
2. 打开 **设置 → AI 检测**，启用 AI，选择 **浏览器端推理**，确认模型地址为 `/models/yolo11n.onnx` 并保存。
3. 在实时预览中选择 **WebCodecs** 播放协议。首次推理会下载模型；浏览器需要能访问 NVR 的 `/models/yolo11n.onnx` 和 `/ort/` 运行时资源。
4. 在 AI 检测页或事件中心查看结果。正向检测会发给 NVR 并归档为 AI 事件；同一摄像头、相同类别组合在 30 秒内会合并抑制重复事件。

可在设置中调整置信度、抽帧间隔和推理超时。当前默认值分别为 `0.3`、每 `3` 帧推理一次和 `30000` 毫秒。第三方检测服务可改选 **Webhook** 模式，继续通过 `POST /api/ai/webhook` 上报检测结果。

## 第三方检测器：Docker 部署

Docker 提供跨平台一致的环境，是最简单的部署方式。

### 前置要求

- 已安装 [Docker Desktop](https://www.docker.com/products/docker-desktop/)
- 至少 4GB 可用内存

### 快速开始

```bash
# 克隆仓库
git clone https://github.com/lalmax-pro/lalmax-nvr.git
cd lalmax-nvr

# 启动 AI 服务
docker compose -f docker-compose.ai.yml up -d
```

### 启用 YOLO 服务

编辑 `docker-compose.ai.yml`，取消注释 `yolo-service` 部分：

```yaml
services:
  # ... 现有服务 ...
  
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

然后重启服务：

```bash
docker compose -f docker-compose.ai.yml up -d
```

### GPU 加速（NVIDIA）

使用 NVIDIA GPU 获得更快的推理速度：

1. 安装 [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html)

2. 编辑 `docker-compose.ai.yml`：

```yaml
  yolo-service:
    # ... 其他配置 ...
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 1
              capabilities: [gpu]
```

3. 重启服务

## 方案二：本地安装

### macOS 系统

#### 安装 Python 和依赖

```bash
# 安装 Homebrew（如果未安装）
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# 安装 Python
brew install python@3.11

# 安装 OpenCV 依赖
brew install opencv

# 创建虚拟环境
python3 -m venv ~/yolo-env
source ~/yolo-env/bin/activate

# 安装 Python 包
pip install ultralytics fastapi uvicorn pillow numpy opencv-python-headless
```

#### 运行 YOLO 服务

```bash
cd docker/yolo-service
python main.py
```

#### macOS 常见问题

如果遇到 OpenCV 错误：

```bash
# 安装带 GUI 支持的 OpenCV
brew install opencv
export OPENCV_VIDEOIO_PRIORITY_AVFOUNDATION=1
```

对于 M1/M2 Mac，确保使用 ARM 版本的 Python：

```bash
# 检查架构
uname -m  # 应显示 "arm64"

# 如果使用 Rosetta，重新安装 ARM 版 Python
arch -arm64 brew install python@3.11
```

### Linux 系统（Ubuntu/Debian）

#### 安装系统依赖

```bash
# 更新包列表
sudo apt update

# 安装 Python 和 OpenCV 依赖
sudo apt install -y python3.11 python3.11-venv python3-pip
sudo apt install -y libgl1-mesa-glx libglib2.0-0 libsm6 libxext6 libxrender-dev

# 安装 NVIDIA 驱动（如果使用 GPU）
sudo apt install -y nvidia-driver-535
sudo apt install -y nvidia-container-toolkit
```

#### 创建虚拟环境

```bash
python3 -m venv ~/yolo-env
source ~/yolo-env/bin/activate

pip install ultralytics fastapi uvicorn pillow numpy opencv-python-headless
```

#### 运行服务

进入服务目录并以前台方式启动 YOLO 服务：

```bash
cd /path/to/lalmax-nvr/docker/yolo-service
~/yolo-env/bin/python main.py
```

### Windows 系统

#### 安装 Python

1. 从 [python.org](https://www.python.org/downloads/) 下载 Python 3.11
2. 安装时勾选 "Add Python to PATH"

#### 安装依赖

以管理员身份打开 PowerShell：

```powershell
# 创建虚拟环境
python -m venv C:\yolo-env
C:\yolo-env\Scripts\Activate.ps1

# 安装包
pip install ultralytics fastapi uvicorn pillow numpy opencv-python
```

#### 运行 YOLO 服务

```powershell
cd docker\yolo-service
python main.py
```

#### 设置 Windows 服务

使用 [NSSM](https://nssm.cc/) 将服务注册为 Windows 服务：

```powershell
# 下载 NSSM
# 以管理员身份运行
nssm install YOLOService C:\yolo-env\python.exe main.py
nssm set YOLOService AppDirectory C:\path\to\lalmax-nvr\docker\yolo-service
nssm start YOLOService
```

## 配置大模型分析

### DeepSeek 配置

1. 在 [DeepSeek 开放平台](https://platform.deepseek.com/) 注册账号
2. 获取 API Key
3. 在 lalmax-nvr Web 界面：
   - 进入 设置 → AI 检测
   - 选择 "大模型分析" 作为后端
   - 启用多模态分析
   - 选择 "DeepSeek" 作为提供商
   - 输入 API Key

### OpenAI 配置

1. 从 [OpenAI Platform](https://platform.openai.com/) 获取 API Key
2. 在 lalmax-nvr Web 界面：
   - 进入 设置 → AI 检测
   - 选择 "大模型分析" 作为后端
   - 选择 "OpenAI" 作为提供商
   - 输入 API Key

### 自定义 Provider

对于兼容 OpenAI 接口的服务（通义千问、Kimi 等）：

1. 选择 "自定义" 作为提供商
2. 输入 API 端点 URL
3. 输入模型名称和 API Key

## 环境检测

lalmax-nvr 会在启动时自动检测可用的 AI 服务。查看状态：

- **Web 界面**：设置 → AI 检测 → 服务状态
- **API 接口**：`GET /api/ai/status`

### 检测逻辑

```
┌─────────────────────────────────────────────────────────────┐
│                    AI 服务检测流程                            │
├─────────────────────────────────────────────────────────────┤
│  1. 检查配置的 YOLO 服务端点                                  │
│  2. 检查大模型 API 连接性                                     │
│  3. 报告状态和可用功能                                        │
└─────────────────────────────────────────────────────────────┘
```

### 状态说明

| 状态 | 含义 |
|------|------|
| `available: true` | 服务就绪 |
| `available: false` | 服务未配置或不可达 |
| `backend: "disabled"` | AI 未启用 |

## 性能优化

### YOLO 模型选择

| 模型 | 速度 | 精度 | 适用场景 |
|------|------|------|----------|
| yolov8n.pt | 最快 | 较低 | 树莓派、实时场景 |
| yolov8s.pt | 快 | 良好 | 通用场景 |
| yolov8m.pt | 中等 | 较好 | 平衡选择 |
| yolov8l.pt | 慢 | 高 | 精度要求高 |
| yolov8x.pt | 最慢 | 最高 | 最高精度 |

### 帧率跳过设置

在 设置 → AI 检测 中调整：
- **1**：每帧都检测（CPU 占用高）
- **3**：每 3 帧检测一次（平衡）
- **10**：每 10 帧检测一次（CPU 占用低）

### 置信度阈值

- **0.3**：检测更多，误报更多
- **0.5**：平衡（推荐）
- **0.7**：检测更少，误报更少

## 故障排除

### YOLO 服务无法启动

```bash
# 查看日志
docker logs yolo-service

# 常见问题：
# 1. 端口被占用
lsof -i :8080

# 2. 内存不足
docker stats

# 3. 模型下载失败
docker exec yolo-service ls -la /root/.cache/ultralytics/
```

### 大模型 API 错误

```bash
# 测试 DeepSeek API
curl -X POST https://api.deepseek.com/v1/chat/completions \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"hello"}]}'

# 查看 NVR 日志
docker logs lalmax-nvr | grep -i "ai\|multimodal"
```

### 性能问题

```bash
# 监控 CPU 使用
docker stats

# 检查推理时间
curl http://localhost:8080/api/detect -X POST -d '{"frame":"base64..."}'

# 降低帧率跳过设置
```

## API 参考

### YOLO 服务端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/health` | GET | 健康检查 |
| `/api/detect` | POST | 目标检测 |
| `/api/models` | GET | 列出可用模型 |
| `/api/labels` | GET | 列出支持的标签 |

### 检测请求

```json
{
  "frame": "base64编码的图片",
  "camera_id": "cam-1",
  "timestamp": 1234567890,
  "confidence": 0.5
}
```

### 检测响应

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

## 相关文档

- [摄像头配置指南](camera-guide.md)
- [配置参考](configuration.md)
- [API 参考](api-reference.md)

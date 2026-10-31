# 通用转码部署与当前边界

关联 [Issue #66](https://github.com/lalmax-pro/lalmax-nvr/issues/66)、[架构设计](transcoding-design.md)、[验收记录](transcoding-testing.md)。

> 实验性实现，尚未发布。第六轮已在隔离的原生 Linux 环境完成 FFmpeg/GStreamer 实际媒体、Chromium WHEP 和回归验证；这不是生产验收，也不替代真实 Home Assistant 实例验收。下面描述当前分支的配置与操作，不代表已发布版本可用。

## 本地运行

将以下配置合并到 NVR 配置中，首次保留 `enabled: false`：

```yaml
transcoding:
  enabled: false
  max_concurrent: 2
  threads_per_job: 2
  worker_enabled: false  # P5：独立子进程，先通过能力检测及专项测试再开启
  ffmpeg_path: /opt/homebrew/bin/ffmpeg
  ffprobe_path: /opt/homebrew/bin/ffprobe
  gst_launch_path: /opt/homebrew/bin/gst-launch-1.0
  gst_inspect_path: /opt/homebrew/bin/gst-inspect-1.0
```

以上路径仅为 macOS Homebrew 示例；Linux/容器应使用各自安装路径，空字符串表示 PATH 查找。不要把宿主机 macOS 二进制挂载到 Linux 容器使用。

目前路径与转码运行配置在启动时固定，修改后需重启。缺失工具不会阻止 NVR 启动，检测页显示失败。

## Docker 可选后端

默认 [Dockerfile](../../Dockerfile) 已安装 FFmpeg。可选 GStreamer 构建参数：

```sh
docker build --build-arg WITH_GSTREAMER=true -t lalmax-nvr:transcode-gst .
```

默认参数为 false。镜像保留 FFmpeg/ffprobe，因为快照、媒体检查与 GStreamer 源尺寸探测仍使用它们。不要挂载 Docker socket。

插件依赖包括 gstreamer-tools、base/good/bad/ugly 和 libav 包；具体目标架构可用性必须通过构建与实际检测确认。包含 x264 的构建涉及对应许可证，请在分发镜像时核对许可证要求。linux/arm64 可选镜像构建和有限编码 smoke test 已通过；其他架构和完整容器 API 媒体链路仍未验收，详见[记录](transcoding-testing.md)。

## 首次使用步骤

1. 用管理员账户打开“设置 → 通用转码能力”，点击检测，不会修改源或启动录像。
2. 添加 H.264 720p 模板，然后对**已保存模板**再次检测；后端默认模板的结果不能替代不同配置模板的结果。
3. 输入已接入的源流 ID、实际应用名和模板，保存禁用手动任务草稿。
4. 检查原始流在线，媒体模式为 embedded，RTMP 监听已由管理员显式开启。
5. 配置 `transcoding.enabled: true` 并重启后，重新检测保存的模板，再启用、启动任务。当前内存检测缓存重启即失效。
6. 查询任务源的 variants，并通过管理员播放接口取得带临时令牌的 RTSP 地址；不要把此地址放入公开日志或 Issue。
7. HA 发布设置可加载该源的转码任务作为输出；身份仍为原源 ID，选择操作不会启动任务。
8. 结束测试先停止任务；原流与原录像不因任务删除而修改。

## 已知限制

- 全局开启目前通过配置文件完成，网页不会自动开启。
- GStreamer 固定模板的本机和可选 linux/arm64 镜像自检已通过；运行中的 H.264/HEVC 合成发布源已经通过 API 管理的 RTSP → GStreamer → RTMP → RTSP 解码；实体摄像机和生产稳定性仍未验收。macOS Homebrew 本机自检需为进程设置 `DYLD_FALLBACK_LIBRARY_PATH=/opt/homebrew/lib`，该环境变量并未写入 NVR 全局配置。
- FFmpeg copy 模式使用内置的合成 H.264/AAC 样本执行直拷、媒体检查与解码自检；真实源仍须符合 H.264/AAC 的 copy 限制，自检不等于实际源或媒体回灌已验证。GStreamer copy 模式自检仍未支持。
- 管理员 RTSP 派生地址、同源 FLV 接口以及 HA 目录地址均已实际取回解码；第六轮还验证了 NVR 侧 HA-WHEP 的 Chromium H.264 + PCMA/8000 音视频播放、ICE/DTLS、RTP、视频解码和 Web Audio 非静音采样。真实 Home Assistant 客户端/实例仍为 `BLOCKED_ENVIRONMENT`。StreamDetail 派生预览仍走 HTTP-FLV/mpegts.js，不支持 PCMA 音频，不能把该路径当作 WHEP 验收。
- 检测有效期 10 分钟；已运行任务不会因检测过期被停止，但失败后的重新启动需要新的有效检测。
- 常驻策略 API 可配置；当前网页创建的是手动任务。持久化的已启用常驻任务在重启或检测过期后，由有界后台协调重新自检再恢复；自检失败遵循退避并记录安全错误码，运行中合成源的常驻任务实际进程重启恢复和解码已通过，实体海康源的视频 copy + 音频转码已有有限实际解码证据；其他实体源场景及长时间运行仍待验证。
- 不支持自动硬件加速、跨后端回退、任意命令、任意外部输入 URL 或离线录像转码。
- 独立 [ARM64 scratch Dockerfile](../../Dockerfile.arm64) 不包含后端工具，不应当作转码镜像使用。

## 安全验收要求

上线前必须实际验证源拉取、派生发布、RTSP 播放的鉴权；拒绝无令牌和跨源播放；验证取消 HA 发布与全局停止的不同作用域。不得以参数构造、进程启动或前端构建成功替代实际媒体解码验收。


## P4/P5 实验功能（已完成范围验收，仍非生产承诺）

- **按需订阅**：`POST /api/transcoding/jobs/{id}/leases?generation=N&wait_ms=20000` 可等待实际媒体就绪。租约会按需续期，最后一个租约释放或过期后自动收回媒体进程。前端派生预览已接入申请、续期和释放。
- **Home Assistant 预加载**：发布列表中选择 `on_demand` 转码 Job 后可单独勾选 `preload_transcode`；默认关闭，关闭 HA 发布、任务被禁用或代次变化后撤销租约。
- **WebRTC 有声派生输出**：选择 G.711 A-law 8kHz mono / H.264 模板，媒体检测必须成功；只接受能够协商 PCMA 的 WHEP SDP，其他音频明确拒绝，不静默回退 AAC 或原流。派生会话签名令牌已改为认证加密。
- **硬件编码**：可显式选择 `h264_nvenc`；必须完成当前机器上实际 FFmpeg 编码、ffprobe 与解码自检后才能启用，失败绝不回退 `libx264`。Intel/AMD 和其他架构不声称支持。
- **多档共享解码**：本期只完成对照评估，保留独立编码任务与同配置去重；参见 [实验记录](transcoding-multitier-evaluation.md)。未启用未通过收益与故障隔离门槛的共享解码。
- **派生录像**：通过受控 `/api/transcoding/jobs/{id}/record/start|stop?generation=N` 显式启动/停止，默认录像策略不改变；录制物理流及其来源 Job、代次、Profile 版本和实际编码被持久化关联。录制停止时随派生物理输出释放。
- **派生转推**：Relay 创建接口接受可选 `transcode_job_id`；存逻辑源与 Job 而非物理流 ID，启动时从内部注册表解析受保护输出并获取定向 RTMP 播放凭据，不能自行启动转码。失败不自动回落原流。
- **独立 Worker**：启用 `worker_enabled: true` 后，NVR 启动一个临时独立的本机 Worker 来运行固定 FFmpeg/GStreamer 管线。一次性 HMAC 控制消息通过私有 stdin 传输；无网络控制端口、不经 shell、不继承 NVR 密码/数据库 DSN；父进程管道断开触发退出与进程回收，原有 Always/OnDemand 管理器负责恢复。现阶段为**同用户身份的进程级隔离**，不等同于 seccomp、容器或专用 OS 用户隔离；高安全等级环境必须补充独立运行账户、沙箱和设备访问控制。
- **历史录像离线转码**：不在实时 Issue #66 范围，见 [独立后续设计](transcoding-offline-followup.md)。

第六轮隔离证据（2026-10-31）：FFmpeg/GStreamer 原生实际媒体通过；GStreamer 30 轮为 21 次实际媒体通过、9 次负控/状态检查；Chromium 直接 NVR 侧 HA-WHEP H.264 + PCMA/8000 音视频、ICE/DTLS、RTP、视频解码和 Web Audio 非静音采样通过；Go Race、前端 404/404 及 Linux/Windows/macOS 跨平台 CI 通过。真实 Home Assistant 实例仍为 `BLOCKED_ENVIRONMENT`，Docker 全链路、生产源和长期稳定性不由这轮证据覆盖。未拿到证据的场景不应标记验收通过。

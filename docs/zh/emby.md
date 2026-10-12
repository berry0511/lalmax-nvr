# Emby 影音库

Emby 模块把 Emby 媒体库中的电影或剧集作为 NVR 的直播流发布。Emby 负责媒体库和转码，NVR 负责将兼容的 HLS 媒体接入 `lalmax`，再提供 NVR 支持的播放协议。

## 配置

从侧栏进入 **媒体接入 → Emby 影音库**（`#/media-sources?tab=emby`；旧链接 `#/emby` 仍可用），填写 Emby 服务地址、Emby 用户 ID 和访问令牌。连接成功后，可以搜索电影和剧集，并将媒体项添加为 NVR 频道。访问令牌保存在服务端；启用数据加密后会加密存储。媒体接入总览见[媒体接入指南](media-sources.md)。

可在 **设置 → 安全** 中首次配置加密密钥。Web 设置会将密钥单独保存在配置文件旁的 `.encryption-key` 文件中（权限 `0600`），并立即启用。也可以通过 `NVR_ENCRYPTION_KEY` 或 `NVR_ENCRYPTION_KEY_FILE` 环境变量管理密钥；环境配置优先。密钥首次保存后不可直接替换，请备份密钥文件，轮换密钥前需要迁移已有密文。

Emby 服务需要能被 NVR 主机访问。NVR 请求 Emby 的 `PlaybackInfo`，再请求 HLS 播放清单；服务端转码能力和媒体授权由 Emby 自身决定。

## 发布行为

- 默认发布一次。媒体播放结束后，HLS 拉流结束，NVR 流正常下线，发布开关自动关闭。
- 启用 **片尾自动循环** 后，NVR 会在当前播放结束后重新创建 Emby 播放会话，从头继续播放。
- 手动停止会取消 Emby 拉流；添加到 NVR 的媒体项会保留，可再次发布。
- 每个 Emby 媒体项有稳定的 NVR `stream_id`，可在流详情中查看 RTSP、HLS、HTTP-FLV、WebRTC 等可用播放地址。

Emby 发布使用内嵌媒体模式，且 Emby 输出需满足 NVR HLS 接入支持的 H.264/H.265 视频与可选 AAC 音频格式。建议让 Emby 负责转码以获得兼容输出。

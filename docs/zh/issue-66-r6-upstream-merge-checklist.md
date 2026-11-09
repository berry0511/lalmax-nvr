# Issue #66 / PR #91 — R6 上游集成验证清单

> 本清单记录集成候选，不代表验收通过、可部署或可合并。

## Git 状态

- 来源：`berry0511/lalmax-nvr:sync/upstream-main-pr91-20261103`
- 来源 HEAD：`c89b52c2f5cfb882ec25107a8f5116e02df5336f`
- 上游目标：`lalmax-pro/lalmax-nvr:main`
- 上游采集 HEAD：`b5d1cd4f87bf231826cfc00e5f69cbfb7985dd88`
- 双亲整合候选：`2f35e8bb009685400c209a824f0e6b58293787b1`
- 集成方式：独立候选分支、非强推、完整双亲提交；不修改任何 `main`。

三方文件比较：165 项仅上游变更（其中 3 项为已删除的 SFU 演示文件），14 项双方修改，已逐项解决。对共享文件，不通过覆盖整个文件替代合并。

## 冲突处理原则

- 发布/CI：上游 HACS、`v*` 与 `V*` 标签、发布去重、Go toolchain 缓存、跨平台构建；保留 fork 的真实 FFmpeg/GStreamer 转码测试、G711 SDP 回归及完整前端测试。
- 代码集成：上游 SFU 路由、Go 依赖和录像源时钟；保留 fork 的按需转码/租约、派生录像、HA 权限、播放密钥与流量历史。
- RTMP 监听器：采用上游并发保护实现；保留 fork 的 Race 回归测试。
- DLNA 和 VoIP：优先采用上游已修复的时序处理，同时保留对应功能回归。
- Go Race 和覆盖率：不得把超时、跳过或日志缺失判定为 PASS；本地覆盖率门槛至少 85%。

## 尚未闭合的验收

- API 全量 Race 必须在最终 SHA 上完整结束并取得原始日志（不能只提高超时）。
- `third/lal` 和 `third/lalmax` 嵌套模块的完整 Race、单测与必要媒体回归。
- 原生 Ubuntu Chromium/Pion H264、H265 WebRTC 与 HA 生命周期回归。
- 原生 PostgreSQL/MySQL 连接、迁移、Emby 数据恢复与重启回归。
- 标准 Build、Codecov（fork 上传不可用单独记录）、覆盖率阈值和跨平台构建。
- 实体 ONVIF/GB28181 摄像机与 PTZ：`BLOCKED_HARDWARE`，不得宣称 `FULL_ACCEPTED`。

上游 PR #91 的七项评审整改要保持有效；当前合并候选先在 Draft PR 执行 CI，满足全部软件验收后才能考虑合并原 PR #23 和用户 `main`。

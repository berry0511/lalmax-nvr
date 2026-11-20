# GB28181 生命周期一致性

## 状态与操作

- 设备在线来自 SIP 注册及心跳，不因关闭某个点播会话而改变。
- 通道 `is_playing` 来自 NVR 的 SIP 会话表，不代表有可解码帧。
- `media_active` 来自一次有超时的媒体列表查询：活动发布者还需有正 FPS 或 15 秒内的帧。查询失败或引擎未配置时不返回该字段，界面显示未知。
- 录像中、暂停和未录像独立取自 Camera/Recorder，不用于推断点播状态。设备页会重新读取服务端状态，不对点播结果做乐观更新。
- 重启推流按当前会话状态执行 GB28181 Stop → Play，Stop 失败时停止流程；同一通道请求进行中禁止重复操作。重启录像走 Camera 接口，不发送 SIP BYE/INVITE。

## Camera 运行时

Play 发 INVITE 前同步 Camera 持久化记录、流绑定和 CameraManager 配置。默认流 ID 为 `deviceID:channelID`；兼容客户端显式指定的已有 `stream_id` 契约。数据库提供持久化配置，复用启动时的配置解码（包括 extras），CameraManager 在按 ID 加锁后写入或更新内存；不会启动、替换或重复添加 Recorder。仅生成的默认名称可跟随通道目录名称。恢复归档复用现有取消归档语义，保留记录及录像文件。

已配置但无活动 Recorder 的 Camera 可以停止；仅未知 Camera 返回 CameraNotFound。实际 Recorder 或录像任务停止错误仍返回给调用者。

## SIP 停止与权限

设备发来的 BYE 必须匹配 Call-ID、对话参与方及正确方向的 tag（设备侧 From 对应 INVITE 的 To）。缺 tag 时不容许只按设备匹配。未匹配返回 481。接收器清理与会话对象绑定，重复/迟到的清理不会移除后续会话。NVR 主动停止仅在收到远端 2xx 或 481 后清理对应 RTP；清理失败保留会话供重试。

所有点播、回放控制、对讲、广播、下载和平台写入沿用 `RequireOperatePermission`。仓库目前只有 super_admin 和只读 user；默认单用户模式仍注入 super_admin。设备列表及录像查询等只读接口不改变角色门槛。

诊断从 NVR 服务日志过滤 `[GB28181][PLAY]`、`[GB28181][STOP]`、`[GB28181][BYE]` 和 `[GB28181][CAMERA_SYNC]`。不要共享认证头、密码或完整 SIP 报文。

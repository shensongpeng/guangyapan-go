# WebAPI 客户端

此包封装光鸭消费端 WebAPI，独立于模块根目录的官方 OpenAPI 客户端。协议依据 [AList 光鸭驱动](https://github.com/AlistGo/alist/tree/fb0731a6953012e7b72b89bf5473817caa4625f9/drivers/guangyapan)（固定参考提交 `fb0731a6953012e7b72b89bf5473817caa4625f9`）。本包使用标准库独立实现请求、类型、校验及测试，不依赖 AList 或其内部框架。

| 项目 | OpenAPI（根包） | WebAPI（本包） |
| --- | --- | --- |
| 导入路径 | `github.com/shensongpeng/guangyapan-go` | `github.com/shensongpeng/guangyapan-go/webapi` |
| 业务域名 | `openapi.guangyapan.com` | `api.guangyapan.com` |
| 账号域名 | `openapi-account.guangyapan.com` | `account.guangyapan.com` |
| 应用标识 | 平台登记的开放平台 client_id | Web 客户端标识，默认采用 AList 的公开值 |
| 授权 | Device Code / Web OAuth + PKCE | 消费端 Token / 短信验证登录 |
| 业务头 | `x-client-id`、Bearer | `Did`、`Dt: 4`、Bearer |
| 列表 | GET，查询参数 | POST，JSON 请求体 |
| 上传凭证 capacity | 30 | 2 |

**不能只替换域名，也不要混用两套 Token。** 构造本包客户端不会调用 OpenAPI，不会发送短信、刷新 Token 或进行网络请求。WebAPI 不是公开 OpenAPI 合约，后续可能随官方客户端变化。

## 快速使用

```go
import (
    "context"
    "os"
    "time"

    "github.com/shensongpeng/guangyapan-go/webapi"
)

client, err := webapi.NewClient(webapi.Config{
    AccessToken: os.Getenv("GUANGYAPAN_WEB_ACCESS_TOKEN"),
    DeviceID: os.Getenv("GUANGYAPAN_WEB_DEVICE_ID"),
    // ClientID 可省略，采用 AList 的 Web 客户端默认标识。
})
if err != nil { return err }
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()
me, err := client.GetUserInfo(ctx)
if err != nil { return err }
_ = me.Subject
list, err := client.GetFileList(ctx, webapi.FileListRequest{
    ParentID: "", Page: 0, PageSize: 20, OrderBy: 3, SortType: 1,
})
if err != nil { return err }
_ = list.Data.List
```

`DeviceID` 留空会生成随机设备 ID，可用 `client.DeviceID()` 获取并保存，下次登录或调用继续使用。已有账号登录时最好使用对应设备 ID。所有请求支持 context；默认 30 秒 HTTP 超时、16 MiB 响应上限和每端点 500ms 请求间隔。可通过 `Config.HTTPClient`、`MaxResponseBytes`、`DisableRateLimit` 调整。多个实例共享出口时仍需应用统一限流。

示例命令：`GUANGYAPAN_WEB_ACCESS_TOKEN=... go run ./examples/web-list`。请在安全的本地环境配置 Token，不要写入仓库。

## 账号方法

| 方法 | 账号域名下的路径 | 用途 |
| --- | --- | --- |
| `GetUserInfo` | `GET /v1/user/me` | 验证消费端 Token，返回用户 sub |
| `RefreshToken` | `POST /v1/auth/token` | 刷新消费端 Token |
| `InitCaptcha` | `POST /v1/shield/captcha/init` | 请求验证码元数据 |
| `SendSMS` | `POST /v1/auth/verification` | 发送短信并返回 verification_id |
| `VerifySMS` | `POST /v1/auth/verification/verify` | 提交短信码，获取 verification_token |
| `SignIn` | `POST /v1/auth/signin` | 手机号、短信码及 verification_token 换 Token |

手机号须提供 E.164 国家码，例如 `+86 13800000000`。完整短信流程由调用方显式编排：`InitCaptcha → SendSMS → VerifySMS → SignIn`。如果验证码需要交互，返回 `ErrCaptchaRequired` 或平台的 `AccountError`，调用方完成官方验证后再使用 captcha token；SDK 不自动解决验证码，也不重复发送短信。

授权和刷新方法只返回 `Token`，不会隐式改变 Client。成功后调用 `SetAccessToken(token.AccessToken)`，并保存最新 refresh token 和到期时间。刷新响应未返回 refresh token 时，保留本次输入的旧值。401/403 不自动重放请求，应用可刷新后显式重试，防止写操作被重复执行。

## 业务方法

以下全部使用业务域名的 **POST JSON**：

| 方法 | 路径 |
| --- | --- |
| `GetFileList` | `/userres/v1/file/get_file_list` |
| `GetResourceFileList` | `/nd.bizuserres.s/v1/file/get_file_list` |
| `GetDownloadURL` | `/nd.bizuserres.s/v1/get_res_download_url` |
| `CreateDir` | `/nd.bizuserres.s/v1/file/create_dir` |
| `Rename` | `/nd.bizuserres.s/v1/file/rename` |
| `DeleteFiles` | `/nd.bizuserres.s/v1/file/delete_file` |
| `MoveFiles` | `/nd.bizuserres.s/v1/file/move_file` |
| `CopyFiles` | `/nd.bizuserres.s/v1/file/copy_file` |
| `GetTaskStatus` | `/nd.bizuserres.s/v1/get_task_status` |
| `GetUploadToken` | `/nd.bizuserres.s/v1/get_res_center_token` |
| `GetUploadInfo` | `/nd.bizuserres.s/v1/file/get_info_by_task_id` |
| `ResolveOfflineResource` | `/cloudcollection/v1/resolve_res` |
| `CreateOfflineTask` | `/cloudcollection/v1/create_task` |
| `ListOfflineTasks` | `/cloudcollection/v1/list_task` |
| `DeleteOfflineTasks` | `/cloudcollection/v2/delete_task` |

- `GetFileList` 使用从 0 开始的页码；调用方按页读取。`FileTypes` 在 JSON 中发送数组，未指定时发送 `[]`，没有 OpenAPI 查询参数编码的歧义。
- `GetResourceFileList` 是 AList 查找目录路径时使用的另一条列表路由；本 SDK 直接接受目录 ID，不自动解析路径字符串。
- `DownloadURL.URL()` 优先返回 `signedURL`，否则返回 `downloadUrl`。SDK 不下载文件内容。
- 创建目录、移动和复制的目标 `parentID` 可传空串表示根目录。移动、复制或删除可能同步完成并返回空 taskId；只有非空 taskId 才需要 `WaitTask`。
- `WaitTask` 识别 2 为成功、-1 或 3 为失败，其他已知状态每秒查询一次；调用方应设置 context 截止时间。
- 上传凭证方法固定 `capacity=2`，支持扁平 STS 字段与 `creds` 内嵌字段，保留原始 provider 和 callbackVar。156 表示秒传完成；`WaitUpload` 获取最终 fileId。
- `WaitUpload` 兼容参考驱动观察到的处理中状态 145/146/147/155/163，最长等待 5 分钟，并可通过 context 提前取消。163 的终态含义不与 OpenAPI 共用。
- 本次封装包括 AList 使用的所有 HTTP API 路由，但**不包含 AList 的阿里云 OSS 文件字节传输实现**。普通上传还需要使用返回的 STS 凭据对接 OSS；仅获取凭证不表示上传成功。
- 离线任务的资源解析和任务创建分开，允许调用方检查内容并选择 `fileIndexes`。删除离线任务不暴露参考实现没有发送的 `deleteFiles` 参数。
- 参考驱动没有使用 WebAPI 文件详情、VOD、秒传检查和续传凭证路由，因此本包不猜测这些路径，更不会暗中调用 OpenAPI 作为回退。

## 错误与安全

业务错误返回 `*webapi.APIError`；账号错误返回 `*webapi.AccountError`，保留 `ErrorCode`、`ErrorName` 和 `Description`；HTTP 错误返回 `*webapi.HTTPError`。缺失必要字段或格式不合法返回 `ErrInvalidResponse`。错误字符串不打印账号响应描述，避免服务端回显凭据；应用若直接打印结构体仍可能暴露敏感信息。

非零业务码严格返回错误，只有上传凭证/上传结果接口接受 156；不会像部分驱动逻辑那样只检查 HTTP 状态。禁用自动重定向，防止 Bearer、设备头或认证请求体转发到其他目标。凭据只由显式构造的独立客户端持有。

## 验证范围

测试覆盖全部 6 个账号及 15 个业务路由、JSON 数组、Web/OpenAPI 请求头隔离、短信步骤、Token 刷新、上传凭据兼容、业务错误、异步状态、限频、并发 Token 更新和重定向阻止。所有自动化测试使用内存 Transport，不发送短信或操作云盘。

2026-09-28 的真实只读联调：消费端账号接口、两条列表接口和离线任务列表均已到达真实服务，但当前本地凭据返回 401；账号错误码为 16。由于认证未通过，下载地址验证跳过。没有执行短信发送、Token 刷新、文件写入或上传；不能据此声称真实业务成功路径已通过。

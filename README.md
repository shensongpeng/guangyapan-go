# 光鸭云盘 Go SDK：OpenAPI + WebAPI

本 SDK 包含两套独立协议：子包 [`openapi`](openapi) 实现官方 **OpenAPI**；子包 [`webapi`](webapi/README.md) 参考 AList 实现消费端 **WebAPI**，具有独立域名、认证和请求类型。WebAPI Token 不能用作开放平台 OAuth Token；两者不能只通过换域名互相替代。

以下章节介绍 OpenAPI；WebAPI 的初始化、短信登录、文件及离线任务用法见 [WebAPI 文档](webapi/README.md)。

依据 [光鸭盘开放平台文档 v1.3](https://app.guangyapan.com/pan/docs/open-api)（2026-09-20）实现，要求 Go 1.22+，仅使用标准库。

支持 Device Code、Web OAuth + PKCE、Token 刷新，以及文档中的全部 14 个业务接口。所有网络方法接收 `context.Context`；客户端支持并发调用。

## 安装

```sh
go get github.com/shensongpeng/guangyapan-go/openapi
go get github.com/shensongpeng/guangyapan-go/webapi
```

OpenAPI 导入路径为 `github.com/shensongpeng/guangyapan-go/openapi`，包名为 `openapi`；WebAPI 导入路径为 `github.com/shensongpeng/guangyapan-go/webapi`，包名为 `webapi`。

迁移已有代码时，将原根包导入路径追加 `/openapi`，并把 `guangyapan.NewClient` 等包限定符改为 `openapi.NewClient`。根目录不再提供 Go 包，模块路径保持不变。

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/shensongpeng/guangyapan-go/openapi"
)

func main() {
    client, err := openapi.NewClient(openapi.Config{
        ClientID:    os.Getenv("GUANGYAPAN_CLIENT_ID"),
        AccessToken: os.Getenv("GUANGYAPAN_ACCESS_TOKEN"),
    })
    if err != nil { log.Fatal(err) }

    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    result, err := client.GetFileList(ctx, openapi.FileListRequest{
        Page: 0, PageSize: 20,
        OrderBy: openapi.Ptr(openapi.OrderByUpdatedAt),
        SortType: openapi.Ptr(openapi.SortDescending),
    })
    if err != nil { log.Fatal(err) }
    for _, file := range result.Data.List {
        fmt.Println(file.FileID, file.FileName, file.FileSize)
    }
}
```

设置环境变量后，可在 SDK 目录运行 `go run ./examples/list`。该示例只查询文件列表。

## 授权

### Device Code

仅获取设备码需要 `sign_secret`，须在可信应用服务器执行；该服务器出口 IP 需要由平台加入白名单。SDK 不在 Client 中保存此密钥。

```go
device := openapi.DeviceContext{DeviceID: "device-id", ProjectID: "project-id"}
// 以下调用只在可信应用服务器执行，signSecret 来自安全配置。
code, err := client.RequestDeviceCode(ctx, device, signSecret, "")
if err != nil { return err }
// 将 code.VerificationURIComplete 展示给用户，或作为二维码内容。
// 移动端 Scheme 可由 openapi.DeviceAppURL(code.VerificationURIComplete) 生成。
```

接入方拿到设备码后，直接访问账号接口获取 Token：

```go
token, err := client.WaitDeviceToken(ctx, device, code)
if err != nil { return err }
client.SetAccessToken(token.AccessToken)
// 保存 token.RefreshToken 与 token.ExpiresAt 到应用自己的安全凭据存储。
```

`WaitDeviceToken` 每次请求前等待服务端返回的 `interval`，并在设备码到期、调用方取消、授权成功或其他错误时停止。`RequestDeviceCode` 自动记录 `code.ExpiresAt`。该字段不进入 JSON；跨进程传递设备码时，应通过应用自己的数据结构保留并恢复原始到期时刻，不能反序列化后重新起算有效期。

也可使用单次调用 `PollDeviceToken(ctx, device, deviceCode)` 自行管理轮询；使用 `errors.Is(err, openapi.ErrAuthorizationPending)` 判断纯文本 `authorization_pending`。SDK 同时兼容 OAuth JSON 形式的 pending。

### Web OAuth + PKCE

```go
pkce, err := openapi.GeneratePKCE()
if err != nil { return err }
state, err := openapi.GenerateState()
if err != nil { return err }
authorizeURL, err := client.AuthorizationURL(redirectURI, state, pkce.Challenge)
if err != nil { return err }
// 将 state、pkce.Verifier 与当前用户会话绑定，短期保存。
// 引导浏览器打开 authorizeURL。

// 回调时先校验会话中的 state，处理用户拒绝授权的错误，再换 Token。
code, err := openapi.ParseOAuthCallback(callbackURL, state)
if err != nil { return err }
token, err := client.ExchangeCode(ctx, code, pkce.Verifier, redirectURI)
if err != nil { return err }
client.SetAccessToken(token.AccessToken)
// 成功或授权被拒绝后，清除该次授权的 state 和 verifier。
```

`AuthorizationURL` 固定使用 `scope=user offline` 和 `code_challenge_method=S256`，并正确编码所有查询参数。回调地址必须与平台登记值相同。Web 流程不需要 `project_id`、设备头或签名密钥。

### Token 刷新

```go
next, err := client.RefreshToken(ctx, savedRefreshToken)
if err != nil { return err } // 失效时进入重新授权流程。
if next.RefreshToken == "" {
    next.RefreshToken = savedRefreshToken
}
client.SetAccessToken(next.AccessToken)
// 持久化 next.RefreshToken、next.ExpiresAt，并按返回有效期安排下次刷新。
```

Token 方法不会隐式修改 Client。调用方负责安全保存凭据、刷新调度以及同一用户的刷新并发控制。建议在 `ExpiresAt` 之前留出余量刷新。业务错误码 117 可触发刷新，成功后由调用方重试原请求。SDK 不会自动重放写请求，以免网络结果不明确时产生重复操作。

## 文件与上传任务

通常返回 `*Response[T]`，业务数据位于 `Data`，平台状态保留在 `Code`、`Message` 中；重命名无响应数据，仅返回 `error`。读取结果前先检查 `error`。

| 文档接口 | Go 方法 |
| --- | --- |
| `GET /openapi/v1/user/get_user_info` | `GetUserInfo` |
| `GET /openapi/v1/file/get_file_list` | `GetFileList` |
| `GET /openapi/v1/file/get_file_detail` | `GetFileDetail` |
| `GET /openapi/v1/file/get_res_download_url` | `GetResDownloadURL` |
| `GET /openapi/v1/file/get_vod_download_url` | `GetVODDownloadURL` |
| `POST /openapi/v1/file/create_dir` | `CreateDir` |
| `POST /openapi/v1/file/rename` | `Rename` |
| `POST /openapi/v1/file/move_file` | `MoveFile` |
| `POST /openapi/v1/file/get_task_status` | `GetTaskStatus` / `WaitTask` |
| `POST /openapi/v2/file/get_res_center_token` | `GetResCenterToken` |
| `POST /openapi/v1/file/check_can_flash_upload` | `CheckCanFlashUpload` |
| `POST /openapi/v1/file/get_res_center_resume_token` | `GetResCenterResumeToken` |
| `POST /openapi/v1/file/get_info_by_task_id` | `GetInfoByTaskID` / `WaitUpload` |
| `POST /openapi/v1/file/delete_upload_task` | `DeleteUploadTask` |

创建目录或移动到根目录时，`ParentID` 传空字符串。移动一次支持 1–500 个 ID；成功受理后，用 `WaitTask(ctx, result.Data.TaskID, time.Second)` 等待完成，失败终态返回 `ErrTaskFailed`。上传结果必须使用 `WaitUpload`，两类任务不能混用。

文件列表从第 0 页开始，`PageSize` 必填。数值过滤器使用指针，区分“未传”与“显式传 0”；可用 `Ptr` 辅助。`fileTypes` 数组的查询字符串格式在文档中未明确，本实现选择重复键 `fileTypes=1&fileTypes=2`，此项需要与平台实机联调确认。

### 上传流程

1. `GetResCenterToken` 自动发送固定 `capacity=30`。请求不暴露 `userId` 和新建任务的 `object` 字段。
2. 如果 `result.Code == CodeUploadCompleted`（156），已秒传成功，直接调用 `WaitUpload(ctx, result.Data.TaskID)` 获取最终文件信息。
3. 如果返回 0，可调用 `CheckCanFlashUpload`；返回 `CanFlashUpload=true` 时继续查询上传结果。
4. 无法秒传时，把申请到的临时凭证和文件交给**官方对象存储上传 SDK**。必须保留 `callback`、`callbackVar`，并在对象存储 Complete 时携带。
5. 续传使用 `GetResCenterResumeToken`，原样携带初次申请的文件大小、provider 和 objectPath。
6. 上传完成后调用 `WaitUpload`，其对 147 或尚无 `fileId` 的成功响应按 1、2、4、8、10 秒间隔退避，直到取得非空 `fileId`。

**本 SDK 实现文档中的 OpenAPI 控制接口，不实现文件字节传输、分片上传、对象存储签名或 Complete。** 所给文档未定义这些协议，需要另行对接官方上传 SDK；不能仅申请凭证就视为文件上传完成。`DeleteUploadTask` 只取消上传任务，不删除已落盘文件。

创建目录、重命名、移动和上传等能力须由平台开通；123 表示应用能力未开通，430 表示用户非会员。测试代码不会调用真实账号、修改云盘或消耗上传额度。

## 错误与请求配置

```go
if openapi.IsCode(err, openapi.CodeInvalidToken) {
    // 刷新 Token，然后显式重试。
}
var apiErr *openapi.APIError
if errors.As(err, &apiErr) {
    // apiErr.Code / apiErr.Message
}
var httpErr *openapi.HTTPError
if errors.As(err, &httpErr) {
    // httpErr.StatusCode / httpErr.RetryAfter
}
```

- HTTP 200 仍检查业务 `code`。156 仅在上传凭证、秒传和上传结果相关方法中作为成功处理；不会全局忽略该码。147 由 `WaitUpload` 处理为等待状态。
- HTTP 非 2xx 返回 `HTTPError`；格式错误、缺少必要响应字段或超限响应返回 `ProtocolError`；OAuth 拒绝返回 `OAuthError`。未知纯文本不会误判为 pending。
- 默认 HTTP 超时 30 秒，JSON 响应上限 16 MiB；可在 `Config` 中设置 `HTTPClient`、`MaxResponseBytes`。网络取消可用 `errors.Is(err, context.Canceled)` 判断。
- 客户端复制 `http.Client` 配置并禁止自动重定向，防止向重定向目标转发签名、Token 或敏感请求体；调用方自定义的 `CheckRedirect` 不会生效。
- 自定义 `APIURL`、`AccountURL` 用于测试环境或代理。提供 `TestAPIURL`、`TestAccountURL` 常量；测试环境的 OAuth 授权地址需由平台确认，再设置 `AuthorizeURL`。生产环境请使用 HTTPS；HTTP 仅适合本地模拟。
- 用 `WithTraceparent(ctx, value)` 附加调用方的链路追踪头。SDK 不输出请求/响应日志；Token、STS 凭证不要直接打印。
- SDK 不自动全局限流，也不自动重试。文档按出口 IP 限频：文件列表、异步状态、上传结果为 5 次/秒，其余业务接口为 2 次/秒。多个 Client 或进程共享出口时，需在应用层统一限流；不要靠单个客户端的轮询间隔代替全局控制。
- 给等待任务设置 `context.WithTimeout`，避免平台始终未进入终态时无限等待。

## 验证

```sh
go test -race -cover ./...
go vet ./...
go build ./...
```

测试使用内存 HTTP Transport 和 `httptest.ResponseRecorder`，不依赖本地监听端口、外网或账号。覆盖全部业务路由、请求头/请求体、64 位文件字段、签名、PKCE 标准向量、回调 state 校验、设备码轮询、Token 刷新、上传与移动轮询、错误响应、并发 Token 更新和重定向拦截。

已进行真实只读请求验证：当前本地配置在 OpenAPI 返回 120（client_id not registered）；WebAPI 返回 401（账号错误码 16）。尚未通过有效凭据验证业务成功路径；OpenAPI 多值数组编码仍需平台联调确认。

初版 OpenAPI 验证环境：Go 1.26.1 / macOS arm64。`go test -race`、`go vet`、`go build` 均通过；SDK 包语句覆盖率 93.9%（包含未执行的联网命令行示例后，全模块为 91.3%）。声明兼容 Go 1.22+，未在 Go 1.22 工具链上另行验证。

新增 WebAPI 包在相同环境通过 `go test -race`、`go vet`、`go build`，语句覆盖率 87.0%；原 OpenAPI 包覆盖率保持 93.9%。

package openapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type handlerTransport struct{ handler http.HandlerFunc }

func (h handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	recorder := httptest.NewRecorder()
	h.handler(recorder, r)
	response := recorder.Result()
	response.Request = r
	return response, nil
}

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	c, err := NewClient(Config{ClientID: "client", AccessToken: "token", APIURL: "https://api.example", AccountURL: "https://account.example", HTTPClient: &http.Client{Transport: handlerTransport{handler}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func checkBody(t *testing.T, r *http.Request, want string) {
	t.Helper()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
		return
	}
	if want == "" {
		if len(data) != 0 {
			t.Errorf("unexpected body %s", data)
		}
		return
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(data, &gotValue); err != nil {
		t.Error(err)
		return
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("body = %s; want %s", data, want)
	}
}

func TestBusinessEndpointContracts(t *testing.T) {
	ctx := WithTraceparent(context.Background(), "00-01234567890123456789012345678901-0123456789012345-01")
	cases := []struct {
		name, method, path, query, body, response string
		invoke                                    func(*Client) error
	}{
		{"list", "GET", fileV1 + "get_file_list", "page=0&pageSize=20&parentId=a%26b&orderBy=0&sortType=0&resType=0&fileTypes=1&fileTypes=2", "",
			`{"total":2,"list":[{"fileId":"1835603479919075411","mineType":"image/jpeg","fileSize":5046219,"ctime":1762931573,"utime":1762931673}]}`,
			func(c *Client) error {
				r, e := c.GetFileList(ctx, FileListRequest{PageSize: 20, ParentID: "a&b", OrderBy: Ptr(OrderByName), SortType: Ptr(SortAscending), ResType: Ptr(ResourceUnknown), FileTypes: []FileType{FileTypeImage, FileTypeVideo}})
				if e == nil && (r.Data.Total != 2 || r.Data.List[0].MIMEType != "image/jpeg" || r.Data.List[0].UpdatedAt != 1762931673 || r.Data.List[0].FileID != "1835603479919075411") {
					t.Error("file list fields lost")
				}
				return e
			}},
		{"detail", "GET", fileV1 + "get_file_detail", "fileId=a%26b", "",
			`{"fileInfo":{"fileId":"a&b","fileSize":2438173414},"videoResource":[{"bizId":"biz","info":{"resolution":{"width":1920,"height":1080},"frameRate":23,"defaultResolution":true}}],"sizeInfo":{"size":2438173414,"subDirCount":2,"subFileCount":3}}`,
			func(c *Client) error {
				r, e := c.GetFileDetail(ctx, "a&b")
				if e == nil && (r.Data.FileInfo.FileSize != 2438173414 || r.Data.VideoResource[0].Info.Resolution.Width != 1920 || r.Data.SizeInfo.SubFileCount != 3) {
					t.Error("detail fields lost")
				}
				return e
			}},
		{"download", "GET", fileV1 + "get_res_download_url", "fileId=id", "", `{"signedURL":"https://cdn.example/file","urlDuration":21600}`,
			func(c *Client) error {
				r, e := c.GetResDownloadURL(ctx, "id")
				if e == nil && r.Data.URLDuration != 21600 {
					t.Error("duration lost")
				}
				return e
			}},
		{"vod", "GET", fileV1 + "get_vod_download_url", "fileId=id&bizId=biz&requestId=resume", "", `{"signedURL":"https://cdn.example/video","urlDuration":3600,"requestId":"resume","speedupSignature":"s"}`,
			func(c *Client) error {
				r, e := c.GetVODDownloadURL(ctx, VODDownloadRequest{"id", "biz", "resume"})
				if e == nil && (r.Data.RequestID != "resume" || r.Data.SignedURL != "https://cdn.example/video" || r.Data.SpeedupSignature != "s") {
					t.Error("VOD fields lost")
				}
				return e
			}},
		{"user", "GET", "/openapi/v1/user/get_user_info", "", "", `{"userId":"u","nickName":"用户","vipStatus":2,"vipLeftSeconds":86400,"totalSpace":1099511627776,"usedSpace":100}`,
			func(c *Client) error {
				r, e := c.GetUserInfo(ctx)
				if e == nil && (r.Data.VIPStatus != VIPActive || r.Data.TotalSpace != 1099511627776) {
					t.Error("user fields lost")
				}
				return e
			}},
		{"mkdir", "POST", fileV1 + "create_dir", "", `{"dirName":"文档","parentId":"","failIfNameExist":false}`, `{"fileId":"dir","fileName":"文档","resType":2}`,
			func(c *Client) error {
				r, e := c.CreateDir(ctx, CreateDirRequest{DirName: "文档"})
				if e == nil && r.Data.ResType != ResourceDirectory {
					t.Error("directory fields lost")
				}
				return e
			}},
		{"rename", "POST", fileV1 + "rename", "", `{"fileId":"id","newName":"新名字.txt"}`, "",
			func(c *Client) error { return c.Rename(ctx, RenameRequest{"id", "新名字.txt"}) }},
		{"move", "POST", fileV1 + "move_file", "", `{"fileIds":["id"],"parentId":""}`, `{"taskId":"move-task"}`,
			func(c *Client) error {
				r, e := c.MoveFile(ctx, MoveFileRequest{FileIDs: []string{"id"}})
				if e == nil && r.Data.TaskID != "move-task" {
					t.Error("task lost")
				}
				return e
			}},
		{"task", "POST", fileV1 + "get_task_status", "", `{"taskId":"move-task"}`, `{"status":2}`,
			func(c *Client) error {
				r, e := c.GetTaskStatus(ctx, "move-task")
				if e == nil && r.Data.Status != TaskCompleted {
					t.Error("status lost")
				}
				return e
			}},
		{"upload", "POST", "/openapi/v2/file/get_res_center_token", "", `{"capacity":30,"name":"zero.txt","parentId":"","res":{"fileSize":0}}`,
			`{"gcid":"g","provider":1,"creds":{"accessKeyID":"k","secretAccessKey":"s","sessionToken":"t","expiration":"2026-09-16T12:30:00Z"},"endPoint":"e","fullEndPoint":"https://e","bucketName":"b","objectPath":"o","callback":"cb","callbackVar":"cv","region":"r","taskId":"upload-task"}`,
			func(c *Client) error {
				r, e := c.GetResCenterToken(ctx, UploadTokenRequest{Name: "zero.txt"})
				if e == nil && (r.Data.Credentials.AccessKeyID != "k" || r.Data.Callback != "cb" || r.Data.CallbackVar != "cv" || r.Data.FullEndpoint != "https://e") {
					t.Error("credentials lost")
				}
				return e
			}},
		{"flash", "POST", fileV1 + "check_can_flash_upload", "", `{"taskId":"task","md5":"0123456789abcdef0123456789abcdef"}`, `{"taskId":"task","canFlashUpload":true}`,
			func(c *Client) error {
				r, e := c.CheckCanFlashUpload(ctx, FlashUploadRequest{TaskID: "task", MD5: "0123456789abcdef0123456789abcdef"})
				if e == nil && !r.Data.CanFlashUpload {
					t.Error("flash state lost")
				}
				return e
			}},
		{"resume", "POST", fileV1 + "get_res_center_resume_token", "", `{"capacity":30,"taskId":"task","res":{"fileSize":10485760,"cid":"cid"},"object":{"provider":1,"objectPath":"original/path"}}`, `{"taskId":"task","provider":1,"objectPath":"original/path","creds":{"sessionToken":"new"}}`,
			func(c *Client) error {
				r, e := c.GetResCenterResumeToken(ctx, ResumeTokenRequest{TaskID: "task", Resource: UploadResource{FileSize: 10485760, CID: "cid"}, Object: UploadObject{Provider: 1, ObjectPath: "original/path"}})
				if e == nil && r.Data.Credentials.SessionToken != "new" {
					t.Error("resume fields lost")
				}
				return e
			}},
		{"upload result", "POST", fileV1 + "get_info_by_task_id", "", `{"taskId":"task"}`, `{"fileId":"final","fileSize":10485760}`,
			func(c *Client) error {
				r, e := c.GetInfoByTaskID(ctx, "task")
				if e == nil && r.Data.FileID != "final" {
					t.Error("fileId lost")
				}
				return e
			}},
		{"cancel upload", "POST", fileV1 + "delete_upload_task", "", `{"taskIds":["a","b"]}`, `{"taskIds":["a","b"]}`,
			func(c *Client) error {
				r, e := c.DeleteUploadTask(ctx, []string{"a", "b"})
				if e == nil && len(r.Data.TaskIDs) != 2 {
					t.Error("taskIds lost")
				}
				return e
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("route %s %s; want %s %s", r.Method, r.URL.Path, tc.method, tc.path)
				}
				q, _ := url.ParseQuery(tc.query)
				if !reflect.DeepEqual(r.URL.Query(), q) {
					t.Errorf("query %v; want %v", r.URL.Query(), q)
				}
				if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("x-client-id") != "client" || r.Header.Get("traceparent") == "" {
					t.Error("missing common headers")
				}
				for _, k := range []string{"timestamp", "sign", "x-device-id", "x-project-id"} {
					if r.Header.Get(k) != "" {
						t.Errorf("business request leaked %s", k)
					}
				}
				if tc.method == "POST" && r.Header.Get("Content-Type") != "application/json" {
					t.Error("missing content type")
				}
				checkBody(t, r, tc.body)
				if tc.response == "" {
					io.WriteString(w, `{"code":0,"msg":"success"}`)
				} else {
					fmt.Fprintf(w, `{"code":0,"msg":"success","data":%s}`, tc.response)
				}
			})
			if err := tc.invoke(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestErrorsAndUploadCompleted(t *testing.T) {
	for _, code := range []int{101, 111, 112, 116, 117, 120, 123, 145, 146, 147, 149, 152, 163, 164, 167, 430} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"code":%d,"msg":"rejected"}`, code) })
			r, err := c.GetUserInfo(context.Background())
			if !IsCode(fmt.Errorf("wrapped: %w", err), code) || r == nil || r.Code != code {
				t.Fatalf("response=%+v err=%v", r, err)
			}
		})
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":156,"msg":"上传已完成","data":{"provider":1,"taskId":"done"}}`)
	})
	r, err := c.GetResCenterToken(context.Background(), UploadTokenRequest{Name: "demo"})
	if err != nil || r.Code != 156 || r.Data.TaskID != "done" || r.Data.Credentials != nil {
		t.Fatalf("%+v %v", r, err)
	}
	_, err = c.GetUserInfo(context.Background())
	if !IsCode(err, 156) {
		t.Fatalf("156 must not be global success: %v", err)
	}
}

func TestMalformedBusinessResponses(t *testing.T) {
	for _, body := range []string{"", "<html>error</html>", "null", `{}`, `{"code":"0"}`, `{"code":0}`, `{"code":0,"data":null}`, `{"code":0,"data":[]}`, `{"code":0,"data":{"total":"wrong"}}`, `{"code":0,"data":{}} trailing`} {
		t.Run(body, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
			_, err := c.GetFileList(context.Background(), FileListRequest{PageSize: 20})
			var pe *ProtocolError
			if !errors.As(err, &pe) {
				t.Fatalf("wanted ProtocolError, got %v", err)
			}
		})
	}
}

func TestHTTPAndLimits(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(429)
		io.WriteString(w, "sensitive-body")
	})
	_, err := c.GetUserInfo(context.Background())
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != 429 || he.RetryAfter != "3" || strings.Contains(err.Error(), "sensitive") {
		t.Fatal(err)
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", 101)) })
	c.maxResponseBytes = 100
	_, err = c.GetUserInfo(context.Background())
	var pe *ProtocolError
	if !errors.As(err, &pe) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.GetUserInfo(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host == "destination.example" {
			calls.Add(1)
			return
		}
		http.Redirect(w, r, "https://destination.example", http.StatusTemporaryRedirect)
	})
	_, err := c.GetUserInfo(context.Background())
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != 307 || calls.Load() != 0 {
		t.Fatalf("redirect followed: %v", err)
	}
}

func TestConcurrentTokenUpdates(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("missing token")
		}
		io.WriteString(w, `{"code":0,"data":{"userId":"u"}}`)
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.SetAccessToken(fmt.Sprint(i))
			if _, err := c.GetUserInfo(context.Background()); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
}

func TestConfigAndValidation(t *testing.T) {
	for _, cfg := range []Config{{}, {ClientID: "c", APIURL: "relative"}, {ClientID: "c", AccountURL: "https://user:pass@example.com"}, {ClientID: "c", AuthorizeURL: "https://example.com/?query=x"}, {ClientID: "c", MaxResponseBytes: -1}} {
		if _, err := NewClient(cfg); err == nil {
			t.Errorf("accepted invalid config")
		}
	}
	c, err := NewClient(Config{ClientID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if c.apiURL != ProductionAPIURL || c.accountURL != ProductionAccountURL || c.http.Timeout != 30*time.Second {
		t.Error("bad defaults")
	}
	if _, err := c.GetUserInfo(context.Background()); err == nil {
		t.Error("missing token allowed")
	}
	for _, name := range []string{"", ".", "..", "dir/child", "CON.txt", "LPT1", "NUL", "bad ", "bad.", "a\x00b", strings.Repeat("中", 256)} {
		if err := validateWindowsName(name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	for _, name := range []string{"文档", "demo.mp4", "COM10", strings.Repeat("中", 255)} {
		if err := validateWindowsName(name); err != nil {
			t.Errorf("rejected %q: %v", name, err)
		}
	}
}

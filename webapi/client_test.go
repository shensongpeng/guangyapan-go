package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	c, e := NewClient(Config{ClientID: "web-client", DeviceID: "device", AccessToken: "web-token", DisableRateLimit: true, HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if e := r.Context().Err(); e != nil {
			return nil, e
		}
		w := httptest.NewRecorder()
		handler(w, r)
		resp := w.Result()
		resp.Request = r
		return resp, nil
	})}})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func onlyError[T any](_ T, e error) error { return e }
func TestRoutesAndProtocolIsolation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, path, body, response string
		invoke                     func(*Client) error
	}{
		{"list", "/userres/v1/file/get_file_list", `{"parentId":"","page":0,"pageSize":20,"orderBy":0,"sortType":0,"fileTypes":[]}`, `{"code":0,"data":{"list":[],"total":0}}`, func(c *Client) error { return onlyError(c.GetFileList(ctx, FileListRequest{PageSize: 20})) }},
		{"alternate list", resourcePath + "file/get_file_list", `{"parentId":"p","page":1,"pageSize":5,"orderBy":3,"sortType":1,"fileTypes":[1,2]}`, `{"code":0,"data":{"list":[],"total":0}}`, func(c *Client) error {
			return onlyError(c.GetResourceFileList(ctx, FileListRequest{ParentID: "p", Page: 1, PageSize: 5, OrderBy: 3, SortType: 1, FileTypes: []int{1, 2}}))
		}},
		{"download", resourcePath + "get_res_download_url", `{"fileId":"f"}`, `{"code":0,"data":{"downloadUrl":"https://cdn.example/download"}}`, func(c *Client) error { return onlyError(c.GetDownloadURL(ctx, "f")) }},
		{"mkdir", resourcePath + "file/create_dir", `{"parentId":"","dirName":"文档"}`, `{"code":0,"data":{"fileId":"dir"}}`, func(c *Client) error { return onlyError(c.CreateDir(ctx, "", "文档")) }},
		{"rename", resourcePath + "file/rename", `{"fileId":"f","newName":"new.txt"}`, `{"code":0}`, func(c *Client) error { return c.Rename(ctx, "f", "new.txt") }},
		{"delete", resourcePath + "file/delete_file", `{"fileIds":["f"]}`, `{"code":0}`, func(c *Client) error { return onlyError(c.DeleteFiles(ctx, []string{"f"})) }},
		{"move", resourcePath + "file/move_file", `{"fileIds":["a","b"],"parentId":"p"}`, `{"code":0,"data":{"taskId":"task"}}`, func(c *Client) error { return onlyError(c.MoveFiles(ctx, []string{"a", "b"}, "p")) }},
		{"copy", resourcePath + "file/copy_file", `{"fileIds":["f"],"parentId":""}`, `{"code":0,"data":{}}`, func(c *Client) error { return onlyError(c.CopyFiles(ctx, []string{"f"}, "")) }},
		{"task", resourcePath + "get_task_status", `{"taskId":"task"}`, `{"code":0,"data":{"status":2}}`, func(c *Client) error { return onlyError(c.GetTaskStatus(ctx, "task")) }},
		{"upload", resourcePath + "get_res_center_token", `{"capacity":2,"parentId":"","name":"file.txt","res":{"fileSize":4294967296}}`, `{"code":156,"msg":"上传已完成","data":{"taskId":"upload"}}`, func(c *Client) error { return onlyError(c.GetUploadToken(ctx, "", "file.txt", 4294967296)) }},
		{"upload info", resourcePath + "file/get_info_by_task_id", `{"taskId":"upload"}`, `{"code":0,"data":{"fileId":"final"}}`, func(c *Client) error { return onlyError(c.GetUploadInfo(ctx, "upload")) }},
		{"resolve", "/cloudcollection/v1/resolve_res", `{"url":"magnet:?xt=test"}`, `{"code":0,"data":{"url":"magnet:?xt=test","resType":1}}`, func(c *Client) error { return onlyError(c.ResolveOfflineResource(ctx, "magnet:?xt=test")) }},
		{"create offline", "/cloudcollection/v1/create_task", `{"url":"https://example.com/file","parentId":"","newName":"file","fileIndexes":[0,2]}`, `{"code":0,"data":{"taskId":"off"}}`, func(c *Client) error {
			return onlyError(c.CreateOfflineTask(ctx, CreateOfflineRequest{URL: "https://example.com/file", NewName: "file", FileIndexes: []int{0, 2}}))
		}},
		{"list offline", "/cloudcollection/v1/list_task", `{"taskIds":["t"],"status":[0,1],"cursor":"c","pageSize":5}`, `{"code":0,"data":{"list":[],"cursor":"next","total":0}}`, func(c *Client) error {
			return onlyError(c.ListOfflineTasks(ctx, ListOfflineRequest{TaskIDs: []string{"t"}, Statuses: []int{0, 1}, Cursor: "c", PageSize: 5}))
		}},
		{"delete offline", "/cloudcollection/v2/delete_task", `{"taskIds":["t"]}`, `{"code":0,"data":{"taskIds":["t"]}}`, func(c *Client) error { return onlyError(c.DeleteOfflineTasks(ctx, []string{"t"})) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.Method != "POST" || r.URL.Host != "api.guangyapan.com" || r.URL.Path != tc.path || r.URL.RawQuery != "" {
					t.Errorf("wrong WebAPI route %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Did") != "device" || r.Header.Get("Dt") != "4" || r.Header.Get("Authorization") != "Bearer web-token" {
					t.Error("wrong WebAPI headers")
				}
				for _, h := range []string{"x-client-id", "x-device-id", "x-project-id", "timestamp", "sign"} {
					if r.Header.Get(h) != "" {
						t.Errorf("OpenAPI/account header leaked: %s", h)
					}
				}
				var got, want any
				if json.NewDecoder(r.Body).Decode(&got) != nil {
					t.Fatal("bad body")
				}
				json.Unmarshal([]byte(tc.body), &want)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("body %v want %v", got, want)
				}
				io.WriteString(w, tc.response)
			})
			if e := tc.invoke(c); e != nil {
				t.Fatal(e)
			}
			if !called {
				t.Fatal("no request")
			}
		})
	}
}
func TestAccountContracts(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, path, body, response string
		authorized                 bool
		invoke                     func(*Client) error
	}{
		{"me", "/v1/user/me", "", `{"sub":"user"}`, true, func(c *Client) error { return onlyError(c.GetUserInfo(ctx)) }},
		{"refresh", "/v1/auth/token", `{"client_id":"web-client","grant_type":"refresh_token","refresh_token":"refresh"}`, `{"access_token":"new","expires_in":60}`, false, func(c *Client) error {
			r, e := c.RefreshToken(ctx, "refresh")
			if e == nil && (r.RefreshToken != "refresh" || r.ExpiresAt.IsZero()) {
				t.Error("token retention or expiry")
			}
			return e
		}},
		{"captcha", "/v1/shield/captcha/init", `{"client_id":"web-client","device_id":"device","action":"POST:/v1/auth/verification","meta":{"username":"+8613800000000","phone_number":"+8613800000000","VERIFICATION_PHONE":"+8613800000000"}}`, `{"captcha_token":"captcha","expires_in":60}`, false, func(c *Client) error { return onlyError(c.InitCaptcha(ctx, "+86 13800000000")) }},
		{"SMS", "/v1/auth/verification", `{"client_id":"web-client","phone_number":"+8613800000000","target":"ANY"}`, `{"verification_id":"vid"}`, false, func(c *Client) error { return onlyError(c.SendSMS(ctx, "+8613800000000", "captcha")) }},
		{"verify", "/v1/auth/verification/verify", `{"client_id":"web-client","verification_id":"vid","verification_code":"123456"}`, `{"verification_token":"verified"}`, false, func(c *Client) error { return onlyError(c.VerifySMS(ctx, "vid", "123456")) }},
		{"signin", "/v1/auth/signin", `{"client_id":"web-client","username":"+8613800000000","verification_code":"123456","verification_token":"verified"}`, `{"access_token":"new","refresh_token":"r","expires_in":60}`, false, func(c *Client) error { return onlyError(c.SignIn(ctx, "+8613800000000", "123456", "verified")) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Host != "account.guangyapan.com" || r.URL.Path != tc.path {
					t.Error("wrong account route")
				}
				method := "POST"
				if tc.authorized {
					method = "GET"
				}
				if r.Method != method {
					t.Error("wrong method")
				}
				if r.Header.Get("X-Client-Id") != "web-client" || r.Header.Get("X-Device-Id") != "device" || r.Header.Get("X-Protocol-Version") != "301" || r.Header.Get("X-Device-Sign") != "wdi10.devicexxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" {
					t.Error("missing account profile")
				}
				if r.Header.Get("Did") != "" || r.Header.Get("Dt") != "" || r.Header.Get("Sign") != "" {
					t.Error("business headers leaked")
				}
				if tc.authorized {
					if r.Header.Get("Authorization") != "Bearer web-token" {
						t.Error("missing auth")
					}
				} else if r.Header.Get("Authorization") != "" {
					t.Error("token leaked to sign-in")
				}
				if tc.name == "SMS" && r.Header.Get("X-Captcha-Token") != "captcha" {
					t.Error("captcha missing")
				}
				if tc.body != "" {
					var got, want any
					json.NewDecoder(r.Body).Decode(&got)
					json.Unmarshal([]byte(tc.body), &want)
					if !reflect.DeepEqual(got, want) {
						t.Errorf("wrong body %v", got)
					}
				}
				io.WriteString(w, tc.response)
			})
			if e := tc.invoke(c); e != nil {
				t.Fatal(e)
			}
			if c.accessToken != "web-token" {
				t.Error("auth unexpectedly mutated token")
			}
		})
	}
}
func TestErrorHandling(t *testing.T) {
	for _, b := range []string{`{}`, `null`, `{"code":0}`, `{"code":0,"data":null}`, `{"code":0,"data":[]}`, `{"code":"0"}`, `not-json`} {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, b) })
		if _, e := c.GetFileList(context.Background(), FileListRequest{PageSize: 1}); !errors.Is(e, ErrInvalidResponse) {
			t.Errorf("%s: %v", b, e)
		}
	}
	for _, code := range []string{"117", "147", "156"} {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"code":`+code+`,"msg":"secret server text"}`)
		})
		_, e := c.GetFileList(context.Background(), FileListRequest{PageSize: 1})
		var a *APIError
		if !errors.As(e, &a) || strings.Contains(e.Error(), "secret") {
			t.Fatal(e)
		}
	}
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":"invalid_token","error_code":401,"error_description":"secret"}`)
	})
	_, e := c.GetUserInfo(context.Background())
	var a *AccountError
	if !errors.As(e, &a) || a.ErrorName != "invalid_token" || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
	c = newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "5"); w.WriteHeader(429) })
	_, e = c.GetFileList(context.Background(), FileListRequest{PageSize: 1})
	var h *HTTPError
	if !errors.As(e, &h) || h.RetryAfter != "5" {
		t.Fatal(e)
	}
	c = newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"url":"https://account.guangyapan.com/challenge"}`)
	})
	cap, e := c.InitCaptcha(context.Background(), "+8613800000000")
	if !errors.Is(e, ErrCaptchaRequired) || cap.URL == "" {
		t.Fatal(e)
	}
}
func TestUploadCredentialsAndCompletion(t *testing.T) {
	for _, b := range []string{`{"code":0,"data":{"taskId":"t","fullEndPoint":"https://oss.example","creds":{"accessKeyID":"key","secretAccessKey":"secret","sessionToken":"session"}}}`, `{"code":0,"data":{"taskId":"t","endPoint":"https://oss.example","accessKeyID":"key","secretAccessKey":"secret","sessionToken":"session"}}`} {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, b) })
		r, e := c.GetUploadToken(context.Background(), "", "zero.txt", 0)
		if e != nil || r.Data.AccessKeyID != "key" || r.Data.SecretAccessKey != "secret" || r.Data.SessionToken != "session" || r.Data.Endpoint != "https://oss.example" {
			t.Fatal("credential normalization failed", e)
		}
	}
	var n atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			io.WriteString(w, `{"code":147,"msg":"processing"}`)
		} else {
			io.WriteString(w, `{"code":0,"data":{"fileId":"done"}}`)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, e := c.WaitUpload(ctx, "t")
	if e != nil || r.Data.FileID != "done" || n.Load() != 2 {
		t.Fatal(e)
	}
}
func TestTaskStatesAndCancellation(t *testing.T) {
	for _, status := range []string{"-1", "3"} {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"code":0,"data":{"status":`+status+`}}`)
		})
		if _, e := c.WaitTask(context.Background(), "t"); !errors.Is(e, ErrTaskFailed) {
			t.Fatal(e)
		}
	}
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"code":0,"data":{}}`) })
	if _, e := c.GetTaskStatus(context.Background(), "t"); !errors.Is(e, ErrInvalidResponse) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.GetFileList(ctx, FileListRequest{PageSize: 1}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestConcurrencyRateLimitAndRedirect(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"code":0,"data":{"list":[]}}`) })
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.SetAccessToken("new")
			if _, e := c.GetFileList(context.Background(), FileListRequest{PageSize: 1}); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	c.disableRateLimit = false
	start := time.Now()
	c.throttle(context.Background(), "test")
	c.throttle(context.Background(), "test")
	if time.Since(start) < 500*time.Millisecond {
		t.Fatal("rate limit ignored")
	}
	var calls atomic.Int32
	c = newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "https://other.example", 307)
	})
	_, e := c.GetUserInfo(context.Background())
	var h *HTTPError
	if !errors.As(e, &h) || h.StatusCode != 307 || calls.Load() != 1 {
		t.Fatal("redirect followed")
	}
}
func TestConfigAndValidation(t *testing.T) {
	c, e := NewClient(Config{})
	if e != nil || c.clientID != DefaultClientID || len(c.DeviceID()) != 32 {
		t.Fatal(e)
	}
	for _, cfg := range []Config{{APIURL: "relative"}, {AccountURL: "https://user:pass@host"}, {MaxResponseBytes: -1}} {
		if _, e := NewClient(cfg); e == nil {
			t.Fatal("invalid config accepted")
		}
	}
	c = newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request sent") })
	ctx := context.Background()
	checks := []error{
		onlyError(c.GetFileList(ctx, FileListRequest{})), onlyError(c.GetDownloadURL(ctx, "")), onlyError(c.CreateDir(ctx, "", "")), c.Rename(ctx, "f", ""), onlyError(c.DeleteFiles(ctx, nil)), onlyError(c.MoveFiles(ctx, []string{""}, "")), onlyError(c.GetTaskStatus(ctx, "")), onlyError(c.GetUploadToken(ctx, "", "name", -1)), onlyError(c.GetUploadInfo(ctx, "")), onlyError(c.RefreshToken(ctx, "")), onlyError(c.VerifySMS(ctx, "", "")), onlyError(c.SendSMS(ctx, "bad", "")), onlyError(c.SignIn(ctx, "+8613800000000", "", "")), onlyError(c.ResolveOfflineResource(ctx, "")), onlyError(c.CreateOfflineTask(ctx, CreateOfflineRequest{})), onlyError(c.ListOfflineTasks(ctx, ListOfflineRequest{PageSize: -1})), onlyError(c.DeleteOfflineTasks(ctx, nil)),
	}
	for i, e := range checks {
		if e == nil {
			t.Errorf("validation %d", i)
		}
	}
}

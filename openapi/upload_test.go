package openapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskPolling(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fileV1+"get_task_status" {
			t.Error("move polled upload endpoint")
		}
		if calls.Add(1) == 1 {
			io.WriteString(w, `{"code":0,"data":{"status":1}}`)
		} else {
			io.WriteString(w, `{"code":0,"data":{"status":2}}`)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	r, err := c.WaitTask(ctx, "move", time.Second)
	if err != nil || r.Data.Status != TaskCompleted || calls.Load() != 2 || time.Since(start) < time.Second {
		t.Fatalf("task: %+v %v", r, err)
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"code":0,"data":{"status":3}}`) })
	r, err = c.WaitTask(ctx, "failed", time.Second)
	if !errors.Is(err, ErrTaskFailed) || r.Data.Status != TaskFailed {
		t.Fatal(err)
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"code":0,"data":{"status":99}}`) })
	_, err = c.GetTaskStatus(ctx, "unknown")
	var pe *ProtocolError
	if !errors.As(err, &pe) {
		t.Fatal(err)
	}
}

func TestUploadPolling(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fileV1+"get_info_by_task_id" {
			t.Error("upload polled move endpoint")
		}
		switch calls.Add(1) {
		case 1:
			io.WriteString(w, `{"code":147,"msg":"文件上传中"}`)
		case 2:
			io.WriteString(w, `{"code":0,"data":{"fileId":""}}`)
		default:
			io.WriteString(w, `{"code":0,"data":{"fileId":"finished"}}`)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	start := time.Now()
	r, err := c.WaitUpload(ctx, "task")
	if err != nil || r.Data.FileID != "finished" || calls.Load() != 3 || time.Since(start) < 3*time.Second {
		t.Fatalf("upload: %+v %v", r, err)
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"code":163,"msg":"expired"}`) })
	_, err = c.WaitUpload(ctx, "task")
	if !IsCode(err, 163) {
		t.Fatal(err)
	}
}

func TestPollingCancellation(t *testing.T) {
	for _, upload := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			if upload {
				io.WriteString(w, `{"code":147,"msg":"pending"}`)
			} else {
				io.WriteString(w, `{"code":0,"data":{"status":0}}`)
			}
			cancel()
		})
		var err error
		if upload {
			_, err = c.WaitUpload(ctx, "task")
		} else {
			_, err = c.WaitTask(ctx, "task", time.Second)
		}
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
}

func TestBusinessValidation(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request reached server") })
	ctx := context.Background()
	checks := []func() error{
		func() error { _, e := c.GetFileList(ctx, FileListRequest{}); return e },
		func() error { _, e := c.GetFileList(ctx, FileListRequest{PageSize: 1, Page: -1}); return e },
		func() error {
			_, e := c.GetFileList(ctx, FileListRequest{PageSize: 1, OrderBy: Ptr(OrderBy(99))})
			return e
		},
		func() error {
			_, e := c.GetFileList(ctx, FileListRequest{PageSize: 1, SortType: Ptr(SortType(99))})
			return e
		},
		func() error {
			_, e := c.GetFileList(ctx, FileListRequest{PageSize: 1, ResType: Ptr(ResourceType(99))})
			return e
		},
		func() error {
			_, e := c.GetFileList(ctx, FileListRequest{PageSize: 1, FileTypes: []FileType{99}})
			return e
		},
		func() error { _, e := c.GetFileDetail(ctx, ""); return e },
		func() error { _, e := c.GetResDownloadURL(ctx, ""); return e },
		func() error { _, e := c.GetVODDownloadURL(ctx, VODDownloadRequest{FileID: "id"}); return e },
		func() error { _, e := c.CreateDir(ctx, CreateDirRequest{DirName: "NUL"}); return e },
		func() error { return c.Rename(ctx, RenameRequest{NewName: "valid"}) },
		func() error { return c.Rename(ctx, RenameRequest{FileID: "id", NewName: "bad/"}) },
		func() error { _, e := c.MoveFile(ctx, MoveFileRequest{}); return e },
		func() error { _, e := c.MoveFile(ctx, MoveFileRequest{FileIDs: make([]string, 501)}); return e },
		func() error { _, e := c.MoveFile(ctx, MoveFileRequest{FileIDs: []string{""}}); return e },
		func() error { _, e := c.GetTaskStatus(ctx, ""); return e },
		func() error { _, e := c.WaitTask(ctx, "task", time.Millisecond); return e },
		func() error { _, e := c.GetResCenterToken(ctx, UploadTokenRequest{}); return e },
		func() error {
			_, e := c.GetResCenterToken(ctx, UploadTokenRequest{Name: "a", Resource: UploadResource{MD5: "bad"}})
			return e
		},
		func() error {
			_, e := c.GetResCenterToken(ctx, UploadTokenRequest{Name: "a", Resource: UploadResource{GCID: "bad"}})
			return e
		},
		func() error {
			_, e := c.CheckCanFlashUpload(ctx, FlashUploadRequest{TaskID: strings.Repeat("t", 41)})
			return e
		},
		func() error { _, e := c.CheckCanFlashUpload(ctx, FlashUploadRequest{TaskID: "t"}); return e },
		func() error {
			_, e := c.CheckCanFlashUpload(ctx, FlashUploadRequest{TaskID: "t", GCID: "bad", CID: "cid"})
			return e
		},
		func() error {
			_, e := c.CheckCanFlashUpload(ctx, FlashUploadRequest{TaskID: "t", GCID: strings.Repeat("a", 40)})
			return e
		},
		func() error { _, e := c.GetResCenterResumeToken(ctx, ResumeTokenRequest{}); return e },
		func() error {
			_, e := c.GetResCenterResumeToken(ctx, ResumeTokenRequest{TaskID: "t", Object: UploadObject{ObjectPath: "o"}, Resource: UploadResource{MD5: "bad"}})
			return e
		},
		func() error { _, e := c.GetInfoByTaskID(ctx, ""); return e },
		func() error { _, e := c.DeleteUploadTask(ctx, nil); return e },
	}
	for i, check := range checks {
		if e := check(); e == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

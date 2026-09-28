package webapi

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const resourcePath = "/nd.bizuserres.s/v1/"

type File struct {
	FileID       string `json:"fileId"`
	FileName     string `json:"fileName"`
	ParentID     string `json:"parentId"`
	FileSize     int64  `json:"fileSize"`
	ResourceType int    `json:"resType"`
	CreatedAt    int64  `json:"ctime"`
	UpdatedAt    int64  `json:"utime"`
}
type FileList struct {
	Total int64  `json:"total"`
	List  []File `json:"list"`
}
type FileListRequest struct {
	ParentID  string `json:"parentId"`
	Page      int    `json:"page"`
	PageSize  int    `json:"pageSize"`
	OrderBy   int    `json:"orderBy"`
	SortType  int    `json:"sortType"`
	FileTypes []int  `json:"fileTypes"`
}

func (c *Client) GetFileList(ctx context.Context, in FileListRequest) (*Response[FileList], error) {
	return c.list(ctx, "/userres/v1/file/get_file_list", in)
}

// GetResourceFileList uses the alternate listing route used by AList's folder
// path resolver. Both are WebAPI POST endpoints, never OpenAPI GET endpoints.
func (c *Client) GetResourceFileList(ctx context.Context, in FileListRequest) (*Response[FileList], error) {
	return c.list(ctx, resourcePath+"file/get_file_list", in)
}
func (c *Client) list(ctx context.Context, path string, in FileListRequest) (*Response[FileList], error) {
	if in.Page < 0 || in.PageSize <= 0 || in.OrderBy < 0 || in.OrderBy > 4 || in.SortType < 0 || in.SortType > 1 {
		return nil, errors.New("webapi: invalid list options")
	}
	if in.FileTypes == nil {
		in.FileTypes = []int{}
	}
	return post[FileList](ctx, c, path, in, false, false)
}

type DownloadURL struct {
	SignedURL   string `json:"signedURL"`
	DownloadURL string `json:"downloadUrl"`
}

func (d DownloadURL) URL() string {
	if strings.TrimSpace(d.SignedURL) != "" {
		return d.SignedURL
	}
	return d.DownloadURL
}
func (c *Client) GetDownloadURL(ctx context.Context, fileID string) (*Response[DownloadURL], error) {
	if e := required(fileID); e != nil {
		return nil, e
	}
	r, e := post[DownloadURL](ctx, c, resourcePath+"get_res_download_url", map[string]string{"fileId": fileID}, false, false)
	if e == nil && strings.TrimSpace(r.Data.URL()) == "" {
		return r, ErrInvalidResponse
	}
	return r, e
}
func nameValid(name string) error {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 255 {
		return errors.New("webapi: invalid name")
	}
	return required(name)
}
func (c *Client) CreateDir(ctx context.Context, parentID, name string) (*Response[File], error) {
	if e := nameValid(name); e != nil {
		return nil, e
	}
	return post[File](ctx, c, resourcePath+"file/create_dir", map[string]string{"parentId": parentID, "dirName": name}, false, false)
}
func (c *Client) Rename(ctx context.Context, fileID, name string) error {
	if e := required(fileID); e != nil {
		return e
	}
	if e := nameValid(name); e != nil {
		return e
	}
	_, e := post[struct{}](ctx, c, resourcePath+"file/rename", map[string]string{"fileId": fileID, "newName": name}, false, true)
	return e
}

type Task struct {
	TaskID string `json:"taskId"`
}
type TaskState struct {
	Status int `json:"status"`
}

func validIDs(ids []string) error {
	if len(ids) == 0 {
		return errors.New("webapi: IDs required")
	}
	return required(ids...)
}

// DeleteFiles may return an empty taskId when applied synchronously.
func (c *Client) DeleteFiles(ctx context.Context, ids []string) (*Response[Task], error) {
	if e := validIDs(ids); e != nil {
		return nil, e
	}
	return post[Task](ctx, c, resourcePath+"file/delete_file", map[string]any{"fileIds": ids}, false, true)
}
func (c *Client) MoveFiles(ctx context.Context, ids []string, parentID string) (*Response[Task], error) {
	return c.relocate(ctx, "move_file", ids, parentID)
}
func (c *Client) CopyFiles(ctx context.Context, ids []string, parentID string) (*Response[Task], error) {
	return c.relocate(ctx, "copy_file", ids, parentID)
}
func (c *Client) relocate(ctx context.Context, operation string, ids []string, parentID string) (*Response[Task], error) {
	if e := validIDs(ids); e != nil {
		return nil, e
	}
	return post[Task](ctx, c, resourcePath+"file/"+operation, map[string]any{"fileIds": ids, "parentId": parentID}, false, true)
}
func (c *Client) GetTaskStatus(ctx context.Context, id string) (*Response[TaskState], error) {
	if e := required(id); e != nil {
		return nil, e
	}
	// A pointer detects missing status, which must not be interpreted as pending.
	r, e := post[struct {
		Status *int `json:"status"`
	}](ctx, c, resourcePath+"get_task_status", Task{id}, false, false)
	if e != nil {
		return nil, e
	}
	if r.Data.Status == nil {
		return nil, ErrInvalidResponse
	}
	status := *r.Data.Status
	if status < -1 || status > 3 {
		return nil, ErrInvalidResponse
	}
	return &Response[TaskState]{Code: r.Code, Message: r.Message, Data: TaskState{status}}, nil
}
func (c *Client) WaitTask(ctx context.Context, id string) (*Response[TaskState], error) {
	for {
		r, e := c.GetTaskStatus(ctx, id)
		if e != nil {
			return r, e
		}
		switch r.Data.Status {
		case 2:
			return r, nil
		case -1, 3:
			return r, ErrTaskFailed
		}
		if e := wait(ctx, time.Second); e != nil {
			return r, e
		}
	}
}

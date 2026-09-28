package openapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const fileV1 = "/openapi/v1/file/"

type FileListRequest struct {
	ParentID  string
	Page      int
	PageSize  int
	OrderBy   *OrderBy
	SortType  *SortType
	FileTypes []FileType
	ResType   *ResourceType
}

// GetFileList uses zero-based page numbers. fileTypes is encoded as repeated
// query keys (fileTypes=1&fileTypes=2); the documentation does not specify its
// wire encoding, so verify multi-type filters during platform integration.
func (c *Client) GetFileList(ctx context.Context, in FileListRequest) (*Response[FileList], error) {
	if in.PageSize <= 0 || in.Page < 0 {
		return nil, invalid("pagination", "requires PageSize > 0 and Page >= 0")
	}
	q := url.Values{"pageSize": {strconv.Itoa(in.PageSize)}, "page": {strconv.Itoa(in.Page)}}
	if in.ParentID != "" {
		q.Set("parentId", in.ParentID)
	}
	if in.OrderBy != nil {
		if *in.OrderBy < OrderByName || *in.OrderBy > OrderByExtension {
			return nil, invalid("OrderBy", "is out of range")
		}
		q.Set("orderBy", strconv.Itoa(int(*in.OrderBy)))
	}
	if in.SortType != nil {
		if *in.SortType != SortAscending && *in.SortType != SortDescending {
			return nil, invalid("SortType", "is out of range")
		}
		q.Set("sortType", strconv.Itoa(int(*in.SortType)))
	}
	if in.ResType != nil {
		if *in.ResType < ResourceUnknown || *in.ResType > ResourceDirectory {
			return nil, invalid("ResType", "is out of range")
		}
		q.Set("resType", strconv.Itoa(int(*in.ResType)))
	}
	for _, t := range in.FileTypes {
		if t < FileTypeUnknown || t > FileTypeCode {
			return nil, invalid("FileTypes", "contains an unknown type")
		}
		q.Add("fileTypes", strconv.Itoa(int(t)))
	}
	return call[FileList](ctx, c, http.MethodGet, fileV1+"get_file_list", q, nil, false, false)
}

func (c *Client) GetFileDetail(ctx context.Context, fileID string) (*Response[FileDetail], error) {
	if fileID == "" {
		return nil, invalid("fileID", "is required")
	}
	return call[FileDetail](ctx, c, http.MethodGet, fileV1+"get_file_detail", url.Values{"fileId": {fileID}}, nil, false, false)
}

func (c *Client) GetResDownloadURL(ctx context.Context, fileID string) (*Response[DownloadURL], error) {
	if fileID == "" {
		return nil, invalid("fileID", "is required")
	}
	return call[DownloadURL](ctx, c, http.MethodGet, fileV1+"get_res_download_url", url.Values{"fileId": {fileID}}, nil, false, false)
}

type VODDownloadRequest struct{ FileID, BizID, RequestID string }

// GetVODDownloadURL accepts the previous RequestID when resuming or refreshing
// an expired URL. Leave RequestID empty only for the first request.
func (c *Client) GetVODDownloadURL(ctx context.Context, in VODDownloadRequest) (*Response[VODDownloadURL], error) {
	if in.FileID == "" || in.BizID == "" {
		return nil, invalid("FileID and BizID", "are required")
	}
	q := url.Values{"fileId": {in.FileID}, "bizId": {in.BizID}}
	if in.RequestID != "" {
		q.Set("requestId", in.RequestID)
	}
	return call[VODDownloadURL](ctx, c, http.MethodGet, fileV1+"get_vod_download_url", q, nil, false, false)
}

func (c *Client) GetUserInfo(ctx context.Context) (*Response[UserInfo], error) {
	return call[UserInfo](ctx, c, http.MethodGet, "/openapi/v1/user/get_user_info", nil, nil, false, false)
}

type CreateDirRequest struct {
	DirName         string `json:"dirName"`
	ParentID        string `json:"parentId"`
	FailIfNameExist bool   `json:"failIfNameExist"`
}

func (c *Client) CreateDir(ctx context.Context, in CreateDirRequest) (*Response[File], error) {
	if err := validateWindowsName(in.DirName); err != nil {
		return nil, err
	}
	return call[File](ctx, c, http.MethodPost, fileV1+"create_dir", nil, in, false, false)
}

type RenameRequest struct {
	FileID  string `json:"fileId"`
	NewName string `json:"newName"`
}

func (c *Client) Rename(ctx context.Context, in RenameRequest) error {
	if in.FileID == "" {
		return invalid("FileID", "is required")
	}
	if err := validateWindowsName(in.NewName); err != nil {
		return err
	}
	_, err := call[struct{}](ctx, c, http.MethodPost, fileV1+"rename", nil, in, false, true)
	return err
}

type MoveFileRequest struct {
	FileIDs  []string `json:"fileIds"`
	ParentID string   `json:"parentId"`
}

func (c *Client) MoveFile(ctx context.Context, in MoveFileRequest) (*Response[Task], error) {
	if err := validateIDs(in.FileIDs, 500); err != nil {
		return nil, err
	}
	return call[Task](ctx, c, http.MethodPost, fileV1+"move_file", nil, in, false, false)
}

func (c *Client) GetTaskStatus(ctx context.Context, taskID string) (*Response[TaskState], error) {
	if taskID == "" {
		return nil, invalid("taskID", "is required")
	}
	r, err := call[TaskState](ctx, c, http.MethodPost, fileV1+"get_task_status", nil, Task{TaskID: taskID}, false, false)
	if err == nil && (r.Data.Status < TaskPending || r.Data.Status > TaskFailed) {
		return r, &ProtocolError{Reason: "unknown task status"}
	}
	return r, err
}

// WaitTask stops at completed/failed; interval must be >= 1s. A deadline on ctx
// is recommended. A failed task returns both its response and ErrTaskFailed.
func (c *Client) WaitTask(ctx context.Context, taskID string, interval time.Duration) (*Response[TaskState], error) {
	if interval < time.Second {
		return nil, invalid("interval", "must be at least one second")
	}
	for {
		r, err := c.GetTaskStatus(ctx, taskID)
		if err != nil {
			return r, err
		}
		switch r.Data.Status {
		case TaskCompleted:
			return r, nil
		case TaskFailed:
			return r, ErrTaskFailed
		}
		if err := wait(ctx, interval); err != nil {
			return r, err
		}
	}
}

func validateIDs(ids []string, max int) error {
	if len(ids) == 0 || (max > 0 && len(ids) > max) {
		return invalid("IDs", "has invalid count")
	}
	for _, id := range ids {
		if id == "" {
			return invalid("IDs", "must not contain empty values")
		}
	}
	return nil
}

func validateName(name string) error {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 255 {
		return invalid("name", "must be 1-255 UTF-8 characters")
	}
	return nil
}

func validateWindowsName(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || strings.IndexFunc(name, func(r rune) bool { return r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) }) >= 0 {
		return invalid("name", "violates Windows filename rules")
	}
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || ((strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && utf8.RuneCountInString(base) == 4 && strings.ContainsRune("123456789¹²³", []rune(base)[3])) {
		return invalid("name", "is a reserved Windows filename")
	}
	return nil
}

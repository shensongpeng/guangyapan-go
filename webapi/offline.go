package webapi

import (
	"context"
	"errors"
)

type OfflineResource struct {
	ResourceType int          `json:"resType"`
	URL          string       `json:"url"`
	BTInfo       *TorrentInfo `json:"btResInfo"`
}
type TorrentInfo struct {
	InfoHash       string        `json:"infoHash"`
	FileName       string        `json:"fileName"`
	FileSize       int64         `json:"fileSize"`
	SubfilesNum    int           `json:"subfilesNum"`
	CreatedAt      int64         `json:"createTime"`
	ExcludeIndices []int         `json:"excludeIndices"`
	Subfiles       []TorrentFile `json:"subfiles"`
}
type TorrentFile struct {
	FileName string `json:"fileName"`
	Index    *int   `json:"fileIndex"`
	Size     int64  `json:"fileSize"`
}
type OfflineTask struct {
	TaskID       string `json:"taskId"`
	URL          string `json:"url"`
	FileName     string `json:"fileName"`
	TotalSize    int64  `json:"totalSize"`
	Status       int    `json:"status"`
	CreatedAt    int64  `json:"createTime"`
	Resource     string `json:"res"`
	ResourceType int    `json:"resType"`
	Progress     int    `json:"progress"`
	FileID       string `json:"fileId"`
	IsDir        bool   `json:"isDir"`
	Exists       bool   `json:"exist"`
}
type CreateOfflineRequest struct {
	URL         string `json:"url"`
	ParentID    string `json:"parentId"`
	NewName     string `json:"newName"`
	FileIndexes []int  `json:"fileIndexes,omitempty"`
}
type ListOfflineRequest struct {
	TaskIDs  []string `json:"taskIds,omitempty"`
	Statuses []int    `json:"status,omitempty"`
	Cursor   string   `json:"cursor,omitempty"`
	PageSize int      `json:"pageSize,omitempty"`
}
type OfflineList struct {
	StatusCounts []struct {
		Status int   `json:"status"`
		Count  int64 `json:"count"`
	} `json:"statusCounts"`
	Cursor string        `json:"cursor"`
	List   []OfflineTask `json:"list"`
	Total  int64         `json:"total"`
}
type DeletedOfflineTasks struct {
	TaskIDs []string `json:"taskIds"`
}

func (c *Client) ResolveOfflineResource(ctx context.Context, resourceURL string) (*Response[OfflineResource], error) {
	if e := required(resourceURL); e != nil {
		return nil, e
	}
	return post[OfflineResource](ctx, c, "/cloudcollection/v1/resolve_res", map[string]string{"url": resourceURL}, false, false)
}
func (c *Client) CreateOfflineTask(ctx context.Context, in CreateOfflineRequest) (*Response[OfflineTask], error) {
	if e := required(in.URL); e != nil {
		return nil, e
	}
	if e := nameValid(in.NewName); e != nil {
		return nil, e
	}
	for _, i := range in.FileIndexes {
		if i < 0 {
			return nil, errors.New("webapi: negative file index")
		}
	}
	r, e := post[OfflineTask](ctx, c, "/cloudcollection/v1/create_task", in, false, false)
	if e == nil && r.Data.TaskID == "" {
		return r, ErrInvalidResponse
	}
	return r, e
}
func (c *Client) ListOfflineTasks(ctx context.Context, in ListOfflineRequest) (*Response[OfflineList], error) {
	if in.PageSize < 0 {
		return nil, errors.New("webapi: negative page size")
	}
	return post[OfflineList](ctx, c, "/cloudcollection/v1/list_task", in, false, false)
}

// DeleteOfflineTasks removes task records. It deliberately exposes no deleteFiles
// switch because AList does not send such a field to this WebAPI endpoint.
func (c *Client) DeleteOfflineTasks(ctx context.Context, ids []string) (*Response[DeletedOfflineTasks], error) {
	if e := validIDs(ids); e != nil {
		return nil, e
	}
	return post[DeletedOfflineTasks](ctx, c, "/cloudcollection/v2/delete_task", map[string]any{"taskIds": ids}, false, true)
}

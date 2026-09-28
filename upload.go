package guangyapan

import (
	"context"
	"encoding/hex"
	"net/http"
	"time"
	"unicode/utf8"
)

// UploadTokenRequest deliberately excludes userId, object and capacity.
// User identity comes from the access token; capacity is always 30.
type UploadTokenRequest struct {
	Name     string         `json:"name"`
	ParentID string         `json:"parentId"`
	Resource UploadResource `json:"res"`
}

// GetResCenterToken returns code 156 as successful completion. In that case,
// skip object storage and query GetInfoByTaskID using Data.TaskID.
func (c *Client) GetResCenterToken(ctx context.Context, in UploadTokenRequest) (*Response[UploadToken], error) {
	if err := validateName(in.Name); err != nil {
		return nil, err
	}
	if err := validateResource(in.Resource); err != nil {
		return nil, err
	}
	body := struct {
		Capacity uint32 `json:"capacity"`
		UploadTokenRequest
	}{30, in}
	return call[UploadToken](ctx, c, http.MethodPost, "/openapi/v2/file/get_res_center_token", nil, body, true, false)
}

type FlashUploadRequest struct {
	TaskID string `json:"taskId"`
	MD5    string `json:"md5,omitempty"`
	GCID   string `json:"gcid,omitempty"`
	CID    string `json:"cid,omitempty"`
}

// CheckCanFlashUpload is needed only after GetResCenterToken returns code 0.
func (c *Client) CheckCanFlashUpload(ctx context.Context, in FlashUploadRequest) (*Response[FlashUploadResult], error) {
	if n := utf8.RuneCountInString(in.TaskID); n < 1 || n > 40 {
		return nil, invalid("TaskID", "must be 1-40 characters")
	}
	if in.MD5 == "" && in.GCID == "" {
		return nil, invalid("MD5 or GCID", "is required")
	}
	if in.GCID != "" && !validHex(in.GCID, 40) {
		return nil, invalid("GCID", "must be 40 hexadecimal characters")
	}
	if !validHex(in.MD5, 32) && in.CID == "" {
		return nil, invalid("CID", "is required without a valid 32-character MD5")
	}
	return call[FlashUploadResult](ctx, c, http.MethodPost, fileV1+"check_can_flash_upload", nil, in, true, false)
}

type ResumeTokenRequest struct {
	TaskID   string         `json:"taskId"`
	Resource UploadResource `json:"res"`
	Object   UploadObject   `json:"object"`
}

// GetResCenterResumeToken requires the original resource size/provider/objectPath.
func (c *Client) GetResCenterResumeToken(ctx context.Context, in ResumeTokenRequest) (*Response[UploadToken], error) {
	if in.TaskID == "" || in.Object.ObjectPath == "" {
		return nil, invalid("TaskID and ObjectPath", "are required")
	}
	if err := validateResource(in.Resource); err != nil {
		return nil, err
	}
	body := struct {
		Capacity uint32 `json:"capacity"`
		ResumeTokenRequest
	}{30, in}
	return call[UploadToken](ctx, c, http.MethodPost, fileV1+"get_res_center_resume_token", nil, body, true, false)
}

// GetInfoByTaskID returns APIError code 147 while upload processing continues.
// It must not be used for move tasks; use GetTaskStatus for those.
func (c *Client) GetInfoByTaskID(ctx context.Context, taskID string) (*Response[File], error) {
	if taskID == "" {
		return nil, invalid("taskID", "is required")
	}
	return call[File](ctx, c, http.MethodPost, fileV1+"get_info_by_task_id", nil, Task{TaskID: taskID}, true, false)
}

// WaitUpload polls on code 147 (or success with no fileId), exponentially backing
// off from 1s to 10s. It returns only after a non-empty fileId or terminal error.
func (c *Client) WaitUpload(ctx context.Context, taskID string) (*Response[File], error) {
	delay := time.Second
	for {
		r, err := c.GetInfoByTaskID(ctx, taskID)
		if err != nil && !IsCode(err, CodeUploadProcessing) {
			return r, err
		}
		if err == nil && r.Data.FileID != "" {
			return r, nil
		}
		if err := wait(ctx, delay); err != nil {
			return r, err
		}
		delay *= 2
		if delay > 10*time.Second {
			delay = 10 * time.Second
		}
	}
}

// DeleteUploadTask cancels upload tasks; it does not delete completed drive files.
func (c *Client) DeleteUploadTask(ctx context.Context, taskIDs []string) (*Response[DeletedUploadTasks], error) {
	if err := validateIDs(taskIDs, 0); err != nil {
		return nil, err
	}
	body := struct {
		TaskIDs []string `json:"taskIds"`
	}{taskIDs}
	return call[DeletedUploadTasks](ctx, c, http.MethodPost, fileV1+"delete_upload_task", nil, body, false, false)
}

func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateResource(res UploadResource) error {
	if res.MD5 != "" && !validHex(res.MD5, 32) {
		return invalid("MD5", "must be 32 hexadecimal characters")
	}
	if res.GCID != "" && !validHex(res.GCID, 40) {
		return invalid("GCID", "must be 40 hexadecimal characters")
	}
	return nil
}

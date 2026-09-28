package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type Credentials struct {
	AccessKeyID     string `json:"accessKeyID"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken"`
}
type UploadToken struct {
	TaskID       string          `json:"taskId"`
	ObjectPath   string          `json:"objectPath"`
	Provider     json.RawMessage `json:"provider"`
	Region       string          `json:"region"`
	BucketName   string          `json:"bucketName"`
	Endpoint     string          `json:"endPoint"`
	FullEndpoint string          `json:"fullEndPoint"`
	CallbackVar  string          `json:"callbackVar"`
	Credentials
	NestedCredentials Credentials `json:"creds"`
}

// GetUploadToken uses WebAPI capacity=2, not OpenAPI capacity=30. Code 156 is
// returned as successful completion, with the taskId retained for polling.
func (c *Client) GetUploadToken(ctx context.Context, parentID, name string, size int64) (*Response[UploadToken], error) {
	if e := nameValid(name); e != nil {
		return nil, e
	}
	if size < 0 {
		return nil, errors.New("webapi: negative file size")
	}
	r, e := post[UploadToken](ctx, c, resourcePath+"get_res_center_token", map[string]any{"capacity": 2, "parentId": parentID, "name": name, "res": map[string]int64{"fileSize": size}}, true, false)
	if e != nil {
		return r, e
	}
	if r.Data.TaskID == "" {
		return r, ErrInvalidResponse
	}
	d := &r.Data
	if d.AccessKeyID == "" {
		d.AccessKeyID = d.NestedCredentials.AccessKeyID
	}
	if d.SecretAccessKey == "" {
		d.SecretAccessKey = d.NestedCredentials.SecretAccessKey
	}
	if d.SessionToken == "" {
		d.SessionToken = d.NestedCredentials.SessionToken
	}
	if d.Endpoint == "" {
		d.Endpoint = d.FullEndpoint
	}
	return r, nil
}
func (c *Client) GetUploadInfo(ctx context.Context, id string) (*Response[File], error) {
	if e := required(id); e != nil {
		return nil, e
	}
	return post[File](ctx, c, resourcePath+"file/get_info_by_task_id", Task{id}, true, false)
}

// WaitUpload accepts AList's observed transient codes 145/146/147/155/163.
// The default five-minute bound prevents waiting forever on an expired task.
func (c *Client) WaitUpload(ctx context.Context, id string) (*Response[File], error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		r, e := c.GetUploadInfo(ctx, id)
		if e == nil && r.Data.FileID != "" {
			return r, nil
		}
		if e != nil && !IsCode(e, 145) && !IsCode(e, 146) && !IsCode(e, 147) && !IsCode(e, 155) && !IsCode(e, 163) {
			return r, e
		}
		if e := wait(ctx, time.Second); e != nil {
			return r, e
		}
	}
}

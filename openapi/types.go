package openapi

// Ptr helps specify optional numeric filters, including explicit zero values.
func Ptr[T any](value T) *T { return &value }

type OrderBy int

const (
	OrderByName OrderBy = iota
	OrderBySize
	OrderByCreatedAt
	OrderByUpdatedAt
	OrderByExtension
)

type SortType int

const (
	SortAscending SortType = iota
	SortDescending
)

type FileType int

const (
	FileTypeUnknown FileType = iota
	FileTypeImage
	FileTypeVideo
	FileTypeAudio
	FileTypeDocument
	FileTypeArchive
	FileTypeSubtitle
	FileTypeFont
	FileTypeInstaller
	FileTypeTorrent
	FileTypeCode
)

type ResourceType int

const (
	ResourceUnknown ResourceType = iota
	ResourceFile
	ResourceDirectory
)

type VIPStatus int

const (
	VIPInactive VIPStatus = 1
	VIPActive   VIPStatus = 2
	VIPExpired  VIPStatus = 3
)

type TaskStatus int

const (
	TaskPending TaskStatus = iota
	TaskRunning
	TaskCompleted
	TaskFailed
)

type File struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
	BizID    string `json:"bizId"`
	ParentID string `json:"parentId"`
	Depth    int    `json:"depth"`
	// The API spells this field mineType, not mimeType.
	MIMEType      string       `json:"mineType"`
	FileType      FileType     `json:"fileType"`
	ResType       ResourceType `json:"resType"`
	Extension     string       `json:"ext"`
	FullParentIDs string       `json:"fullParentIds"`
	CreatedAt     int64        `json:"ctime"`
	UpdatedAt     int64        `json:"utime"`
	Thumbnail     string       `json:"thumbnail"`
}

type FileList struct {
	Total int64  `json:"total"`
	List  []File `json:"list"`
}

type FileDetail struct {
	FileInfo      File            `json:"fileInfo"`
	VideoResource []VideoResource `json:"videoResource"`
	SizeInfo      *DirectorySize  `json:"sizeInfo,omitempty"`
}

type DirectorySize struct {
	Size         int64 `json:"size"`
	SubDirCount  int64 `json:"subDirCount"`
	SubFileCount int64 `json:"subFileCount"`
}

type VideoResource struct {
	Info  VideoInfo `json:"info"`
	BizID string    `json:"bizId"`
}

type Resolution struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type VideoInfo struct {
	Resolution        Resolution `json:"resolution"`
	Duration          int64      `json:"duration"`
	BitRate           int64      `json:"bitRate"`
	FrameRate         int        `json:"frameRate"`
	VideoCodec        string     `json:"videoCodec"`
	AudioCodec        string     `json:"audioCodec"`
	VideoType         string     `json:"videoType"`
	DefaultResolution bool       `json:"defaultResolution"`
	ResolutionName    string     `json:"resolutionName"`
}

type DownloadURL struct {
	SignedURL   string `json:"signedURL"`
	URLDuration int64  `json:"urlDuration"`
}

type VODDownloadURL struct {
	DownloadURL
	SpeedupSignature string `json:"speedupSignature"`
	RequestID        string `json:"requestId"`
}

type UserInfo struct {
	UserID         string    `json:"userId"`
	NickName       string    `json:"nickName"`
	Avatar         string    `json:"avatar"`
	VIPStatus      VIPStatus `json:"vipStatus"`
	VIPLeftSeconds int64     `json:"vipLeftSeconds"`
	TotalSpace     int64     `json:"totalSpace"`
	UsedSpace      int64     `json:"usedSpace"`
}

type Task struct {
	TaskID string `json:"taskId"`
}
type TaskState struct {
	Status TaskStatus `json:"status"`
}

type UploadResource struct {
	FileSize uint64 `json:"fileSize"`
	MD5      string `json:"md5,omitempty"`
	CID      string `json:"cid,omitempty"`
	GCID     string `json:"gcid,omitempty"`
}

type UploadObject struct {
	Provider   uint32 `json:"provider"`
	ObjectPath string `json:"objectPath"`
}

// Credentials contains temporary secrets. Do not log or persist this structure.
type Credentials struct {
	AccessKeyID     string `json:"accessKeyID"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken"`
	Expiration      string `json:"expiration"`
}

type UploadToken struct {
	GCID         string       `json:"gcid"`
	Provider     uint32       `json:"provider"`
	Credentials  *Credentials `json:"creds,omitempty"`
	Endpoint     string       `json:"endPoint"`
	FullEndpoint string       `json:"fullEndPoint"`
	BucketName   string       `json:"bucketName"`
	ObjectPath   string       `json:"objectPath"`
	Callback     string       `json:"callback"`
	CallbackVar  string       `json:"callbackVar"`
	Region       string       `json:"region"`
	TaskID       string       `json:"taskId"`
}

type FlashUploadResult struct {
	CanFlashUpload bool   `json:"canFlashUpload"`
	TaskID         string `json:"taskId"`
}

type DeletedUploadTasks struct {
	TaskIDs []string `json:"taskIds"`
}

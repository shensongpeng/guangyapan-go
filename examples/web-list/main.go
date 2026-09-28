package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/shensongpeng/guangyapan-go/webapi"
)

func main() {
	c, e := webapi.NewClient(webapi.Config{AccessToken: os.Getenv("GUANGYAPAN_WEB_ACCESS_TOKEN"), DeviceID: os.Getenv("GUANGYAPAN_WEB_DEVICE_ID")})
	if e != nil {
		log.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r, e := c.GetFileList(ctx, webapi.FileListRequest{PageSize: 20, OrderBy: 3, SortType: 1})
	if e != nil {
		log.Fatal(e)
	}
	for _, f := range r.Data.List {
		fmt.Printf("%s\t%s\t%d bytes\n", f.FileID, f.FileName, f.FileSize)
	}
}

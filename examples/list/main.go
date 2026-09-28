// This example only reads file metadata. It never writes to the drive.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/shensongpeng/guangyapan-go"
)

func main() {
	client, err := guangyapan.NewClient(guangyapan.Config{
		ClientID:    os.Getenv("GUANGYAPAN_CLIENT_ID"),
		AccessToken: os.Getenv("GUANGYAPAN_ACCESS_TOKEN"),
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := client.GetFileList(ctx, guangyapan.FileListRequest{PageSize: 20})
	if err != nil {
		log.Fatal(err)
	}
	for _, file := range result.Data.List {
		fmt.Printf("%s\t%s\t%d bytes\n", file.FileID, file.FileName, file.FileSize)
	}
}

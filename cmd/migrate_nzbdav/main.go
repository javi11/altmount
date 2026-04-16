package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"encoding/json"
)

// Simplified structures for nzbdav API
type WebDavItem struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type"` // 1 = folder, 0 = file
}

func main() {
	// CONFIGURATION
	apiBase := "http://localhost:8080" // Update to your nzbdav address
	targetDir := "/opt/altmount/config_test/.nzbs/migrated"
	os.MkdirAll(targetDir, 0755)

	// Get all items from the root
	fmt.Println("Fetching releases from nzbdav API...")
	items, err := fetchItems(apiBase, "/")
	if err != nil {
		panic(err)
	}

	for _, item := range items {
		if item.Type == 1 { // If it's a folder/release
			fmt.Printf("Fetching NZB for: %s\n", item.Name)
			err := downloadNzb(apiBase, item.Id, filepath.Join(targetDir, sanitizeFilename(item.Name)+".nzb"))
			if err != nil {
				fmt.Printf("Failed to download %s: %v\n", item.Name, err)
			}
		}
	}
}

func fetchItems(apiBase, path string) ([]WebDavItem, error) {
	resp, err := http.Get(fmt.Sprintf("%s/api/list?path=%s", apiBase, path))
	if err != nil { return nil, err }
	defer resp.Body.Close()
	
	var items []WebDavItem
	json.NewDecoder(resp.Body).Decode(&items)
	return items, nil
}

func downloadNzb(apiBase, id, outputPath string) error {
	// Adjust endpoint based on nzbdav API design
	resp, err := http.Get(fmt.Sprintf("%s/api/download?id=%s", apiBase, id))
	if err != nil { return err }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", resp.StatusCode)
	}

	out, _ := os.Create(outputPath)
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func sanitizeFilename(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}

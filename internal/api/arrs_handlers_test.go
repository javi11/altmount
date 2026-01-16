package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestArrsWebhookRequest_Unmarshal(t *testing.T) {
	t.Run("deletedFiles is false", func(t *testing.T) {
		jsonData := `{
			"eventType": "Grab",
			"deletedFiles": false
		}`

		var req ArrsWebhookRequest
		err := json.Unmarshal([]byte(jsonData), &req)
		assert.NoError(t, err)
		assert.Nil(t, req.DeletedFiles)
	})

	t.Run("deletedFiles is array", func(t *testing.T) {
		jsonData := `{
			"eventType": "Upgrade",
			"deletedFiles": [
				{"path": "/path/to/file1.mkv"},
				{"path": "/path/to/file2.mkv"}
			]
		}`

		var req ArrsWebhookRequest
		err := json.Unmarshal([]byte(jsonData), &req)
		assert.NoError(t, err)
		assert.Len(t, req.DeletedFiles, 2)
		assert.Equal(t, "/path/to/file1.mkv", req.DeletedFiles[0].Path)
	})

	t.Run("movieFile path is present", func(t *testing.T) {
		jsonData := `{
			"eventType": "Download",
			"movieFile": {
				"path": "/path/to/movie/file.mkv"
			}
		}`

		var req ArrsWebhookRequest
		err := json.Unmarshal([]byte(jsonData), &req)
		assert.NoError(t, err)
		assert.Equal(t, "/path/to/movie/file.mkv", req.MovieFile.Path)
	})

	t.Run("movie delete has folderPath", func(t *testing.T) {
		jsonData := `{
			"eventType": "MovieDelete",
			"movie": {
				"folderPath": "/path/to/movie/folder"
			}
		}`

		var req ArrsWebhookRequest
		err := json.Unmarshal([]byte(jsonData), &req)
		assert.NoError(t, err)
		assert.Equal(t, "/path/to/movie/folder", req.Movie.FolderPath)
	})
}

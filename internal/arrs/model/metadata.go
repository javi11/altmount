package model

type MovieMetadata struct {
	Id     int64 `json:"id,omitempty"`
	TmdbId int64 `json:"tmdbId,omitempty"`
}

type MovieFileMetadata struct {
	Id        int64  `json:"id,omitempty"`
	SceneName string `json:"sceneName,omitempty"`
}

type SeriesMetadata struct {
	Id     int64 `json:"id,omitempty"`
	TvdbId int64 `json:"tvdbId,omitempty"`
}

type EpisodeFileMetadata struct {
	Id        int64  `json:"id,omitempty"`
	SceneName string `json:"sceneName,omitempty"`
}

type WebhookMetadata struct {
	EventType    string               `json:"eventType,omitempty"`
	InstanceName string               `json:"instanceName,omitempty"`
	Movie        *MovieMetadata       `json:"movie,omitempty"`
	MovieFile    *MovieFileMetadata   `json:"movieFile,omitempty"`
	Series       *SeriesMetadata      `json:"series,omitempty"`
	EpisodeFile  *EpisodeFileMetadata `json:"episodeFile,omitempty"`
}

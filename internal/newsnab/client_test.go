package newsnab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewsnabClient_SearchMovie_JSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "movie", r.URL.Query().Get("t"))
		assert.Equal(t, "1234567", r.URL.Query().Get("imdbid"))
		assert.Equal(t, "testkey", r.URL.Query().Get("apikey"))
		assert.Equal(t, "Radarr/6.5.1.2032 (alpine 3.23.3)", r.Header.Get("User-Agent"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"channel": {
				"title": "NZB Indexer",
				"item": [
					{
						"title": "Dune.Part.Two.2024.2160p.UHD.Remux.DV.HDR10+.TrueHD.Atmos.7.1-FraMeSToR",
						"link": "https://indexer.test/getnzb/1.nzb",
						"pubDate": "Mon, 10 Jun 2024 12:00:00 +0000",
						"enclosure": {
							"_url": "https://indexer.test/getnzb/1.nzb",
							"_length": "58720257000",
							"_type": "application/x-nzb"
						},
						"attr": [
							{"_name": "category", "_value": "2045"}
						]
					}
				]
			}
		}`))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{
		ID:             "test_idx",
		Name:           "Test Indexer",
		URL:            ts.URL,
		APIKey:         "testkey",
		TimeoutSeconds: 2,
		Enabled:        true,
	}, ts.Client())

	results, err := client.SearchMovie(context.Background(), "tt1234567", nil, "Radarr/6.5.1.2032 (alpine 3.23.3)")
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Dune.Part.Two.2024.2160p.UHD.Remux.DV.HDR10+.TrueHD.Atmos.7.1-FraMeSToR", results[0].Title)
	assert.Equal(t, "https://indexer.test/getnzb/1.nzb", results[0].DownloadURL)
	assert.Equal(t, int64(58720257000), results[0].Size)
	assert.Equal(t, "Test Indexer", results[0].Indexer)
}

func TestNewsnabClient_SearchTV_XML(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "tvsearch", r.URL.Query().Get("t"))
		assert.Equal(t, "Arcane", r.URL.Query().Get("q"))
		assert.Equal(t, "2", r.URL.Query().Get("season"))
		assert.Equal(t, "1", r.URL.Query().Get("ep"))

		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
		<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
			<channel>
				<title>Test TV Indexer</title>
				<item>
					<title>Arcane.S02E01.2160p.HDR.DDP5.1.Atmos.H.265-FLUX</title>
					<link>https://indexer.test/getnzb/2.nzb</link>
					<pubDate>Mon, 10 Jun 2024 14:00:00 +0000</pubDate>
					<enclosure url="https://indexer.test/getnzb/2.nzb" length="2500000000" type="application/x-nzb" />
					<newznab:attr name="category" value="5045" />
					<newznab:attr name="size" value="2500000000" />
				</item>
			</channel>
		</rss>`))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{
		ID:             "test_idx",
		Name:           "Test TV Indexer",
		URL:            ts.URL,
		APIKey:         "testkey",
		TimeoutSeconds: 2,
		Enabled:        true,
	}, ts.Client())

	results, err := client.SearchTV(context.Background(), "", "", "Arcane", 2, 1, nil, "Sonarr/4.1.1.824 (alpine 3.23.3)")
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Arcane.S02E01.2160p.HDR.DDP5.1.Atmos.H.265-FLUX", results[0].Title)
	assert.Equal(t, int64(2500000000), results[0].Size)
}

func TestNewsnabClient_SearchGeneral_NZBGeek_JSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "search", r.URL.Query().Get("t"))
		assert.Equal(t, "Gladiator II", r.URL.Query().Get("q"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"@attributes": {"version": "1.0"},
			"channel": {
				"title": "api.nzbgeek.info",
				"item": [
					{
						"title": "Gladiator.II.2024.720p.AMZN.WEB-DL.DDP5.1.H.264-ViSTA",
						"guid": "https://nzbgeek.info/geekseek.php?guid=1fc90df05debd7d37615bc1638aa3389",
						"link": "https://api.nzbgeek.info/api?t=get&id=1fc90df05debd7d37615bc1638aa3389&apikey=testkey",
						"pubDate": "Thu, 13 Aug 2026 01:18:34 +0000",
						"enclosure": {
							"@attributes": {
								"url": "http://api.nzbgeek.info/api?t=get&id=1fc90df05debd7d37615bc1638aa3389&apikey=testkey",
								"length": "5471988000",
								"type": "application/x-nzb"
							}
						},
						"attr": [
							{"@attributes": {"name": "category", "value": "2000"}},
							{"@attributes": {"name": "size", "value": "5471988000"}}
						]
					}
				]
			}
		}`))
	}))
	defer ts.Close()

	client := NewClient(IndexerConfig{
		ID:             "nzbgeek",
		Name:           "nzbgeek",
		URL:            ts.URL,
		APIKey:         "testkey",
		TimeoutSeconds: 2,
		Enabled:        true,
	}, ts.Client())

	results, err := client.SearchGeneral(context.Background(), "Gladiator II", []int{2000, 2010, 2030}, "Altmount/1.0")
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Gladiator.II.2024.720p.AMZN.WEB-DL.DDP5.1.H.264-ViSTA", results[0].Title)
	assert.Equal(t, int64(5471988000), results[0].Size)
	assert.Equal(t, "http://api.nzbgeek.info/api?t=get&id=1fc90df05debd7d37615bc1638aa3389&apikey=testkey", results[0].DownloadURL)
}

package java

import (
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestBuildJFRIngestProfile(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	endTime := time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC)
	jfrData := []byte("fake JFR binary data")
	lbls := labels.FromMap(map[string]string{
		"service_name": "test-service",
		"env":          "production",
	})

	t.Run("complete options", func(t *testing.T) {
		opts := JFRIngestOptions{
			StartTime:   startTime,
			EndTime:     endTime,
			SampleRate:  100,
			SpyName:     "alloy.java",
			Units:       "samples",
			Aggregation: "sum",
		}

		profile, err := buildJFRIngestProfile(jfrData, lbls, opts)
		require.NoError(t, err)
		require.NotNil(t, profile)

		// Verify labels
		require.Equal(t, lbls, profile.Labels)

		// Verify URL path and query parameters
		require.Equal(t, "/ingest", profile.URL.Path)
		query := profile.URL.Query()
		require.Equal(t, "jfr", query.Get("format"))
		require.Equal(t, "samples", query.Get("units"))
		require.Equal(t, "sum", query.Get("aggregationType"))
		require.Equal(t, "100", query.Get("sampleRate"))
		require.Equal(t, "1704110400", query.Get("from"))  // startTime.Unix()
		require.Equal(t, "1704110460", query.Get("until")) // endTime.Unix()
		require.Equal(t, "alloy.java", query.Get("spyName"))

		// Verify Content-Type header contains multipart/form-data
		require.Len(t, profile.ContentType, 1)
		require.True(t, strings.HasPrefix(profile.ContentType[0], "multipart/form-data; boundary="))

		// Verify multipart body structure
		mediaType, params, err := mime.ParseMediaType(profile.ContentType[0])
		require.NoError(t, err)
		require.Equal(t, "multipart/form-data", mediaType)

		reader := multipart.NewReader(strings.NewReader(string(profile.RawBody)), params["boundary"])
		part, err := reader.NextPart()
		require.NoError(t, err)
		require.Equal(t, "jfr", part.FormName())
		require.Equal(t, "profile.jfr", part.FileName())

		bodyData, err := io.ReadAll(part)
		require.NoError(t, err)
		require.Equal(t, jfrData, bodyData)

		// Verify no more parts
		_, err = reader.NextPart()
		require.Equal(t, io.EOF, err)
	})

	t.Run("default units and aggregation", func(t *testing.T) {
		opts := JFRIngestOptions{
			StartTime:  startTime,
			EndTime:    endTime,
			SampleRate: 200,
			SpyName:    "test-spy",
			// Units and Aggregation not set - should get defaults
		}

		profile, err := buildJFRIngestProfile(jfrData, lbls, opts)
		require.NoError(t, err)
		require.NotNil(t, profile)

		query := profile.URL.Query()
		require.Equal(t, "samples", query.Get("units"))
		require.Equal(t, "sum", query.Get("aggregationType"))
	})

	t.Run("empty JFR data", func(t *testing.T) {
		opts := JFRIngestOptions{
			StartTime:  startTime,
			EndTime:    endTime,
			SampleRate: 100,
			SpyName:    "alloy.java",
		}

		profile, err := buildJFRIngestProfile([]byte{}, lbls, opts)
		require.NoError(t, err)
		require.NotNil(t, profile)

		// Verify empty data is properly encoded in multipart structure
		_, params, err := mime.ParseMediaType(profile.ContentType[0])
		require.NoError(t, err)
		reader := multipart.NewReader(strings.NewReader(string(profile.RawBody)), params["boundary"])
		part, err := reader.NextPart()
		require.NoError(t, err)

		bodyData, err := io.ReadAll(part)
		require.NoError(t, err)
		require.Empty(t, bodyData)
	})
}

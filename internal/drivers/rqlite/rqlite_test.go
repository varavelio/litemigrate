package rqlite

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDriverExec(t *testing.T) {
	t.Run("sends transactional execute requests with auth and headers", func(t *testing.T) {
		var (
			requestPath   string
			requestQuery  string
			requestBody   []string
			authorization string
			customHeader  string
			contentType   string
			requestMethod string
		)

		server := httptest.NewServer(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer func() {
					_ = request.Body.Close()
				}()

				requestPath = request.URL.Path
				requestQuery = request.URL.RawQuery
				authorization = request.Header.Get("Authorization")
				customHeader = request.Header.Get("X-Test")
				contentType = request.Header.Get("Content-Type")
				requestMethod = request.Method

				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				err = json.Unmarshal(body, &requestBody)
				require.NoError(t, err)

				writer.Header().Set("Content-Type", "application/json")
				_, err = writer.Write([]byte(`{"results":[{"rows_affected":1}]}`))
				require.NoError(t, err)
			}),
		)
		defer server.Close()

		driver, err := New(Config{
			URL:      server.URL,
			Timeout:  time.Second,
			Username: "alice",
			Password: "secret",
			Headers:  map[string]string{"X-Test": "value"},
		})
		require.NoError(t, err)

		err = driver.Exec(context.Background(), []string{
			"CREATE TABLE users (id INTEGER PRIMARY KEY)",
			"CREATE INDEX idx_users_id ON users(id)",
		}, true)

		require.NoError(t, err)
		require.Equal(t, http.MethodPost, requestMethod)
		require.Equal(t, "/db/execute", requestPath)
		require.Contains(t, requestQuery, "transaction")
		require.Contains(t, requestQuery, "timeout=1s")
		require.Equal(t, "Basic YWxpY2U6c2VjcmV0", authorization)
		require.Equal(t, "value", customHeader)
		require.Equal(t, "application/json", contentType)
		require.Equal(t, []string{
			"CREATE TABLE users (id INTEGER PRIMARY KEY)",
			"CREATE INDEX idx_users_id ON users(id)",
		}, requestBody)
	})

	t.Run("surfaces database-level execute errors", func(t *testing.T) {
		server := httptest.NewServer(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, err := writer.Write(
					[]byte(`{"results":[{"error":"near \"BAD\": syntax error"}]}`),
				)
				require.NoError(t, err)
			}),
		)
		defer server.Close()

		driver, err := New(Config{URL: server.URL, Timeout: time.Second})
		require.NoError(t, err)

		err = driver.Exec(context.Background(), []string{"BAD SQL"}, false)

		require.Error(t, err)
		require.ErrorContains(t, err, `near "BAD": syntax error`)
	})
}

func TestDriverQuery(t *testing.T) {
	t.Run("uses the associative query endpoint and decodes rows", func(t *testing.T) {
		var requestBody []string

		server := httptest.NewServer(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer func() {
					_ = request.Body.Close()
				}()

				require.Equal(t, "/db/query", request.URL.Path)
				require.Contains(t, request.URL.RawQuery, "associative")
				require.Contains(t, request.URL.RawQuery, "timeout=2s")

				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				err = json.Unmarshal(body, &requestBody)
				require.NoError(t, err)

				writer.Header().Set("Content-Type", "application/json")
				_, err = writer.Write([]byte(`
{"results":[{"rows":[{"version":"20260501143015","name":"20260501143015_create_users.sql","applied_at":"2026-05-01T14:30:15Z"}]}]}
`))
				require.NoError(t, err)
			}),
		)
		defer server.Close()

		driver, err := New(Config{URL: server.URL, Timeout: 2 * time.Second})
		require.NoError(t, err)

		rows, err := driver.Query(
			context.Background(),
			"SELECT version, name, applied_at FROM _litemigrate_migrations",
		)

		require.NoError(t, err)
		require.Equal(
			t,
			[]string{"SELECT version, name, applied_at FROM _litemigrate_migrations"},
			requestBody,
		)
		require.Len(t, rows, 1)
		require.Equal(t, "20260501143015", rows[0]["version"])
		require.Equal(t, "20260501143015_create_users.sql", rows[0]["name"])
	})

	t.Run("surfaces non-success HTTP status codes", func(t *testing.T) {
		server := httptest.NewServer(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				http.Error(writer, "boom", http.StatusBadGateway)
			}),
		)
		defer server.Close()

		driver, err := New(Config{URL: server.URL, Timeout: time.Second})
		require.NoError(t, err)

		_, err = driver.Query(context.Background(), "SELECT 1")

		require.Error(t, err)
		require.ErrorContains(t, err, "unexpected HTTP status 502")
	})
}

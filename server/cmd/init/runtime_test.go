package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWithInstallerAssetsMountsOnlyInstallAndDelegatesTheAPI(t *testing.T) {
	t.Parallel()

	assets := fstest.MapFS{
		"install/index.html": &fstest.MapFile{Data: []byte("<h1>temporary installer</h1>")},
		"install/app.js":     &fstest.MapFile{Data: []byte("console.log('temporary installer')")},
	}
	base := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusAccepted)
		_, _ = response.Write([]byte("base:" + request.URL.Path))
	})
	handler := withInstallerAssets(base, assets)

	for _, testCase := range []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{path: "/install", wantStatus: http.StatusOK, wantBody: "temporary installer"},
		{path: "/install/app.js", wantStatus: http.StatusOK, wantBody: "console.log"},
		{path: "/api/system/install/v1/status", wantStatus: http.StatusAccepted, wantBody: "base:/api/system/install/v1/status"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, testCase.path, nil))
		if response.Code != testCase.wantStatus || !strings.Contains(response.Body.String(), testCase.wantBody) {
			t.Fatalf("GET %s = %d %q", testCase.path, response.Code, response.Body.String())
		}
	}
}

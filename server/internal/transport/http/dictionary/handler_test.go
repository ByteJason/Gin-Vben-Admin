package dictionaryhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dictionaryapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/dictionary"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/gin-gonic/gin"
)

func TestDictionaryHandlerListsLocalizedItemsAndProtectsSystemRows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := dictionaryapp.NewMemoryRepository()
	repo.SeedType(dictionaryapp.DictionaryType{ID: "system", Code: "common.status", SystemOwned: true})
	repo.SeedItem(dictionaryapp.DictionaryItem{ID: "system-active", TypeCode: "common.status", Value: "active", LabelZhCN: "启用", LabelEnUS: "Active", SystemOwned: true, Status: "active"})
	service := dictionaryapp.NewService(repo, nil)
	r := gin.New()
	RegisterRoutes(r, NewHandler(service))
	scope, err := tenant.NewContext("tenant-a", "", false)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/admin/v1/dictionaries/common.status/items", nil)
	request.Header.Set("Accept-Language", "en-US")
	request = request.WithContext(tenant.WithContext(context.Background(), scope))
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"label":"Active"`) {
		t.Fatalf("localized list status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/admin/v1/dictionaries/types/common.status", nil)
	request = request.WithContext(tenant.WithContext(context.Background(), scope))
	response = httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("system delete status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDictionaryHandlerImportsAtomically(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := dictionaryapp.NewMemoryRepository()
	repo.SeedType(dictionaryapp.DictionaryType{ID: "system", Code: "common.status", SystemOwned: true})
	service := dictionaryapp.NewService(repo, nil)
	r := gin.New()
	RegisterRoutes(r, NewHandler(service))
	scope, _ := tenant.NewContext("tenant-a", "", false)
	request := httptest.NewRequest(http.MethodPost, "/api/admin/v1/dictionaries/common.status/items/import", strings.NewReader(`{"items":[{"value":"pending","labelZhCN":"待处理"},{"value":"","labelZhCN":"坏数据"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(tenant.WithContext(context.Background(), scope))
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("import status=%d body=%s", response.Code, response.Body.String())
	}
	items, err := service.ListItems(tenant.WithContext(context.Background(), scope), "common.status", dictionaryapp.ListOptions{})
	if err != nil || len(items) != 0 {
		t.Fatalf("failed import wrote data: items=%+v err=%v", items, err)
	}
}

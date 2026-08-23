// Package importexport exposes the tenant-scoped IMPORT-100 HTTP seam.
package importexport

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	importsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/imports"
	authdomain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const basePath = "/api/admin/v1/import-export"

type Handler struct{ service *importsapp.Service }

func NewHandler(service *importsapp.Service) *Handler { return &Handler{service: service} }

func RegisterRoutes(r gin.IRouter, handler *Handler) { registerRoutes(r.Group(basePath), handler) }

func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	registerRoutes(group.Group("/import-export"), handler)
}

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		for _, method := range []string{"GET", "POST"} {
			group.Handle(method, "/*path", disabled)
		}
		return
	}
	group.GET("/templates/:format", handler.template)
	group.POST("/imports/preview", handler.preview)
	group.POST("/imports/commit", handler.commit)
	group.POST("/exports", handler.startExport)
	group.GET("/jobs", handler.list)
	group.GET("/jobs/:id", handler.get)
	group.GET("/jobs/:id/errors", handler.errors)
	group.GET("/jobs/:id/download", handler.download)
	group.POST("/jobs/:id/cancel", handler.cancel)
	group.POST("/jobs/:id/retry", handler.retry)
}

type commitInput struct {
	PreviewID      string `json:"previewId"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type exportInput struct {
	IdempotencyKey string              `json:"idempotencyKey"`
	Fields         []string            `json:"fields"`
	Allowlist      map[string]bool     `json:"allowlist"`
	Rows           []map[string]string `json:"rows"`
	RedactFields   []string            `json:"redactFields,omitempty"`
}

func (h *Handler) template(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	format := strings.ToLower(strings.TrimSpace(c.Param("format")))
	if format != "csv" && format != "xlsx" {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid template format", "import.format.invalid", nil)
		return
	}
	data, contentType, filename := templateData(format)
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Data(http.StatusOK, contentType, data)
}

func (h *Handler) preview(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	if err := c.Request.ParseMultipartForm(50 << 20); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid import upload", "import.upload.invalid", nil)
		return
	}
	header, err := c.FormFile("file")
	if err != nil || header == nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "file is required", "import.file.required", nil)
		return
	}
	reader, err := header.Open()
	if err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "file is unreadable", "import.file.unreadable", nil)
		return
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, (50<<20)+1))
	if err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "file is unreadable", "import.file.unreadable", nil)
		return
	}
	if int64(len(data)) > 50<<20 {
		response.ErrorWithMessageKey(c, http.StatusRequestEntityTooLarge, 10000, "file is too large", "import.file.tooLarge", nil)
		return
	}
	request := importsapp.Request{TenantID: scope.TenantID, OrgID: scope.Organization, ActorID: actorID(c), Format: c.PostForm("format"), Name: header.Filename, MIME: header.Header.Get("Content-Type"), Columns: splitCSV(c.PostForm("columns")), Required: splitCSV(c.PostForm("required")), Allowlist: parseAllowlist(c.PostForm("allowlist")), Types: parseTypes(c.PostForm("types")), Data: data}
	result, err := h.service.PreviewContext(c.Request.Context(), request)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (h *Handler) commit(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	var input commitInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid import commit", "import.commit.invalid", nil)
		return
	}
	job, err := h.service.Commit(c.Request.Context(), importsapp.CommitRequest{TenantID: scope.TenantID, OrgID: scope.Organization, ActorID: actorID(c), PreviewID: input.PreviewID, IdempotencyKey: input.IdempotencyKey})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Write(c, http.StatusAccepted, 0, "accepted", job)
}

func (h *Handler) startExport(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	var input exportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid export request", "export.request.invalid", nil)
		return
	}
	job, err := h.service.StartExport(c.Request.Context(), importsapp.ExportRequest{TenantID: scope.TenantID, OrgID: scope.Organization, ActorID: actorID(c), IdempotencyKey: input.IdempotencyKey, Fields: input.Fields, Allowlist: input.Allowlist, Rows: input.Rows})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Write(c, http.StatusAccepted, 0, "accepted", job)
}

func (h *Handler) list(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	items, err := h.service.List(c.Request.Context(), strings.TrimSpace(c.Query("kind")))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) get(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	job, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, job)
}

func (h *Handler) errors(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	if strings.EqualFold(c.Query("format"), "csv") || strings.Contains(c.GetHeader("Accept"), "text/csv") {
		data, err := h.service.ErrorCSV(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename="import-errors.csv"`)
		c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
		return
	}
	items, err := h.service.ErrorRows(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) download(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	data, err := h.service.Artifact(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	if len(data) == 0 {
		response.ErrorWithMessageKey(c, http.StatusNotFound, 10001, "download is not ready", "export.download.notReady", nil)
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="export.csv"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

func (h *Handler) cancel(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	job, err := h.service.Cancel(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, job)
}

func (h *Handler) retry(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	job, err := h.service.Retry(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, job)
}

func requestScope(c *gin.Context) (tenant.Context, bool) {
	scope, err := tenant.RequireContext(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid tenant context")
		return tenant.Context{}, false
	}
	return scope, true
}

func actorID(c *gin.Context) string {
	if value, ok := c.Get("auth_claims"); ok {
		if claims, ok := value.(authdomain.Claims); ok {
			return strings.TrimSpace(claims.Subject)
		}
	}
	return strings.TrimSpace(c.GetHeader("X-Actor-ID"))
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func parseAllowlist(value string) map[string]bool {
	out := map[string]bool{}
	for _, field := range splitCSV(value) {
		out[field] = true
	}
	return out
}

func parseTypes(value string) map[string]string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var out map[string]string
	if json.Unmarshal([]byte(value), &out) != nil {
		return nil
	}
	return out
}

func templateData(format string) ([]byte, string, string) {
	if format == "xlsx" {
		return xlsxTemplate(), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "import-template.xlsx"
	}
	return []byte("name,email\n"), "text/csv; charset=utf-8", "import-template.csv"
}

func xlsxTemplate() []byte {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	files := map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels":                `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Import" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>name</t></is></c><c r="B1" t="inlineStr"><is><t>email</t></is></c></row></sheetData></worksheet>`,
	}
	for name, content := range files {
		writer, err := archive.Create(name)
		if err != nil {
			return []byte("name,email\n")
		}
		_, _ = writer.Write([]byte(content))
	}
	if err := archive.Close(); err != nil {
		return []byte("name,email\n")
	}
	return buffer.Bytes()
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, importsapp.ErrJobNotFound), errors.Is(err, importsapp.ErrPreviewNotFound):
		response.Error(c, http.StatusNotFound, 10001, "import/export job not found")
	case errors.Is(err, importsapp.ErrFileTooLarge), errors.Is(err, importsapp.ErrTooManyRows):
		response.Error(c, http.StatusRequestEntityTooLarge, 10000, "import file exceeds limit")
	case errors.Is(err, importsapp.ErrColumnDenied), errors.Is(err, importsapp.ErrVirusDetected):
		response.ErrorWithMessageKey(c, http.StatusForbidden, 30000, "import security policy rejected the file", "import.policy.rejected", nil)
	case errors.Is(err, importsapp.ErrJobStateConflict):
		response.Error(c, http.StatusConflict, 10010, "import/export job state conflict")
	case errors.Is(err, importsapp.ErrInvalidFormat), errors.Is(err, importsapp.ErrInvalidRequest):
		response.Error(c, http.StatusBadRequest, 10000, "invalid import/export request")
	default:
		response.Error(c, http.StatusServiceUnavailable, 40001, "import/export dependency unavailable")
	}
}

func disabled(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 40001, "import/export capability unavailable")
}

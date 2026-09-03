package auth

import (
	"context"

	appnotification "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
)

// RequestMetadata carries bounded, non-secret request attributes through the
// application layer.  HTTP adapters populate it; use cases remain transport
// agnostic and can persist the same values to session/audit ports.
type RequestMetadata struct {
	RequestID string
	// TraceID links application events across the HTTP request, jobs and
	// provider adapters. It is metadata only; it must never be used as an
	// authorization credential.
	TraceID string
	// CallerKey is the trusted, process-level public-capability caller key.
	// HTTP payload/header values are deliberately ignored by application
	// services; bootstrap code installs this value after resolving the caller.
	CallerKey string
	// Locale is the preferred message/template locale for this request.
	Locale string
	// PrincipalID identifies the authenticated actor for audit and media ACL
	// decisions. A blank value means the caller is acting as a system job.
	PrincipalID   string
	DeviceID      string
	DeviceName    string
	JSFingerprint string
	IPAddress     string
	UserAgent     string
}

type requestMetadataKey struct{}

// WithRequestMetadata associates request metadata with ctx.  A copy is stored
// so callers cannot mutate values after the use case begins.
func WithRequestMetadata(ctx context.Context, metadata RequestMetadata) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestMetadataKey{}, normalizeMetadata(metadata))
}

// RequestMetadataFromContext returns the metadata attached by
// WithRequestMetadata, or a zero value for contexts created outside a
// transport adapter.
func RequestMetadataFromContext(ctx context.Context) RequestMetadata {
	if ctx == nil {
		return RequestMetadata{}
	}
	metadata, _ := ctx.Value(requestMetadataKey{}).(RequestMetadata)
	return normalizeMetadata(metadata)
}

// CallerKeyFromContext returns the trusted public-capability caller key, if
// one was installed by bootstrap or another authenticated application
// boundary. It intentionally does not inspect request headers or body data.
func CallerKeyFromContext(ctx context.Context) string {
	return RequestMetadataFromContext(ctx).CallerKey
}

// LocaleFromContext returns the normalized preferred locale for this request.
// Locale resolution and fallback are owned by the notification application
// service; this helper only reads trusted request metadata.
func LocaleFromContext(ctx context.Context) string {
	return RequestMetadataFromContext(ctx).Locale
}

// TraceIDFromContext returns the bounded trace correlation id.
func TraceIDFromContext(ctx context.Context) string {
	return RequestMetadataFromContext(ctx).TraceID
}

// PrincipalIDFromContext returns the bounded authenticated principal id.
func PrincipalIDFromContext(ctx context.Context) string {
	return RequestMetadataFromContext(ctx).PrincipalID
}

// WithCapabilityMetadata is a convenience for internal callers that already
// have a request metadata value and need to install the trusted capability
// fields without replacing device/network metadata.
func WithCapabilityMetadata(ctx context.Context, callerKey, locale, traceID, principalID string) context.Context {
	metadata := RequestMetadataFromContext(ctx)
	metadata.CallerKey = callerKey
	metadata.Locale = locale
	metadata.TraceID = traceID
	metadata.PrincipalID = principalID
	ctx = WithRequestMetadata(ctx, metadata)
	return appnotification.WithContextMetadata(ctx, appnotification.ContextMetadata{
		CallerKey: callerKey, Locale: locale, TraceID: traceID, PrincipalID: principalID,
	})
}

func normalizeMetadata(metadata RequestMetadata) RequestMetadata {
	metadata.RequestID = bounded(metadata.RequestID, 128)
	metadata.TraceID = bounded(metadata.TraceID, 128)
	metadata.CallerKey = bounded(metadata.CallerKey, 128)
	metadata.Locale = bounded(metadata.Locale, 32)
	metadata.PrincipalID = bounded(metadata.PrincipalID, 128)
	metadata.DeviceID = bounded(metadata.DeviceID, 128)
	metadata.DeviceName = bounded(metadata.DeviceName, 128)
	metadata.JSFingerprint = bounded(metadata.JSFingerprint, 256)
	metadata.IPAddress = bounded(metadata.IPAddress, 128)
	metadata.UserAgent = bounded(metadata.UserAgent, 512)
	return metadata
}

func bounded(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

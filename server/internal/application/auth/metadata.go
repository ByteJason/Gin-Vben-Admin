package auth

import "context"

// RequestMetadata carries bounded, non-secret request attributes through the
// application layer.  HTTP adapters populate it; use cases remain transport
// agnostic and can persist the same values to session/audit ports.
type RequestMetadata struct {
	RequestID     string
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

func normalizeMetadata(metadata RequestMetadata) RequestMetadata {
	metadata.RequestID = bounded(metadata.RequestID, 128)
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

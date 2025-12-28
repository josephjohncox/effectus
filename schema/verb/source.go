package verb

// SourceInfo describes where a verb executor comes from.
type SourceInfo struct {
	Type   string `json:"type"`
	Ref    string `json:"ref,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// SourceProvider exposes source metadata for an executor.
type SourceProvider interface {
	SourceInfo() SourceInfo
}

const (
	SourceUnknown  = "unknown"
	SourceInternal = "internal"
	SourcePlugin   = "plugin"
	SourceHTTP     = "http"
	SourceGRPC     = "grpc"
	SourceStream   = "stream"
	SourceOCI      = "oci"
	SourceMock     = "mock"
	SourceNoop     = "noop"
)

package proxy

import (
	"mime"
	"strings"
)

type Classification struct {
	IsJSON   bool
	IsText   bool
	IsBinary bool
	IsStream bool
}

func ClassifyContent(contentType string) Classification {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	mediaType = strings.ToLower(mediaType)

	isJSON := mediaType == "application/json" ||
		strings.HasSuffix(mediaType, "+json") ||
		strings.Contains(mediaType, "json")
	isStream := mediaType == "text/event-stream" ||
		strings.Contains(mediaType, "stream")
	isText := strings.HasPrefix(mediaType, "text/") ||
		isJSON ||
		strings.Contains(mediaType, "xml") ||
		strings.Contains(mediaType, "javascript") ||
		mediaType == "application/x-www-form-urlencoded"
	isBinary := strings.HasPrefix(mediaType, "image/") ||
		strings.HasPrefix(mediaType, "audio/") ||
		strings.HasPrefix(mediaType, "video/") ||
		strings.HasPrefix(mediaType, "font/") ||
		mediaType == "application/octet-stream" ||
		mediaType == "application/pdf" ||
		strings.Contains(mediaType, "zip") ||
		strings.Contains(mediaType, "protobuf")

	return Classification{
		IsJSON:   isJSON,
		IsText:   isText,
		IsBinary: isBinary,
		IsStream: isStream,
	}
}

func CapturePlan(captureEnabled bool, class Classification, allowBinary bool, allowStream bool) (bool, string) {
	if !captureEnabled {
		return false, "capture disabled"
	}
	if class.IsStream && !allowStream {
		if class.IsText || class.IsJSON {
			return true, ""
		}
		return false, "stream body omitted"
	}
	if class.IsBinary && !allowBinary {
		return false, "binary body omitted"
	}
	return true, ""
}

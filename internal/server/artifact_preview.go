package server

import _ "embed"

// OCTO-FORK: inject a read-only sizing report into served previews, never the file.
//
//go:embed artifact_preview.js
var artifactPreviewScript string

var artifactPreviewBridge = []byte("<script>" + artifactPreviewScript + "</script>")

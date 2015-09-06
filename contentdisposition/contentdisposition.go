// Package contentdisposition attempts to correctly implement the confusing
// Content-Disposition header for specifying alternate filenames.
package contentdisposition

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// SetFilename adds a Content-Disposition header to the response to set an
// alternate filename. It probably has proper formatting and non-ASCII
// character support, as described by RFC 2616 and RFC 5987.
func SetFilename(w http.ResponseWriter, filename string) {
	encoded := (&url.URL{Path: filename}).String()
	// RFC2616 §2.2 - syntax of quoted strings
	escaped := strings.Replace(filename, `\`, `\\`, -1)
	escaped = strings.Replace(escaped, `"`, `\"`, -1)
	// RFC5987 §3.2.1 - syntax of regular and extended header value encoding
	disposition := fmt.Sprintf(`filename="%s"; filename*=UTF-8''%s`, escaped, encoded)
	w.Header().Set("Content-Disposition", disposition)
}

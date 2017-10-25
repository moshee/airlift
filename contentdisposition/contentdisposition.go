// Package contentdisposition attempts to correctly implement the confusing
// Content-Disposition header for specifying alternate filenames.
package contentdisposition

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func makeHeader(filename string) string {
	// For some reason Go doesn't provide access to the internal percent
	// encoding routines, meaning we have to do this to get a fully
	// percent-encoded string including spaces as %20.
	// Chrome interprets an unescaped comma in the UTF-8 filename as a header
	// value separator and simply refuses to render the contents, so it all
	// needs to be percent encoded.
	encoded := url.QueryEscape(filename)
	encoded = strings.Replace(encoded, "+", "%20", -1)
	// RFC2616 §2.2 - syntax of quoted strings
	escaped := strings.Replace(filename, `\`, `\\`, -1)
	escaped = strings.Replace(escaped, `"`, `\"`, -1)
	// RFC5987 §3.2.1 - syntax of regular and extended header value encoding
	disposition := fmt.Sprintf(`filename="%s"; filename*=UTF-8''%s`, escaped, encoded)
	return disposition
}

// SetFilename adds a Content-Disposition header to the response to set an
// alternate filename. It probably has proper formatting and non-ASCII
// character support, as described by RFC 2616 and RFC 5987.
func SetFilename(w http.ResponseWriter, filename string) {
	disposition := makeHeader(filename)
	w.Header().Set("Content-Disposition", disposition)
}

// SetAttachment is like SetFilename but it makes the browser treat the content
// as an attachment and present a save dialog.
func SetAttachment(w http.ResponseWriter, filename string) {
	disposition := "attachment; " + makeHeader(filename)
	w.Header().Set("Content-Disposition", disposition)
}

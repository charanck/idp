package pages

import (
	"encoding/json"
	"net/url"
	"strconv"
)

// jsQuote encodes s as a JSON string literal, safe to splice literally into
// a <script> block (e.g. `var x = { templ.Raw(jsQuote(s)) };`).
func jsQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

func formatCount(n int64) string {
	return strconv.FormatInt(n, 10)
}

func pageHref(path, extraQuery string, page int) string {
	q, _ := url.ParseQuery(extraQuery)
	if q == nil {
		q = url.Values{}
	}
	q.Set("page", strconv.Itoa(page))
	return path + "?" + q.Encode()
}

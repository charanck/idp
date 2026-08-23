package pages

import (
	"net/url"
	"strconv"
)

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

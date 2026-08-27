package web

import (
	"strconv"

	"github.com/labstack/echo/v4"
)

// Page is a slice of items for one page, mirroring Django's Paginator/Page.
type Page[T any] struct {
	Items          []T
	Number         int
	NumPages       int
	HasPrevious    bool
	HasNext        bool
	PreviousNumber int
	NextNumber     int
}

// Paginate slices items into fixed-size pages, clamping pageNumber into
// [1, NumPages].
func Paginate[T any](items []T, pageSize, pageNumber int) Page[T] {
	if pageSize < 1 {
		pageSize = 1
	}
	total := len(items)
	numPages := (total + pageSize - 1) / pageSize
	if numPages < 1 {
		numPages = 1
	}
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageNumber > numPages {
		pageNumber = numPages
	}

	start := (pageNumber - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return Page[T]{
		Items:          items[start:end],
		Number:         pageNumber,
		NumPages:       numPages,
		HasPrevious:    pageNumber > 1,
		HasNext:        pageNumber < numPages,
		PreviousNumber: pageNumber - 1,
		NextNumber:     pageNumber + 1,
	}
}

// SlidingWindow returns page numbers within +/-radius of the current page,
// clamped to [1, NumPages] - mirrors activity_log.html's
// "num > activities.number|add:'-3' and num < activities.number|add:'3'".
func (p Page[T]) SlidingWindow(radius int) []int {
	lo := p.Number - radius
	hi := p.Number + radius
	out := make([]int, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		if i >= 1 && i <= p.NumPages {
			out = append(out, i)
		}
	}
	return out
}

// PageRange returns every page number, 1..NumPages - used by list pages
// small enough to render every page link (configs/flags/environments/clients).
func (p Page[T]) PageRange() []int {
	out := make([]int, p.NumPages)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

// PageParam reads the "page" query param, defaulting to 1 on anything invalid.
func PageParam(c echo.Context) int {
	n, err := strconv.Atoi(c.QueryParam("page"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

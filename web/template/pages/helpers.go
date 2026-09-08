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

// multiselectOption is the {"id": "...", "name": "..."} shape ApplicationMultiselect
// (see shared.templ) feeds to the client-side searchable multiselect via
// templ.JSONScript - read back by /static/app.js's `multiselect` Alpine component.
type multiselectOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func multiselectOptions(apps []GroupApplicationOption) []multiselectOption {
	opts := make([]multiselectOption, len(apps))
	for i, app := range apps {
		opts[i] = multiselectOption{ID: app.ID, Name: app.Name}
	}
	return opts
}

func multiselectSelected(apps []GroupApplicationOption, selectedIDs map[string]bool) []string {
	selected := make([]string, 0, len(selectedIDs))
	for _, app := range apps {
		if selectedIDs[app.ID] {
			selected = append(selected, app.ID)
		}
	}
	return selected
}

// defaultAccentColor mirrors the indigo accent already used across the UI
// (e.g. primary buttons) - used as the login page's accent when no branding
// AccentColor is configured.
const defaultAccentColor = "#4f46e5"

// accentColorOrDefault fills in defaultAccentColor since <input type="color">
// requires a valid "#rrggbb" value - it can't be left blank.
func accentColorOrDefault(c string) string {
	if c == "" {
		return defaultAccentColor
	}
	return c
}

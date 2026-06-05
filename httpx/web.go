package httpx

import (
	"html/template"
	"net/http"
)

// MergeParams collects request parameters from both the URL query string and the
// POST form body into a single map, so a handler can read them uniformly whether
// the caller used GET or POST. When a key appears in both, the POST form value
// wins. It is handy for callbacks where the sender's method is not under your
// control (e.g. payment-gateway return URLs).
func MergeParams(r *http.Request) map[string]string {
	params := make(map[string]string)
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}
	if err := r.ParseForm(); err == nil {
		for k, v := range r.PostForm {
			if len(v) > 0 {
				params[k] = v[0]
			}
		}
	}
	return params
}

var autoPostTmpl = template.Must(template.New("autopost").Parse(`<!DOCTYPE html>
<html><head><meta charset="UTF-8"><title>Redirecting…</title></head>
<body onload="document.forms[0].submit()">
<form method="POST" action="{{.Action}}">
{{range $k, $v := .Fields}}<input type="hidden" name="{{$k}}" value="{{$v}}">
{{end}}<noscript><button type="submit">Continue</button></noscript>
</form></body></html>`))

// WriteAutoPostForm writes a self-submitting HTML form that POSTs fields to
// action and submits itself on load (with a no-script fallback button). Use it
// to bounce a browser to an upstream that requires a POST with hidden fields —
// for example a payment page that expects form-posted credentials. The action
// and all field names/values are HTML-escaped.
func WriteAutoPostForm(w http.ResponseWriter, action string, fields map[string]string) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return autoPostTmpl.Execute(w, struct {
		Action string
		Fields map[string]string
	}{Action: action, Fields: fields})
}

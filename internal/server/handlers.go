package server

import (
	"html/template"
	"net/http"
	"sort"
	"time"
)

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleStatus)
	mux.HandleFunc("GET /auth/{bank}", s.handleAuthGet)
	mux.HandleFunc("POST /auth/{bank}", s.handleAuthPost)
	mux.HandleFunc("GET /auth/{bank}/image.png", s.handleAuthImage)
	return mux
}

var statusTmpl = template.Must(template.New("status").Parse(`<!doctype html>
<html><head><title>paperless-bank</title></head>
<body>
<h1>paperless-bank</h1>
<table border="1" cellpadding="6" cellspacing="0">
<tr><th>Bank</th><th>Status</th><th>Last run</th><th>Next run</th></tr>
{{range .}}
<tr>
  <td>{{.Name}}</td>
  <td>{{if .Pending}}<a href="/auth/{{.Name}}">action required</a>{{else if .Err}}error: {{.Err}}{{else}}OK{{end}}</td>
  <td>{{.LastRunAt}}</td>
  <td>{{.NextRunAt}}</td>
</tr>
{{end}}
</table>
</body></html>
`))

type statusRow struct {
	Name      string
	Err       error
	LastRunAt string
	NextRunAt string
	Pending   bool
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(s.states))
	for name := range s.states {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]statusRow, 0, len(names))
	for _, name := range names {
		st := s.states[name]
		st.mu.Lock()
		row := statusRow{Name: name, Err: st.lastErr, NextRunAt: formatTime(st.nextRunAt)}
		row.LastRunAt = "never"
		if !st.lastRunAt.IsZero() {
			row.LastRunAt = formatTime(st.lastRunAt)
		}
		st.mu.Unlock()
		_, row.Pending = s.challenges.pendingFor(name)
		rows = append(rows, row)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := statusTmpl.Execute(w, rows); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

var authTmpl = template.Must(template.New("auth").Parse(`<!doctype html>
<html><head><title>paperless-bank — {{.Bank}}</title>
{{if not .NeedsInput}}<meta http-equiv="refresh" content="3">{{end}}
</head>
<body>
<h1>{{.Bank}}: authentication required</h1>
<p>{{.Description}}</p>
{{if .HasImage}}<p><img src="/auth/{{.Bank}}/image.png" alt="challenge graphic"></p>{{end}}
{{if .Hint}}<p>{{.Hint}}</p>{{end}}
{{if .NeedsInput}}
<form method="post">
  <input type="text" name="value" autofocus>
  <button type="submit">Submit</button>
</form>
{{end}}
<p><a href="/">back to status</a></p>
</body></html>
`))

type authView struct {
	Bank        string
	Description string
	Hint        string
	HasImage    bool
	NeedsInput  bool
}

func (s *Server) handleAuthGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("bank")
	challenge, ok := s.challenges.pendingFor(name)
	if !ok {
		http.Error(w, "no pending authentication challenge for "+name, http.StatusNotFound)
		return
	}
	view := authView{
		Bank:        name,
		Description: challenge.Description,
		Hint:        challenge.Hint,
		HasImage:    len(challenge.Image) > 0,
		NeedsInput:  challenge.NeedsInput,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := authTmpl.Execute(w, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleAuthPost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("bank")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	value := r.FormValue("value")
	if !s.challenges.submit(name, value) {
		http.Error(w, "no pending authentication challenge for "+name+" (it may already have been answered or expired)", http.StatusConflict)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleAuthImage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("bank")
	challenge, ok := s.challenges.pendingFor(name)
	if !ok || len(challenge.Image) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(challenge.Image)
}

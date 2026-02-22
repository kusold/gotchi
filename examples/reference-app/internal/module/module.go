package module

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kusold/gotchi/app"
)

type Module struct{}

func New() Module { return Module{} }

func (m Module) Register(r chi.Router, deps app.Dependencies) error {
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return nil
}

package jobapi

import (
	"net/http"

	"github.com/buildkite/agent/v3/internal/socket"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// router returns a chi router with the jobapi routes and appropriate middlewares mounted
func (s *Server) router() chi.Router {
	middlewares := [](func(http.Handler) http.Handler){}
	if s.debug {
		middlewares = append(middlewares, socket.LoggerMiddleware("Job API", s.Logger.Commentf))
	}
	middlewares = append(middlewares,
		middleware.Recoverer,

		// All responses are in JSON.
		socket.HeadersMiddleware(http.Header{"Content-Type": []string{"application/json"}}),
		socket.AuthMiddleware(s.token, s.Logger.Errorf),
	)

	r := chi.NewRouter()
	r.Use(middlewares...)

	r.Route("/api/current-job/v0", func(r chi.Router) {
		r.Get("/env", s.getEnv)
		r.Patch("/env", s.patchEnv)
		r.Delete("/env", s.deleteEnv)

		r.Post("/redactions", s.createRedaction)
	})

	return r
}

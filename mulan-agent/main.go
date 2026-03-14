package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/viper"

	"mulan-agent/lib/vfd"
)

func main() {
	viper.SetConfigFile(".env")
	viper.SetConfigType("env")
	viper.SetDefault("API_BASE", "http://localhost:8080")
	viper.SetDefault("PORT", "8081")
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("no .env file found, using defaults: %v", err)
	}

	apiBase := viper.GetString("API_BASE")
	port := viper.GetString("PORT")
	templateDir := "templates"

	// Start VFD display loop
	vfdEngine, err := vfd.New("COM3")
	if err != nil {
		log.Printf("VFD unavailable (COM3): %v", err)
	} else {
		defer vfdEngine.Close()
		go runVFD(vfdEngine)
	}

	// Start web server
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/pos", posHandler(templateDir, apiBase))

	log.Printf("mulan-agent starting on :%s (API_BASE=%s)", port, apiBase)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func runVFD(engine *vfd.Engine) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	messages := []string{"Hua Mulan", "Mulan Project"}
	i := 0

	for {
		msg := messages[i%len(messages)]
		engine.Write(msg)
		log.Printf("VFD: %s", msg)
		i++

		select {
		case <-sig:
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func posHandler(templateDir, apiBase string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		paths := []string{
			filepath.Join(templateDir, "layouts/pos.html"),
			filepath.Join(templateDir, "pos/index.html"),
		}

		tmpl, err := template.ParseFiles(paths...)
		if err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
			log.Printf("template parse error: %v", err)
			return
		}

		data := map[string]string{
			"APIBase": apiBase,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "layouts/pos.html", data); err != nil {
			http.Error(w, "render error", http.StatusInternalServerError)
			log.Printf("template render error: %v", err)
		}
	}
}

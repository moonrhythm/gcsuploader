package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	bucket := os.Getenv("BUCKET")
	bucketPath := os.Getenv("BUCKET_PATH")
	baseURL := os.Getenv("BASE_URL")
	authUser := os.Getenv("AUTH_USER")
	authPassword := os.Getenv("AUTH_PASSWORD")

	if bucket == "" {
		log.Fatal("BUCKET env required")
	}
	if baseURL == "" {
		log.Fatal("BASE_URL env required")
	}

	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}

	var h http.Handler
	h = &uploader{
		Bucket:     client.Bucket(bucket),
		BucketPath: bucketPath,
		BaseURL:    baseURL,
	}
	h = &basicAuth{
		User:     authUser,
		Password: authPassword,
		Handler:  h,
	}

	log.Print("start server on 0.0.0.0:" + port)
	log.Fatal(http.ListenAndServe(":"+port, h))
}

type uploader struct {
	Bucket     *storage.BucketHandle
	BucketPath string
	BaseURL    string

	initOnce sync.Once
	t        *template.Template
}

func (h *uploader) init() {
	if !strings.HasSuffix(h.BaseURL, "/") {
		h.BaseURL += "/"
	}

	// language=HTML
	h.t = template.Must(template.New("").Parse(`<!doctype html>
<title>GCS Uploader</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/dropzone@5.5.1/dist/min/dropzone.min.css">
<script src="https://cdn.jsdelivr.net/npm/dropzone@5.5.1/dist/min/dropzone.min.js" defer></script>
<script src="https://cdn.jsdelivr.net/npm/clipboard@2.0.4/dist/clipboard.min.js" defer></script>
<style>
	form {
		margin: 16px;
	}
</style>
<form method="POST" action="/" enctype="multipart/form-data" class="dropzone">
	<div class="fallback">
		<input name="file" type="file">
	</div>
</form>
<script>
	document.addEventListener('DOMContentLoaded', () => {
		Dropzone.autoDiscover = false
		
		new Dropzone("form", {
			url: '/',
			parallelUploads: 5,
			success (file, responseText) {
				file.previewTemplate.addEventListener('click', () => {
					open(responseText, '_blank')
				})
				
				const name = file.previewTemplate.querySelector('[data-dz-name]')
				if (name) {
					name.innerText = responseText
					name.dataset.clipboardText = responseText
					new ClipboardJS(name)
					name.addEventListener('click', (ev) => {
						ev.stopPropagation()
					})
				}
			}
		})
	})
</script>`))
}

func (h *uploader) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.initOnce.Do(h.init)

	if r.URL.Path != "/" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if r.Method == http.MethodPost {
		h.handlePost(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=0")
	h.t.Execute(w, nil)
}

func (h *uploader) handlePost(w http.ResponseWriter, r *http.Request) {
	fp, fh, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer fp.Close()

	fn := uuid.New().String()
	obj := h.Bucket.Object(path.Join(h.BucketPath, fn))
	objWriter := obj.NewWriter(r.Context())

	defer func() {
		if err != nil {
			objWriter.Close()
			obj.Delete(context.Background())
		}
	}()

	objWriter.CacheControl = "public, max-age=31536000, immutable"
	objWriter.ContentType = fh.Header.Get("Content-Type")
	_, err = io.Copy(objWriter, fp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = objWriter.Close()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, h.BaseURL+fn)
	log.Printf("uploaded: fn=%s; size=%d", fn, fh.Size)
}

type basicAuth struct {
	User     string
	Password string
	Handler  http.Handler
}

func (b *basicAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if b.User == "" || b.Password == "" {
		b.Handler.ServeHTTP(w, r)
		return
	}

	user, password, ok := r.BasicAuth()
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	okUser := subtle.ConstantTimeCompare([]byte(b.User), []byte(user)) == 1
	okPassword := subtle.ConstantTimeCompare([]byte(b.Password), []byte(password)) == 1

	if !okUser || !okPassword {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	b.Handler.ServeHTTP(w, r)
}

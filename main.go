package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"google.golang.org/api/option"
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
	objectMetadataJSON := os.Getenv("OBJECT_METADATA")                        // json
	gcpServiceAccountJSON := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS_JSON") // json

	if bucket == "" {
		log.Fatal("BUCKET env required")
	}
	if baseURL == "" {
		log.Fatal("BASE_URL env required")
	}

	var objectMetadata map[string]string
	if len(objectMetadataJSON) > 0 {
		err := json.Unmarshal([]byte(objectMetadataJSON), &objectMetadata)
		if err != nil {
			log.Fatalf("unmarshaling object metadata; %v", err)
		}
	}

	ctx := context.Background()
	var opt []option.ClientOption
	if gcpServiceAccountJSON != "" {
		opt = append(opt, option.WithCredentialsJSON([]byte(gcpServiceAccountJSON)))
	}
	client, err := storage.NewClient(ctx, opt...)
	if err != nil {
		log.Fatal(err)
	}

	var h http.Handler
	h = &uploader{
		Bucket:         client.Bucket(bucket),
		BucketPath:     bucketPath,
		BaseURL:        baseURL,
		ObjectMetadata: objectMetadata,
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
	Bucket         *storage.BucketHandle
	BucketPath     string
	BaseURL        string
	ObjectMetadata map[string]string

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
			maxFilesize: 10240,
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

	ct := fh.Header.Get("Content-Type")

	ext := filepath.Ext(fh.Filename)
	if ext == "" {
		exts, _ := mime.ExtensionsByType(ct)
		if len(exts) > 0 {
			ext = exts[0]
		}
	}

	fn := uuid.New().String() + ext
	obj := h.Bucket.Object(path.Join(h.BucketPath, fn))
	objWriter := obj.NewWriter(r.Context())

	defer func() {
		if err != nil {
			objWriter.Close()
			obj.Delete(context.Background())
		}
	}()

	if objWriter.Metadata == nil {
		objWriter.Metadata = make(map[string]string)
	}
	for k, v := range h.ObjectMetadata {
		objWriter.Metadata[k] = v
	}

	objWriter.CacheControl = "public, max-age=31536000, immutable"
	objWriter.ContentType = ct

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

	var ok bool
	defer func() {
		if !ok {
			w.Header().Set("WWW-Authenticate", "Basic")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		}
	}()

	user, password, ok := r.BasicAuth()
	if !ok {
		return
	}
	okUser := subtle.ConstantTimeCompare([]byte(b.User), []byte(user)) == 1
	okPassword := subtle.ConstantTimeCompare([]byte(b.Password), []byte(password)) == 1

	ok = okUser && okPassword
	if !ok {
		return
	}

	b.Handler.ServeHTTP(w, r)
}

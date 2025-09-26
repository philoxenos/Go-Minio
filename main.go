package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioHandler holds the MinIO client and bucket name.
type MinioHandler struct {
	minioClient *minio.Client
	bucketName  string
}

func main() {

	// -- Minio Environment Variables --
	// Load environment variables from .env file if it exists
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: Could not load .env file. Falling back to system environment variables.")
	}

	// --- MinIO Configuration ---
	endpoint := os.Getenv("MINIO_ENDPOINT") // E.g., "localhost:9000"
	accessKeyID := os.Getenv("MINIO_ACCESS_KEY")
	secretAccessKey := os.Getenv("MINIO_SECRET_KEY")
	bucketName := os.Getenv("MINIO_BUCKET")
	useSSL := true

	// A simple check for required env vars
	if endpoint == "" || accessKeyID == "" || secretAccessKey == "" || bucketName == "" {
		log.Fatal("Error: MINIO_ENDPOINT, MINIO_ACCESS_KEY, MINIO_SECRET_KEY, and MINIO_BUCKET environment variables must be set.")
	}

	// 1. Initialize MinIO client object.
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure:       useSSL,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		log.Fatalf("Error initializing MinIO client: %s\n", err)
	}

	log.Printf("Successfully connected to MinIO at %s\n", endpoint)

	// 2. Ensure the bucket exists.
	ctx := context.Background()
	err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	if err != nil {
		exists, errBucketExists := minioClient.BucketExists(ctx, bucketName)
		if errBucketExists == nil && exists {
			log.Printf("Bucket '%s' already exists.\n", bucketName)
		} else {
			log.Fatalf("Error creating/checking bucket: %s\n", err)
		}
	} else {
		log.Printf("Successfully created bucket '%s'.\n", bucketName)
	}

	// Instantiate our handler
	handler := &MinioHandler{
		minioClient: minioClient,
		bucketName:  bucketName,
	}

	// --- HTTP Server Setup ---
	http.HandleFunc("/upload", handler.uploadFileHandler)
	http.HandleFunc("/download/", handler.downloadFileHandler) // Note the trailing slash
	http.HandleFunc("/modify/", handler.modifyFileHandler)     // Note the trailing slash
	http.HandleFunc("/delete/", handler.deleteFileHandler)     // Note the trailing slash
	http.HandleFunc("/list", handler.listFilesHandler)
	http.HandleFunc("/watch", handler.watchBucketHandler)

	port := "8080"
	log.Printf("Starting server on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %s\n", err)
	}
}

// processAndUploadFile now accepts the object name as an argument.
func (h *MinioHandler) processAndUploadFile(w http.ResponseWriter, r *http.Request, objectName string) {
	// 10 MB limit
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Could not retrieve file from form-data", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// If the objectName was not provided by the handler,
	// then default to the file's original name (for the /upload route).
	if objectName == "" {
		objectName = header.Filename
	}

	contentType := header.Header.Get("Content-Type")

	// Upload the file to MinIO (this will create or overwrite)
	_, err = h.minioClient.PutObject(context.Background(), h.bucketName, objectName, file, header.Size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		log.Printf("Error uploading file to MinIO: %s", err)
		http.Error(w, "Failed to upload file", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Successfully processed '%s' in bucket '%s'.\n", objectName, h.bucketName)
}

// uploadFileHandler now passes an empty string for the object name,
// telling processAndUploadFile to use the file's own name.
func (h *MinioHandler) uploadFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.processAndUploadFile(w, r, "") // Use filename from form-data
}

// modifyFileHandler now extracts the name from the URL
// and passes it explicitly to processAndUploadFile.
func (h *MinioHandler) modifyFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get the object name from the URL path
	objectName := strings.TrimPrefix(r.URL.Path, "/modify/")
	if objectName == "" {
		http.Error(w, "Object name is required in the URL path (e.g., /modify/myfile.png)", http.StatusBadRequest)
		return
	}

	h.processAndUploadFile(w, r, objectName) // Use filename from URL
}

// downloadFileHandler serves a file from the bucket.
func (h *MinioHandler) downloadFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	objectName := strings.TrimPrefix(r.URL.Path, "/download/")
	if objectName == "" {
		http.Error(w, "Object name is required", http.StatusBadRequest)
		return
	}

	object, err := h.minioClient.GetObject(context.Background(), h.bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		log.Printf("Error getting object: %v", err)
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer object.Close()

	// Get object info to set headers
	stat, err := object.Stat()
	if err != nil {
		http.Error(w, "Could not get object stats", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", objectName))
	w.Header().Set("Content-Type", stat.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size))

	if _, err := io.Copy(w, object); err != nil {
		http.Error(w, "Failed to write file to response", http.StatusInternalServerError)
	}
}

// deleteFileHandler removes a file from the bucket.
func (h *MinioHandler) deleteFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	objectName := strings.TrimPrefix(r.URL.Path, "/delete/")
	if objectName == "" {
		http.Error(w, "Object name is required", http.StatusBadRequest)
		return
	}

	err := h.minioClient.RemoveObject(context.Background(), h.bucketName, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		log.Printf("Error removing object: %v", err)
		http.Error(w, "Failed to delete file", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Successfully deleted '%s' from bucket '%s'.\n", objectName, h.bucketName)
}

// listFilesHandler returns a JSON array of files in the bucket.
func (h *MinioHandler) listFilesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var fileList []string
	objectCh := h.minioClient.ListObjects(context.Background(), h.bucketName, minio.ListObjectsOptions{})
	for object := range objectCh {
		if object.Err != nil {
			log.Printf("Error listing object: %v", object.Err)
			http.Error(w, "Failed to list files", http.StatusInternalServerError)
			return
		}
		fileList = append(fileList, object.Key)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fileList)
}

// watchBucketHandler streams bucket events using Server-Sent Events (SSE).
func (h *MinioHandler) watchBucketHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// 2. Get the Flusher interface to send events immediately
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// 3. Listen for bucket notifications
	// NOTE: You must enable notifications on your bucket for this to work.
	// Example command: `mc event add local/mybucket arn:minio:sqs::1:webhook --event put,delete`
	notificationChan := h.minioClient.ListenBucketNotification(r.Context(), h.bucketName, "", "", []string{
		"s3:ObjectCreated:*",
		"s3:ObjectRemoved:*",
	})

	log.Println("SSE connection established. Watching for bucket events...")
	fmt.Fprintf(w, ": connection established\n\n") // SSE comment
	flusher.Flush()

	// 4. Loop and send events to the client
	for {
		select {
		case notification := <-notificationChan:
			if notification.Err != nil {
				log.Printf("Error in bucket notification: %v", notification.Err)
				// Send an error event to the client
				fmt.Fprintf(w, "event: error\ndata: %v\n\n", notification.Err)
				flusher.Flush()
				return
			}

			// Marshal notification to JSON to send as data
			jsonData, err := json.Marshal(notification.Records)
			if err != nil {
				log.Printf("Error marshaling notification: %v", err)
				continue
			}

			// Format as an SSE message
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush() // Flush the data to the client

		case <-r.Context().Done():
			// Client disconnected
			log.Println("SSE client disconnected.")
			return
		}
	}
}

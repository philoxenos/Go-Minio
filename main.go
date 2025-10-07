package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time" // <-- IMPORTED FOR URL EXPIRATION

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
	endpoint := os.Getenv("MINIO_ENDPOINT") // E.g., "dev-minio.psa.gov.ph"
	accessKeyID := os.Getenv("MINIO_ACCESS_KEY")
	secretAccessKey := os.Getenv("MINIO_SECRET_KEY")
	bucketName := os.Getenv("MINIO_BUCKET")
	useSSL := true // Should be true for production

	if endpoint == "" || accessKeyID == "" || secretAccessKey == "" || bucketName == "" {
		log.Fatal("Error: MINIO_ENDPOINT, MINIO_ACCESS_KEY, MINIO_SECRET_KEY, and MINIO_BUCKET environment variables must be set.")
	}

	// 1. Initialize MinIO client object.
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
		// Using BucketLookupPath is important for Nginx proxy compatibility.
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
	http.HandleFunc("/modify/", handler.modifyFileHandler)
	http.HandleFunc("/delete/", handler.deleteFileHandler)
	http.HandleFunc("/list", handler.listFilesHandler)
	http.HandleFunc("/watch", handler.watchBucketHandler)

	// --- REPLACED THE DOWNLOAD HANDLER ---
	// http.HandleFunc("/download/", handler.downloadFileHandler) // <-- OLD WAY
	http.HandleFunc("/get-download-link/", handler.getPresignedURLHandler) // <-- NEW, RECOMMENDED WAY

	port := "8080"
	log.Printf("Starting server on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %s\n", err)
	}
}

// =================================================================================
// NEW HANDLER: getPresignedURLHandler
// This handler generates a temporary, secure URL for a private object.
// =================================================================================
func (h *MinioHandler) getPresignedURLHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	objectName := strings.TrimPrefix(r.URL.Path, "/get-download-link/")
	if objectName == "" {
		http.Error(w, "Object name is required in the URL path (e.g., /get-download-link/my-image.jpg)", http.StatusBadRequest)
		return
	}

	// 1. Set the expiration time for the URL.
	// Here, we set it to 5 minutes.
	expiry := 5 * time.Minute

	// 2. Generate the presigned URL.
	presignedURL, err := h.minioClient.PresignedGetObject(context.Background(), h.bucketName, objectName, expiry, nil)
	if err != nil {
		log.Printf("Error generating presigned URL for '%s': %v", objectName, err)
		// This error often means the object doesn't exist, so 404 is appropriate.
		http.Error(w, "File not found or access denied", http.StatusNotFound)
		return
	}

	// 3. Create a JSON response containing the URL.
	response := map[string]string{
		"url": presignedURL.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// (The rest of your handlers: uploadFileHandler, modifyFileHandler, deleteFileHandler, etc. remain exactly the same)

func (h *MinioHandler) processAndUploadFile(w http.ResponseWriter, r *http.Request, objectName string) {
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
	if objectName == "" {
		objectName = header.Filename
	}
	contentType := header.Header.Get("Content-Type")
	_, err = h.minioClient.PutObject(context.Background(), h.bucketName, objectName, file, header.Size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		log.Printf("Error uploading file to MinIO: %s", err)
		http.Error(w, "Failed to upload file", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Successfully processed '%s' in bucket '%s'.\n", objectName, h.bucketName)
}

func (h *MinioHandler) uploadFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.processAndUploadFile(w, r, "")
}

func (h *MinioHandler) modifyFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	objectName := strings.TrimPrefix(r.URL.Path, "/modify/")
	if objectName == "" {
		http.Error(w, "Object name is required in the URL path (e.g., /modify/myfile.png)", http.StatusBadRequest)
		return
	}
	h.processAndUploadFile(w, r, objectName)
}

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

func (h *MinioHandler) watchBucketHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}
	notificationChan := h.minioClient.ListenBucketNotification(r.Context(), h.bucketName, "", "", []string{
		"s3:ObjectCreated:*",
		"s3:ObjectRemoved:*",
	})
	log.Println("SSE connection established. Watching for bucket events...")
	fmt.Fprintf(w, ": connection established\n\n")
	flusher.Flush()
	for {
		select {
		case notification := <-notificationChan:
			if notification.Err != nil {
				log.Printf("Error in bucket notification: %v", notification.Err)
				fmt.Fprintf(w, "event: error\ndata: %v\n\n", notification.Err)
				flusher.Flush()
				return
			}
			jsonData, err := json.Marshal(notification.Records)
			if err != nil {
				log.Printf("Error marshaling notification: %v", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()
		case <-r.Context().Done():
			log.Println("SSE client disconnected.")
			return
		}
	}
}

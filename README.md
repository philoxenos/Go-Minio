# Go MinIO API Documentation

A simple Go API designed to test and interact with a MinIO object storage server. This API provides basic CRUD (Create, Read, Update, Delete) operations, file listing, and real-time bucket monitoring, all accessible via a standard HTTP interface.

It's a perfect tool for verifying your MinIO setup or for learning how to use the MinIO Go SDK.

## ✨ Features
- Upload a file to a bucket.
- Download a file from a bucket.
- Modify (overwrite) a file in a bucket.
- Delete a file from a bucket.
- List all files in a bucket.
- Watch a bucket for real-time events (uploads/deletes).

## 📋 Prerequisites
Before you begin, ensure you have the following installed and running:

- **Go**: Version 1.18 or later.
- **MinIO Server**: A running MinIO instance. You can start one easily with Docker:
  ```bash
  docker run -p 9000:9000 -p 9001:9001 minio/minio server /data --console-address ":9001"
  ```
- **Postman**: To send requests to the API.
- **MinIO Client (mc)**: Optional, but required to enable bucket notifications for the `/watch` endpoint.

## 🚀 Setup & Installation
Follow these steps to get the API server running on your local machine.

### 1. Get the Code
If you have this in a Git repository, clone it. Otherwise, ensure the `main.go` file is in a dedicated project directory.

### 2. Install Dependencies
Open your terminal in the project directory and run the following commands to install the necessary Go packages:

```bash
go get github.com/minio/minio-go/v7
go get github.com/joho/godotenv
```

### 3. Configure Environment Variables
The API loads its configuration from a `.env` file.

Create a file named `.env` in the root of your project and populate it with your MinIO server details.

```.env
# Your MinIO server address (hostname only)
MINIO_ENDPOINT=localhost:9000

# Your MinIO credentials
MINIO_ACCESS_KEY=minioadmin
MINIO_SECRET_KEY=minioadmin

# The bucket you want the API to use (it will be created if it doesn't exist)
MINIO_BUCKET=testbucket
```

> 🔒 **Security Note**: Always add your `.env` file to your `.gitignore` file to prevent committing secrets to version control.

### 4. Enable Bucket Notifications (for `/watch` endpoint)
For the `/watch` feature to work, you must enable events on your MinIO bucket.

**Configure mc:**
```bash
mc alias set myminio http://localhost:9000 minioadmin minioadmin
```

**Add Event Webhook:**
```bash
mc event add myminio/testbucket arn:minio:sqs::1:webhook --event put,delete
```

**Restart the Service Hook:**
```bash
mc admin service restart myminio
```

### 5. Run the API Server
Now you're ready to start the server!

```bash
go run .
```

You should see output confirming the server has started on port 8080.
```
Successfully connected to MinIO at localhost:9000
Bucket 'testbucket' already exists.
Starting server on port 8080...
```

## 🤖 Testing with Postman
You can now use Postman to interact with the API. Set your base URL in Postman to `http://localhost:8080`.

### 1. Upload a File
Creates a new object in the bucket. The object's name is taken from the uploaded file's name.

- **Method**: `POST`
- **Endpoint**: `/upload`
- **Body**:
  - Select `form-data`.
  - Create a key named `file`.
  - On the right side of the key, change its type from `Text` to `File`.
  - Click "Select Files" and choose any file from your computer.
- **Success Response**: `201 Created`
  ```
  Successfully processed 'my-test-file.txt' in bucket 'testbucket'.
  ```

### 2. List Files
Retrieves a list of all object names in the bucket.

- **Method**: `GET`
- **Endpoint**: `/list`
- **Success Response**: `200 OK`
  ```json
  [
    "my-test-file.txt"
  ]
  ```

### 3. Download a File
Downloads the content of a specific object.

- **Method**: `GET`
- **Endpoint**: `/download/{objectName}`
- **Example**: `/download/my-test-file.txt`
- **Action**: In Postman, use the **Send and Download** button. Postman will prompt you to save the file.
- **Success Response**: `200 OK`

### 4. Modify a File
Replaces the content of an existing object. The object to be replaced is identified by the name in the URL.

- **Method**: `PUT`
- **Endpoint**: `/modify/{objectName}`
- **Example**: `/modify/my-test-file.txt`
- **Body**:
  - Select `form-data`.
  - Create a key named `file` and select a file (its content will be used to overwrite the object). The name of this file doesn't matter.
- **Success Response**: `201 Created`
  ```
  Successfully processed 'my-test-file.txt' in bucket 'testbucket'.
  ```

### 5. Delete a File
Removes an object from the bucket.

- **Method**: `DELETE`
- **Endpoint**: `/delete/{objectName}`
- **Example**: `/delete/my-test-file.txt`
- **Success Response**: `200 OK`
  ```
  Successfully deleted 'my-test-file.txt' from bucket 'testbucket'.
  ```

### 6. Watch Bucket Events
Streams real-time events from the bucket using Server-Sent Events (SSE).

- **Method**: `GET`
- **Endpoint**: `/watch`
- **How to Test**:
  1. Send the request to `/watch`. Postman will show a "loading" state. This is correct, as it's keeping the connection open to listen for events.
  2. While the `/watch` request is still "loading," open a new Postman tab.
  3. In the new tab, perform other actions like **Upload a File** or **Delete a File**.
  4. Switch back to your original `/watch` tab. You will see JSON event data appearing in the response body in real-time as the actions occur.

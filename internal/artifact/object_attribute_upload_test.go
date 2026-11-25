package artifact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"google.golang.org/api/option"
	storage "google.golang.org/api/storage/v1"
)

func TestS3UploadObjectTags(t *testing.T) {
	t.Setenv("BUILDKITE_S3_ACL", "private")
	t.Setenv("BUILDKITE_S3_SSE_ENABLED", "true")
	for _, size := range []int{19, 6 * 1024 * 1024} {
		for _, tagged := range []bool{false, true} {
			t.Run(fmt.Sprintf("size=%d/tagged=%t", size, tagged), func(t *testing.T) {
				payload := bytes.Repeat([]byte("x"), size)
				copy(payload, "artifact contents!")
				path := filepath.Join(t.TempDir(), "report.txt")
				if err := os.WriteFile(path, payload, 0o600); err != nil {
					t.Fatal(err)
				}
				var want url.Values
				var tags map[string]string
				if tagged {
					tags = map[string]string{"pipeline": "payments/api + jobs=3", "empty": ""}
					want = url.Values{"pipeline": {tags["pipeline"]}, "empty": {""}}
				}
				var mu sync.Mutex
				parts := map[int][]byte{}
				var created, completed bool
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					if r.URL.Path != "/bucket/prefix/report.txt" {
						t.Errorf("object path=%q", r.URL.Path)
					}
					if r.Method == "PUT" && r.URL.Query().Get("partNumber") != "" {
						n, _ := strconv.Atoi(r.URL.Query().Get("partNumber"))
						data, err := io.ReadAll(r.Body)
						if err != nil {
							t.Error(err)
						}
						parts[n] = data
						w.Header().Set("ETag", fmt.Sprintf(`"part-%d"`, n))
						return
					}
					if r.Method == "POST" && r.URL.Query().Get("uploadId") != "" {
						completed = true
						_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><ETag>"complete"</ETag></CompleteMultipartUploadResult>`)
						return
					}
					created = true
					got, err := url.ParseQuery(r.Header.Get("X-Amz-Tagging"))
					if err != nil {
						t.Error(err)
					}
					if got.Encode() != want.Encode() {
						t.Errorf("tags=%v, want %v", got, want)
					}
					if r.Header.Get("X-Amz-Acl") != "private" || r.Header.Get("X-Amz-Server-Side-Encryption") != "AES256" || r.Header.Get("Content-Type") != "text/plain" {
						t.Errorf("object properties=%v", r.Header)
					}
					if r.Method == "POST" {
						_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>prefix/report.txt</Key><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>`)
					} else {
						data, err := io.ReadAll(r.Body)
						if err != nil {
							t.Error(err)
						}
						parts[1] = data
						w.Header().Set("ETag", `"single"`)
					}
				}))
				defer server.Close()
				client := s3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, BaseEndpoint: aws.String(server.URL)}, func(o *s3.Options) { o.UsePathStyle = true })
				u := &S3Uploader{client: client, logger: logger.Discard, BucketName: "bucket", BucketPath: "prefix", conf: S3UploaderConfig{Tags: tags}}
				work, err := u.CreateWork(&api.Artifact{Path: "report.txt", AbsolutePath: path, ContentType: "text/plain"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := work[0].DoWork(t.Context()); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				defer mu.Unlock()
				var uploaded []byte
				for i := 1; i <= len(parts); i++ {
					uploaded = append(uploaded, parts[i]...)
				}
				if !bytes.Equal(uploaded, payload) {
					t.Errorf("uploaded bytes differ: got %d, want %d", len(uploaded), len(payload))
				}
				if !created || completed != (size > 5*1024*1024) {
					t.Errorf("created=%t completed=%t", created, completed)
				}
			})
		}
	}
}

func TestGCSUploadObjectContexts(t *testing.T) {
	t.Setenv("BUILDKITE_GS_ACL", "private")
	for _, size := range []int{23, 17 * 1024 * 1024} {
		for _, classified := range []bool{false, true} {
			t.Run(fmt.Sprintf("size=%d/contexts=%t", size, classified), func(t *testing.T) {
				payload := bytes.Repeat([]byte("g"), size)
				path := filepath.Join(t.TempDir(), "report.txt")
				if err := os.WriteFile(path, payload, 0o600); err != nil {
					t.Fatal(err)
				}
				var contexts map[string]string
				if classified {
					contexts = map[string]string{"pipeline": "payments", "job": "abc-123"}
				}
				var object storage.Object
				var uploaded []byte
				var initiated bool
				var server *httptest.Server
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.URL.Path != "/session" {
						if r.URL.Query().Get("predefinedAcl") != "private" {
							t.Errorf("ACL=%q", r.URL.Query().Get("predefinedAcl"))
						}
						if r.URL.Query().Get("uploadType") == "resumable" {
							initiated = true
							if err := json.NewDecoder(r.Body).Decode(&object); err != nil {
								t.Error(err)
							}
							w.Header().Set("Location", server.URL+"/session")
							return
						}
						_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
						if err != nil {
							t.Error(err)
							w.WriteHeader(400)
							return
						}
						reader := multipart.NewReader(r.Body, params["boundary"])
						part, err := reader.NextPart()
						if err != nil {
							t.Error(err)
							w.WriteHeader(400)
							return
						}
						if err := json.NewDecoder(part).Decode(&object); err != nil {
							t.Error(err)
						}
						part, err = reader.NextPart()
						if err != nil {
							t.Error(err)
							w.WriteHeader(400)
							return
						}
						data, err := io.ReadAll(part)
						if err != nil {
							t.Error(err)
						}
						uploaded = append(uploaded, data...)
					} else {
						data, err := io.ReadAll(r.Body)
						if err != nil {
							t.Error(err)
						}
						uploaded = append(uploaded, data...)
						if len(uploaded) < size {
							w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(uploaded)-1))
							// Match the GCS protocol negotiated by X-GUploader-No-308.
							w.Header().Set("X-Http-Status-Code-Override", "308")
							return
						}
					}
					_, _ = io.WriteString(w, `{"name":"prefix/report.txt"}`)
				}))
				defer server.Close()
				client, err := storage.NewService(t.Context(), option.WithoutAuthentication(), option.WithEndpoint(server.URL+"/"))
				if err != nil {
					t.Fatal(err)
				}
				u := &GSUploader{service: client, logger: logger.Discard, BucketName: "bucket", BucketPath: "prefix", conf: GSUploaderConfig{Contexts: contexts}}
				work, err := u.CreateWork(&api.Artifact{Path: "report.txt", AbsolutePath: path, ContentType: "text/plain"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := work[0].DoWork(t.Context()); err != nil {
					t.Fatal(err)
				}
				got := map[string]string{}
				if object.Contexts != nil {
					for key, value := range object.Contexts.Custom {
						got[key] = value.Value
					}
				}
				if !maps.Equal(got, contexts) {
					t.Errorf("contexts=%v, want %v", got, contexts)
				}
				if object.Name != "prefix/report.txt" || object.ContentType != "text/plain" || object.ContentDisposition != `inline; filename="report.txt"` || len(object.Metadata) != 0 {
					t.Errorf("object properties=%+v", object)
				}
				if !bytes.Equal(uploaded, payload) || initiated != (size > 16*1024*1024) {
					t.Errorf("uploaded=%d bytes, initiated=%t", len(uploaded), initiated)
				}
			})
		}
	}
}

type attributeTransport func(*http.Request) (*http.Response, error)

func (f attributeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAzureUploadBlobIndexTags(t *testing.T) {
	for _, size := range []int64{27, 257 * 1024 * 1024} {
		for _, tagged := range []bool{false, true} {
			t.Run(fmt.Sprintf("size=%d/tagged=%t", size, tagged), func(t *testing.T) {
				f, err := os.CreateTemp(t.TempDir(), "report")
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Truncate(size); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				var tags map[string]string
				if tagged {
					tags = map[string]string{"pipeline": "payments/api + jobs=3", "empty": ""}
				}
				var stages atomic.Int32
				var commits atomic.Int32
				client, err := service.NewClientWithNoCredential("https://account.blob.core.windows.net", &service.ClientOptions{ClientOptions: azcore.ClientOptions{Transport: &http.Client{Transport: attributeTransport(func(r *http.Request) (*http.Response, error) {
					if r.Body != nil {
						defer r.Body.Close() //nolint:errcheck // Fake transport request body.
					}
					if r.URL.Path != "/container/prefix/report.txt" {
						t.Errorf("object path=%q", r.URL.Path)
					}
					if r.URL.Query().Get("comp") == "block" {
						stages.Add(1)
					} else {
						commits.Add(1)
						// Azure's generated client stores non-canonical header keys;
						// a fake transport sees them before net/http serializes them.
						var rawTags string
						for key, values := range r.Header {
							if strings.EqualFold(key, "x-ms-tags") {
								rawTags = values[0]
							}
						}
						got, err := url.ParseQuery(rawTags)
						if err != nil {
							t.Error(err)
						}
						values := url.Values{}
						for key, value := range tags {
							values.Set(key, value)
						}
						if got.Encode() != values.Encode() {
							t.Errorf("tags=%v, want %v", got, values)
						}
						if size < 256*1024*1024 {
							data, err := io.ReadAll(r.Body)
							if err != nil {
								t.Error(err)
							}
							if len(data) != int(size) {
								t.Errorf("body size=%d", len(data))
							}
						} else {
							data, err := io.ReadAll(r.Body)
							if err != nil {
								t.Error(err)
							}
							if !strings.Contains(string(data), "<Latest>") {
								t.Errorf("missing committed blocks: %s", data)
							}
						}
					}
					return &http.Response{StatusCode: 201, Header: http.Header{"Etag": {`"created"`}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				})}}})
				if err != nil {
					t.Fatal(err)
				}
				u := &AzureBlobUploader{client: client, logger: logger.Discard, loc: &AzureBlobLocation{StorageAccountName: "account", ContainerName: "container", BlobPath: "prefix"}, conf: AzureBlobUploaderConfig{Tags: tags}}
				work, err := u.CreateWork(&api.Artifact{Path: "report.txt", AbsolutePath: f.Name()})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := work[0].DoWork(t.Context()); err != nil {
					t.Fatal(err)
				}
				if commits.Load() != 1 || (stages.Load() > 0) != (size > 256*1024*1024) {
					t.Errorf("commits=%d stages=%d", commits.Load(), stages.Load())
				}
			})
		}
	}
}

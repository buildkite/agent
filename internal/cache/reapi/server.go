// Package reapi exposes the cache-only Remote Execution API for local jobs.
package reapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"

	repb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/bazelbuild/remote-apis/build/bazel/semver"
	"github.com/google/uuid"
	bs "google.golang.org/genproto/googleapis/bytestream"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const maxBatchSize = 3 << 20

type session struct {
	backend *Backend
	ctx     context.Context
}

type backendKey struct{}

type Server struct {
	repb.UnimplementedCapabilitiesServer
	repb.UnimplementedContentAddressableStorageServer
	repb.UnimplementedActionCacheServer
	bs.UnimplementedByteStreamServer
	grpc     *grpc.Server
	listener net.Listener
	mu       sync.RWMutex
	sessions map[string]session
}

// Listen binds a numeric loopback address. Port zero allocates an unused port.
func Listen(addr string) (*Server, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("REAPI cache address must be a numeric loopback address")
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{listener: l, sessions: make(map[string]session)}
	s.grpc = grpc.NewServer(
		grpc.MaxRecvMsgSize(4<<20),
		grpc.UnaryInterceptor(s.unary),
		grpc.StreamInterceptor(s.stream),
	)
	repb.RegisterCapabilitiesServer(s.grpc, s)
	repb.RegisterContentAddressableStorageServer(s.grpc, s)
	repb.RegisterActionCacheServer(s.grpc, s)
	bs.RegisterByteStreamServer(s.grpc, s)
	go func() {
		if err := s.grpc.Serve(l); err != nil {
			slog.Error("REAPI cache server stopped", "error", err)
		}
	}()
	return s, nil
}

func (s *Server) Endpoint() string { return "http://" + s.listener.Addr().String() }
func (s *Server) Close()           { s.grpc.Stop() }

// Register grants access only for the lifetime of a job. The random bearer
// token is independent of the API credential, including on older agents that
// use a shared session credential for API calls.
func (s *Server) Register(ctx context.Context, token string, b *Backend) func() {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.sessions[token] = session{backend: b, ctx: ctx}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		cancel()
	}
}

func NewToken() string { return uuid.NewString() }

func (s *Server) authorize(ctx context.Context) (context.Context, func(), error) {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return nil, nil, status.Error(codes.Unauthenticated, "job bearer token required")
	}
	s.mu.RLock()
	job, ok := s.sessions[strings.TrimPrefix(values[0], "Bearer ")]
	s.mu.RUnlock()
	if !ok || job.ctx.Err() != nil {
		return nil, nil, status.Error(codes.Unauthenticated, "job bearer token is not active")
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(job.ctx, cancel)
	return context.WithValue(ctx, backendKey{}, job.backend), func() { stop(); cancel() }, nil
}

func (s *Server) unary(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx, cancel, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return handler(ctx, req)
}

type authorizedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authorizedStream) Context() context.Context { return s.ctx }

func (s *Server) stream(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, cancel, err := s.authorize(stream.Context())
	if err != nil {
		return err
	}
	defer cancel()
	return handler(srv, &authorizedStream{ServerStream: stream, ctx: ctx})
}

func validate(instance string, function repb.DigestFunction_Value, d *repb.Digest) error {
	if len(instance) > 256 {
		return status.Error(codes.InvalidArgument, "instance_name must contain at most 256 bytes")
	}
	if function != repb.DigestFunction_UNKNOWN && function != repb.DigestFunction_SHA256 {
		return status.Error(codes.InvalidArgument, "only SHA256 digests are supported")
	}
	if d == nil || len(d.Hash) != 64 || strings.ToLower(d.Hash) != d.Hash || d.SizeBytes < 0 {
		return status.Error(codes.InvalidArgument, "invalid SHA256 digest")
	}
	if _, err := hex.DecodeString(d.Hash); err != nil {
		return status.Error(codes.InvalidArgument, "invalid SHA256 digest")
	}
	if d.SizeBytes > maxBlobSize {
		return status.Error(codes.ResourceExhausted, "blob exceeds 64 MiB limit")
	}
	return nil
}

func matches(d *repb.Digest, data []byte) bool {
	h := sha256.Sum256(data)
	return d.SizeBytes == int64(len(data)) && d.Hash == hex.EncodeToString(h[:])
}

func digestKey(d *repb.Digest) string { return d.Hash + "/" + strconv.FormatInt(d.SizeBytes, 10) }

func getBlob(ctx context.Context, instance string, d *repb.Digest) ([]byte, error) {
	if matches(d, nil) {
		return []byte{}, nil
	}
	b := ctx.Value(backendKey{}).(*Backend)
	data, err := b.get(ctx, instance, "cas", digestKey(d))
	if err != nil {
		return nil, err
	}
	if !matches(d, data) {
		return nil, status.Error(codes.DataLoss, "CAS digest mismatch")
	}
	return data, nil
}

func (*Server) GetCapabilities(ctx context.Context, req *repb.GetCapabilitiesRequest) (*repb.ServerCapabilities, error) {
	return &repb.ServerCapabilities{
		CacheCapabilities: &repb.CacheCapabilities{
			DigestFunctions:               []repb.DigestFunction_Value{repb.DigestFunction_SHA256},
			ActionCacheUpdateCapabilities: &repb.ActionCacheUpdateCapabilities{UpdateEnabled: true},
			MaxBatchTotalSizeBytes:        maxBatchSize,
		},
		ExecutionCapabilities: &repb.ExecutionCapabilities{ExecEnabled: false},
		LowApiVersion:         &semver.SemVer{Major: 2},
		HighApiVersion:        &semver.SemVer{Major: 2, Minor: 3},
	}, nil
}

func (*Server) FindMissingBlobs(ctx context.Context, req *repb.FindMissingBlobsRequest) (*repb.FindMissingBlobsResponse, error) {
	resp := &repb.FindMissingBlobsResponse{}
	for _, d := range req.BlobDigests {
		if err := validate(req.InstanceName, req.DigestFunction, d); err != nil {
			return nil, err
		}
		_, err := getBlob(ctx, req.InstanceName, d)
		if status.Code(err) == codes.NotFound {
			resp.MissingBlobDigests = append(resp.MissingBlobDigests, d)
		} else if err != nil {
			return nil, err
		}
	}
	return resp, nil
}

func (*Server) BatchUpdateBlobs(ctx context.Context, req *repb.BatchUpdateBlobsRequest) (*repb.BatchUpdateBlobsResponse, error) {
	var total int
	for _, r := range req.Requests {
		total += len(r.Data)
	}
	if total > maxBatchSize {
		return nil, status.Error(codes.ResourceExhausted, "batch exceeds 3 MiB limit")
	}
	resp := &repb.BatchUpdateBlobsResponse{}
	for _, r := range req.Requests {
		err := validate(req.InstanceName, req.DigestFunction, r.Digest)
		if err == nil && r.Compressor != repb.Compressor_IDENTITY {
			err = status.Error(codes.InvalidArgument, "compressed blobs are not supported")
		}
		if err == nil && !matches(r.Digest, r.Data) {
			err = status.Error(codes.InvalidArgument, "CAS digest mismatch")
		}
		if err == nil {
			err = ctx.Value(backendKey{}).(*Backend).put(ctx, req.InstanceName, "cas", digestKey(r.Digest), r.Data)
		}
		resp.Responses = append(resp.Responses, &repb.BatchUpdateBlobsResponse_Response{Digest: r.Digest, Status: blobStatus(err)})
	}
	return resp, nil
}

func (*Server) BatchReadBlobs(ctx context.Context, req *repb.BatchReadBlobsRequest) (*repb.BatchReadBlobsResponse, error) {
	var total int64
	for _, d := range req.Digests {
		if err := validate(req.InstanceName, req.DigestFunction, d); err != nil {
			return nil, err
		}
		total += d.SizeBytes
	}
	if total > maxBatchSize {
		return nil, status.Error(codes.ResourceExhausted, "batch exceeds 3 MiB limit")
	}
	resp := &repb.BatchReadBlobsResponse{}
	for _, d := range req.Digests {
		data, err := getBlob(ctx, req.InstanceName, d)
		resp.Responses = append(resp.Responses, &repb.BatchReadBlobsResponse_Response{Digest: d, Data: data, Status: blobStatus(err)})
	}
	return resp, nil
}

func blobStatus(err error) *statuspb.Status {
	return &statuspb.Status{Code: int32(status.Code(err)), Message: status.Convert(err).Message()}
}

func (*Server) UpdateActionResult(ctx context.Context, req *repb.UpdateActionResultRequest) (*repb.ActionResult, error) {
	if err := validate(req.InstanceName, req.DigestFunction, req.ActionDigest); err != nil {
		return nil, err
	}
	if req.ActionResult == nil {
		return nil, status.Error(codes.InvalidArgument, "action_result is required")
	}
	data, err := proto.Marshal(req.ActionResult)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid action_result")
	}
	if err := ctx.Value(backendKey{}).(*Backend).put(ctx, req.InstanceName, "ac", digestKey(req.ActionDigest), data); err != nil {
		return nil, err
	}
	return req.ActionResult, nil
}

func (*Server) GetActionResult(ctx context.Context, req *repb.GetActionResultRequest) (*repb.ActionResult, error) {
	if err := validate(req.InstanceName, req.DigestFunction, req.ActionDigest); err != nil {
		return nil, err
	}
	data, err := ctx.Value(backendKey{}).(*Backend).get(ctx, req.InstanceName, "ac", digestKey(req.ActionDigest))
	if err != nil {
		return nil, err
	}
	result := &repb.ActionResult{}
	if err := proto.Unmarshal(data, result); err != nil {
		return nil, status.Error(codes.DataLoss, "invalid action result")
	}
	if err := checkOutputs(ctx, req.InstanceName, result); err != nil {
		return nil, err
	}
	return result, nil
}

func checkOutputs(ctx context.Context, instance string, result *repb.ActionResult) error {
	digests := []*repb.Digest{result.StdoutDigest, result.StderrDigest}
	for _, f := range result.OutputFiles {
		digests = append(digests, f.Digest)
	}
	for _, output := range result.OutputDirectories {
		if err := validate(instance, repb.DigestFunction_SHA256, output.TreeDigest); err != nil {
			return err
		}
		data, err := getBlob(ctx, instance, output.TreeDigest)
		if err != nil {
			return err
		}
		tree := &repb.Tree{}
		if err := proto.Unmarshal(data, tree); err != nil {
			return status.Error(codes.DataLoss, "invalid output tree")
		}
		for _, dir := range append(tree.Children, tree.Root) {
			for _, file := range dir.GetFiles() {
				digests = append(digests, file.Digest)
			}
		}
	}
	for _, d := range digests {
		if d == nil {
			continue
		}
		if err := validate(instance, repb.DigestFunction_SHA256, d); err != nil {
			return err
		}
		if _, err := getBlob(ctx, instance, d); err != nil {
			return err
		}
	}
	return nil
}

func resource(name string, write bool) (string, *repb.Digest, error) {
	parts := strings.Split(name, "/")
	n := len(parts)
	if n < 3 || parts[n-3] != "blobs" {
		return "", nil, status.Error(codes.InvalidArgument, "expected uncompressed blobs/hash/size resource")
	}
	end := n - 3
	if write {
		if n < 5 || parts[n-5] != "uploads" || parts[n-4] == "" {
			return "", nil, status.Error(codes.InvalidArgument, "expected uploads/id/blobs/hash/size resource")
		}
		end = n - 5
	}
	size, err := strconv.ParseInt(parts[n-1], 10, 64)
	if err != nil {
		return "", nil, status.Error(codes.InvalidArgument, "invalid resource size")
	}
	instance := strings.Join(parts[:end], "/")
	d := &repb.Digest{Hash: parts[n-2], SizeBytes: size}
	return instance, d, validate(instance, repb.DigestFunction_SHA256, d)
}

func (*Server) Read(req *bs.ReadRequest, stream bs.ByteStream_ReadServer) error {
	instance, d, err := resource(req.ResourceName, false)
	if err != nil {
		return err
	}
	if req.ReadOffset < 0 || req.ReadOffset > d.SizeBytes || req.ReadLimit < 0 {
		return status.Error(codes.OutOfRange, "invalid read range")
	}
	data, err := getBlob(stream.Context(), instance, d)
	if err != nil {
		return err
	}
	data = data[req.ReadOffset:]
	if req.ReadLimit > 0 && req.ReadLimit < int64(len(data)) {
		data = data[:req.ReadLimit]
	}
	for len(data) > 0 {
		n := min(len(data), 64<<10)
		if err := stream.Send(&bs.ReadResponse{Data: data[:n]}); err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

func (*Server) Write(stream bs.ByteStream_WriteServer) error {
	var name, instance string
	var d *repb.Digest
	var data []byte
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return status.Error(codes.InvalidArgument, "write ended before finish_write")
		}
		if err != nil {
			return err
		}
		if d == nil {
			name = req.ResourceName
			instance, d, err = resource(name, true)
			if err != nil {
				return err
			}
		}
		if req.ResourceName != "" && req.ResourceName != name {
			return status.Error(codes.InvalidArgument, "resource changed during write")
		}
		if req.WriteOffset != int64(len(data)) {
			return status.Error(codes.OutOfRange, "nonsequential write offset")
		}
		if int64(len(data))+int64(len(req.Data)) > d.SizeBytes {
			return status.Error(codes.InvalidArgument, "write exceeds declared size")
		}
		data = append(data, req.Data...)
		if req.FinishWrite {
			if !matches(d, data) {
				return status.Error(codes.InvalidArgument, "CAS digest mismatch")
			}
			if err := stream.Context().Value(backendKey{}).(*Backend).put(stream.Context(), instance, "cas", digestKey(d), data); err != nil {
				return err
			}
			return stream.SendAndClose(&bs.WriteResponse{CommittedSize: int64(len(data))})
		}
	}
}

func (*Server) QueryWriteStatus(context.Context, *bs.QueryWriteStatusRequest) (*bs.QueryWriteStatusResponse, error) {
	return nil, status.Error(codes.Unimplemented, "resumable writes are not supported; restart the upload")
}

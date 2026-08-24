package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// --- gRPC Service Definition ---
// We define the service manually to avoid requiring protoc.

const grpcServiceName = "agui.AGUIDevice"

// GRPCRunRequest is the gRPC version of RunRequest.
type GRPCRunRequest struct {
	Agent    string            `json:"agent"`
	Message  string            `json:"message"`
	ThreadId string            `json:"thread_id,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

// GRPCRunEvent is a single AG-UI event sent over gRPC.
type GRPCRunEvent struct {
	Type      string          `json:"type"`
	RunId     string          `json:"run_id"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// GRPCServer implements the gRPC AG-UI service.
type GRPCServer struct {
	grpcServer *grpc.Server
	runFunc    RunFunc
	handler    *Handler
	lis        net.Listener
}

// NewGRPCServer creates a new gRPC server for AG-UI.
func NewGRPCServer(runFunc RunFunc, handler *Handler) *GRPCServer {
	s := &GRPCServer{
		runFunc: runFunc,
		handler: handler,
	}
	s.grpcServer = grpc.NewServer()
	return s
}

// Start starts the gRPC server on the given address.
func (s *GRPCServer) Start(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("grpc: listening on %s: %w", addr, err)
	}
	s.lis = lis

	// Register the service.
	s.registerService()

	log.Printf("gRPC: server listening on %s", addr)
	go func() {
		if err := s.grpcServer.Serve(lis); err != nil {
			log.Printf("gRPC: serve error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully stops the gRPC server.
func (s *GRPCServer) Stop() {
	s.grpcServer.GracefulStop()
}

// Addr returns the listener address. Only valid after Start.
func (s *GRPCServer) Addr() net.Addr {
	if s.lis == nil {
		return nil
	}
	return s.lis.Addr()
}

// registerService registers the AG-UI gRPC service with a streaming Run RPC.
func (s *GRPCServer) registerService() {
	// Define the service descriptor manually.
	svcDesc := grpc.ServiceDesc{
		ServiceName: grpcServiceName,
		HandlerType: (*interface{})(nil),
		Methods:     []grpc.MethodDesc{},
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "Run",
				Handler:       s.handleGRPCStream,
				ServerStreams:  true,
			},
		},
		Metadata: "agui.proto",
	}

	// Register with a nil implementation since we handle everything in the stream handler.
	s.grpcServer.RegisterService(&svcDesc, nil)
}

// grpcServerStream implements the StreamServer interface for our Run RPC.
type grpcServerStream struct {
	grpc.ServerStream
	runID    string
	threadID string
}

func (s *grpcServerStream) SendMsg(m interface{}) error {
	return s.ServerStream.SendMsg(m)
}

// handleGRPCStream handles a streaming Run RPC.
func (s *GRPCServer) handleGRPCStream(srv interface{}, stream grpc.ServerStream) error {
	// Receive the initial request.
	var req GRPCRunRequest
	if err := stream.RecvMsg(&req); err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive request: %v", err)
	}

	// Validate.
	if req.Agent == "" {
		return status.Errorf(codes.InvalidArgument, "agent is required")
	}
	if req.Message == "" {
		return status.Errorf(codes.InvalidArgument, "message is required")
	}

	// Generate IDs.
	if req.ThreadId == "" {
		req.ThreadId = NewThreadID()
	}
	runID := NewRunID()

	// Create a gRPC encoder that sends events via the stream.
	encoder := &grpcEncoder{stream: stream, runID: runID}

	// Set up context with cancellation.
	ctx := stream.Context()
	streamWrapper := NewRunStream(runID, req.ThreadId, encoder)
	streamWrapper.SetContext(ctx)

	// Send RUN_START.
	if err := streamWrapper.SendRunStart(req.Agent, req.Meta); err != nil {
		log.Printf("gRPC: failed to send RUN_START: %v", err)
	}

	// Execute the agent run.
	runReq := RunRequest{
		Agent:    req.Agent,
		Message:  req.Message,
		ThreadID: req.ThreadId,
		Meta:     req.Meta,
	}
	s.runFunc(runReq, streamWrapper)

	// Send RUN_END if not already sent.
	if !streamWrapper.HasEnded() {
		_ = streamWrapper.SendRunEnd(nil)
	}

	return nil
}

// grpcEncoder implements EventEncoder by sending events as gRPC stream messages.
type grpcEncoder struct {
	stream grpc.ServerStream
	runID  string
}

func (e *grpcEncoder) WriteEvent(evt Event) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}

	event := &GRPCRunEvent{
		Type:  string(evt.GetType()),
		RunId: e.runID,
		Data:  data,
	}

	return e.stream.SendMsg(event)
}

func (e *grpcEncoder) WriteRaw(eventType string, data []byte) error {
	event := &GRPCRunEvent{
		Type:  eventType,
		RunId: e.runID,
		Data:  data,
	}
	return e.stream.SendMsg(event)
}

func (e *grpcEncoder) WriteComment(comment string) error {
	// gRPC doesn't have comments; no-op.
	return nil
}

func (e *grpcEncoder) Close() {
	// gRPC stream is closed by the framework.
}

func (e *grpcEncoder) IsClosed() bool {
	return false
}

// --- Client-side gRPC helpers ---

// GRPCRunClient is a convenience client for calling the AG-UI gRPC service.
type GRPCRunClient struct {
	conn   *grpc.ClientConn
	client grpc.ClientConnInterface
}

// NewGRPCRunClient creates a new gRPC client connected to the given address.
func NewGRPCRunClient(addr string) (*GRPCRunClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("grpc: connecting to %s: %w", addr, err)
	}
	return &GRPCRunClient{conn: conn, client: conn}, nil
}

// Close closes the client connection.
func (c *GRPCRunClient) Close() error {
	return c.conn.Close()
}

// SendRun sends a run request and returns a channel of events.
func (c *GRPCRunClient) SendRun(ctx context.Context, req GRPCRunRequest) (<-chan GRPCRunEvent, error) {
	stream, err := c.client.NewStream(ctx,
		&grpc.StreamDesc{StreamName: "Run", ServerStreams: true},
		grpcServiceName+".Run",
	)
	if err != nil {
		return nil, fmt.Errorf("grpc: opening stream: %w", err)
	}

	// Send the request.
	if err := stream.SendMsg(&req); err != nil {
		return nil, fmt.Errorf("grpc: sending request: %v", err)
	}

	// Receive events.
	eventCh := make(chan GRPCRunEvent, 32)
	go func() {
		defer close(eventCh)
		for {
			var event GRPCRunEvent
			if err := stream.RecvMsg(&event); err != nil {
				return
			}
			select {
			case eventCh <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return eventCh, nil
}

// InvokeWithHeaders sends a run request with metadata headers.
func InvokeWithHeaders(ctx context.Context, conn *grpc.ClientConn, req GRPCRunRequest, headers map[string]string) (grpc.ClientStream, error) {
	md := metadata.New(headers)
	ctx = metadata.NewOutgoingContext(ctx, md)

	stream, err := conn.NewStream(ctx,
		&grpc.StreamDesc{StreamName: "Run", ServerStreams: true},
		grpcServiceName+".Run",
	)
	if err != nil {
		return nil, err
	}

	if err := stream.SendMsg(&req); err != nil {
		return nil, err
	}

	return stream, nil
}
